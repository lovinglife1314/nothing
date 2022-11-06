package service

import (
	"context"
	"fmt"
	"github.com/lovinglife1314/nothing/grpc/naming"
	"github.com/lovinglife1314/nothing/internal"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/lovinglife1314/nothing/modules"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/robfig/cron"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"

	etcd "github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/clientv3/concurrency"
	etcdns "github.com/coreos/etcd/clientv3/namespace"
	etcdyaml "github.com/coreos/etcd/clientv3/yaml"
	grpcnaming "google.golang.org/grpc/naming"
)

const (
	defaultConfigPath = "./config"
)

type Host interface {
	Name() string
	ModuleName() string
	Description() string
	Roles() []string
	Serve() error
	Logger() *zap.Logger
	ModuleConfig() ModuleConfigReader
	Metrics() prometheus.Registerer
	RegisterModule(moduleName string, addr string, metaData interface{}) error
	EtcdSession() *concurrency.Session
}

type Option func(h *host)

type host struct {
	args       []string
	configPath string

	cron *cron.Cron

	viper           *viper.Viper
	configVariables map[string]*viper.Viper
	client      *etcd.Client
	registry    *prometheus.Registry
	level       zap.AtomicLevel
	logger      *zap.Logger
	modules []*hostedModule

	loggerOnExit      func() error

	serviceConfig serviceConfig
	tlsConfig     tlsConfig
	moduleRunnings   sync.WaitGroup
	signalc          chan os.Signal
	errorc           chan error

	instanceID string

	session     *concurrency.Session
}

func newHost(b Builder) (Host, error){
	h := &host{
		args:       os.Args,
		cron:       cron.New(),
		viper:      viper.New(),
		signalc:    make(chan os.Signal),
		errorc:     make(chan error, 128),
		instanceID: strconv.FormatUint(randomID(), 36),
	}

	for _, o := range b.options {
		o(h)
	}

	if err := h.setupConfig(); err != nil {
		return nil, errors.WithStack(err)
	}
	if err := h.setupLogging(); err != nil {
		return nil, errors.WithStack(err)
	}

	if err := h.setupEtcd(); err != nil {
		return nil, errors.WithStack(err)
	}

	h.logger.Info("adding modules")
	for _, m := range b.modules {
		if err := h.addModule(m); err != nil {
			return nil, errors.WithStack(err)
		}
	}

	h.logger.Info("created host",
		zap.Any("config_path", h.configPath),
		zap.Any("config", h.serviceConfig))
	return h, nil
}

func (h *host) addModule(mi moduleInfo) error {
	module, err := newHostedModule(h, mi, h.tlsConfig, "")
	if err != nil {
		return errors.WithStack(err)
	}

	logger := h.logger.With(
		zap.String("service", module.options.serviceName),
		zap.String("module", module.options.moduleName),
		zap.Strings("roles", module.options.roles),
	)

	if !Roles(h.serviceConfig.Roles).Support(module.options.roles) {
		logger.Info("skip module due to roles",
			zap.Strings("host_roles", h.serviceConfig.Roles))
		return nil
	}

	logger.Info("add module",
		zap.String("service", module.options.serviceName),
		zap.String("module", module.options.moduleName),
		zap.Strings("host_roles", h.serviceConfig.Roles),
		zap.Any("host_tags", h.serviceConfig.Tags),
	)
	h.modules = append(h.modules, module)

	moduleConfig := module.scopedHost.ModuleConfig()
	if moduleConfig.IsSet("forks") {
		var forks map[string]modules.ModuleConfig
		if err := moduleConfig.UnmarshalKey("forks", &forks); err != nil {
			return errors.WithStack(err)
		}
		for forkName := range forks {
			forkedModule, err := newHostedModule(h, mi, h.tlsConfig, forkName)
			if err != nil {
				return errors.WithStack(err)
			}
			logger := h.logger.With(
				zap.String("service", module.options.serviceName),
				zap.String("module", module.options.moduleName),
				zap.Strings("roles", module.options.roles),
				zap.String("fork_name", forkName),
			)

			logger.Info("add fork module",
				zap.String("service", module.options.serviceName),
				zap.String("module", module.options.moduleName),
				zap.Strings("host_roles", h.serviceConfig.Roles),
				zap.Any("host_tags", h.serviceConfig.Tags),
				zap.String("fork_name", forkName),
			)
			h.modules = append(h.modules, forkedModule)
		}
	}
	return nil
}

func WithMetrics(registry *prometheus.Registry) Option {
	return func(h *host) {
		h.registry = registry
	}
}


func WithLogger(logger *zap.Logger) Option {
	return func(h *host) {
		h.logger = logger
	}
}

func (h *host) serveModule(ctx context.Context) {
	for _, module := range h.modules {
		go func(module *hostedModule) {
			logger := h.logger.With(
				zap.String("service", module.options.serviceName),
				zap.String("module", module.options.moduleName),
			)

			h.moduleRunnings.Add(1)
			logger.Info("enter serving module goroutine")
			defer func() {
				if r := recover(); r != nil {
					err := fmt.Errorf("%v", r)
					logger.Error("module recover panic", zap.Error(err))
					nonblockingError(h.errorc, err)
				}
				h.moduleRunnings.Done()
				logger.Info("exit serving module goroutine")
			}()

			if err := module.Serve(ctx); err != nil {
				logger.Error("module serving error", zap.Error(err))
				nonblockingError(h.errorc, err)
			}

		}(module)
	}
}

func(h *host) Serve( ) error{
	h.logger.Info("starting serve modules")

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		_ = h.cleanup()
	}()

	if err := h.prepare(ctx); err != nil {
		h.logger.Error("prepare for serve failed", zap.Error(err))
		return errors.WithStack(err)
	}

	moduleContext, moduleCancel := context.WithCancel(ctx)
	defer func() {
		moduleCancel()
	}()
	h.serveModule(moduleContext)

	select {
	case sig := <-h.signalc:
		h.logger.Error("receive signal", zap.String("signal", sig.String()))
		moduleCancel()
		h.moduleRunnings.Wait()
		cancel()
	case <-h.errorc:
		h.logger.Error("some module failed")
		cancel()
	case <-ctx.Done():
		h.logger.Error("host context done")
	}

	h.logger.Info("waiting modules exit")
	h.logger.Info("finish serve modules")
	return ctx.Err()
}

func (h *host) prepare(ctx context.Context) error {
	if internal.IsETCDEnabled() && internal.IsETCDSessionEnabled() {
		h.logger.Info("creating etcd session")
		var sessionConfig sessionConfig
		if err := h.viper.UnmarshalKey("session", &sessionConfig); err != nil {
			return errors.WithStack(err)
		}

		ttl := sessionConfig.TTL
		session, err := concurrency.NewSession(h.client, concurrency.WithTTL(ttl))
		if err != nil {
			h.logger.Error("created etcd session failed", zap.Error(err))
			return errors.WithStack(err)
		}
		h.session = session
		h.logger.Info("created etcd session",
			zap.Int64("lease_id", int64(h.session.Lease())),
			zap.Int("session_ttl", ttl))
	} else {
		h.logger.Warn("creating etcd session is skipped")
	}

	h.logger.Info("starting cron")
	h.cron.Start()
	h.logger.Info("started cron")

	h.registerSignalHandlers()

	return  nil
}

func (h *host) EtcdSession() *concurrency.Session {
	return h.session
}

func (h *host) registerSignalHandlers() {
	h.logger.Info("registering signal handler")
	signal.Notify(h.signalc,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGABRT,
		syscall.SIGQUIT,
	)
}

func (h *host) RegisterModule(moduleName string, addr string, metaData interface{}) error {
	if h.EtcdSession() == nil {
		h.logger.Warn("skip RegisterModule because etcd session is nil", zap.String("module_name", moduleName))
		return nil
	}

	resolver := naming.EtcdResolver{Client: h.client}
	update := grpcnaming.Update{
		Addr:     addr,
		Metadata: metaData,
	}
	err := resolver.Update(context.Background(), moduleName, update, etcd.WithLease(h.EtcdSession().Lease()))
	if err != nil {
		return errors.WithStack(err)
	}

	return  nil
}

func (h *host) setupConfig() error {
	// bind flags
	flagset := pflag.NewFlagSet(h.args[0], pflag.ExitOnError)
	bindFlags(flagset)
	if err := flagset.Parse(h.args[1:]); err != nil {
		return errors.WithStack(err)
	}

	h.viper.BindPFlags(flagset)

	// set config path from command line if provided
	configPath := h.viper.GetString("config-path")
	if configPath == "" {
		configPath = defaultConfigPath
	}

	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return errors.WithStack(err)
	}
	h.configPath = absConfigPath

	h.viper.SetConfigName("config")
	h.viper.AddConfigPath(h.configPath)
	if err = h.viper.ReadInConfig(); err != nil {
		return errors.WithStack(err)
	}

	if err := h.viper.Unmarshal(&h.serviceConfig); err != nil {
		return errors.WithStack(err)
	}

	variables := make(map[string]*viper.Viper)

	h.configVariables = variables

   return  nil
}

func (h *host) setupLogging() error {
	var loggingconfig loggingConfig
	if err := h.viper.UnmarshalKey("logging", &loggingconfig); err != nil {
		return errors.WithStack(err)
	}

	var opts []zap.Option
	var encoder zapcore.Encoder
	switch loggingconfig.Env {
	case "development":
		level, err := strToLevel(loggingconfig.Level, zapcore.DebugLevel)
		if err != nil {
			return err
		}
		h.level = zap.NewAtomicLevelAt(level)
		opts = []zap.Option{
			zap.Development(),
			zap.AddCaller(),
			zap.AddStacktrace(zap.WarnLevel),
		}
		encoder = zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	case "production":
		level, err := strToLevel(loggingconfig.Level, zapcore.InfoLevel)
		if err != nil {
			return err
		}
		h.level = zap.NewAtomicLevelAt(level)
		opts = []zap.Option{
			zap.AddCaller(),
			zap.AddStacktrace(zap.ErrorLevel),
		}
		encoder = zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	default:
		return fmt.Errorf("unsupported env %s", loggingconfig.Env)
	}

	absPath, err := filepath.Abs(loggingconfig.Path)
	if err != nil {
		return errors.WithStack(err)
	}

	if loggingconfig.Unique {
		absPath = joinLogPathWithInstanceID(absPath, h.instanceID)
	}

	rotateWriter := &lumberjack.Logger{
		Filename: absPath,
		MaxSize:  int(loggingconfig.MaxSize), // MB
	}

	writer := zapcore.AddSync(rotateWriter)
	if loggingconfig.Stderr {
		writer = zap.CombineWriteSyncers(writer, os.Stderr)
	}
	if loggingconfig.Stdout {
		writer = zap.CombineWriteSyncers(writer, os.Stdout)
	}

	wcore := zapcore.NewCore(encoder, writer, h.level)

	core := zapcore.NewTee(wcore)
	h.logger = zap.New(core, opts...).With(zap.String("instance_id", h.instanceID))

	h.logger.Info("add log file rotate cron job", zap.String("cron", loggingconfig.Cron))
	err = h.cron.AddFunc(loggingconfig.Cron, func() {
		logger := h.logger.With(zap.String("path", absPath))
		logger.Info("rotating log file")
		if err := rotateWriter.Rotate(); err != nil {
			logger.Error("failed to rotate log file", zap.Error(err))
		}
		logger.Info("rotated log file")
	})

	if err != nil {
		return errors.WithStack(err)
	}

	h.loggerOnExit = func() error {
		//todo, require review
		//nolint: errcheck
		h.logger.Sync()
		return rotateWriter.Rotate()
	}

    return nil
}

func (h *host) setupEtcd() error {
	if !internal.IsETCDEnabled() {
		h.logger.Warn("etcd client is disabled, skipping setup")
		return nil
	}

	h.logger.Info("creating etcd client")
	etcdconfig, err := etcdyaml.NewConfig(path.Join(h.configPath, "etcd.yaml"))
	if err != nil {
		return errors.WithStack(err)
	}

	client, err := etcd.New(*etcdconfig)
	if err != nil {
		return errors.WithStack(err)
	}

	prefix := "/" + h.Name() + "/"
	h.logger.Info("etcd prefix is " + prefix)
	client.KV = etcdns.NewKV(client.KV, prefix)
	client.Lease = etcdns.NewLease(client.Lease, prefix)
	client.Watcher = etcdns.NewWatcher(client.Watcher, prefix)

	h.client = client
	h.logger.Info("created etcd client")
	return nil

	return  nil
}

func (h *host) cleanup() error {
	if h.logger != nil {
		h.logger.Info("exit Logger")
		if err := h.loggerOnExit(); err != nil {
			// Logger is existing, try to save the error to stdout
			fmt.Printf("error happen during loggerOnExit, %s", err)
		}

		h.logger = nil
	}
	return nil
}

func  (h *host) Name() string{
	return h.serviceConfig.Name
}

func (h *host) ModuleName() string{
	return ""
}

func (h *host)Description() string{
	return h.serviceConfig.Description
}

func (h *host) Roles() []string{
	return h.serviceConfig.Roles
}

func (h *host) Logger() *zap.Logger {
	return h.logger
}

func (h *host) ModuleConfig() ModuleConfigReader {
	return nil
}

func (h *host) Metrics() prometheus.Registerer {
	return h.registry
}

// convert level string to zapcore.Level, use defaultLevel if level is empty
func strToLevel(level string, defaultLevel zapcore.Level) (zapcore.Level, error) {
	if len(level) != 0 {
		if err := defaultLevel.UnmarshalText([]byte(level)); err != nil {
			return zapcore.FatalLevel, fmt.Errorf("unsupported logging level %s", level)
		}
	}

	return defaultLevel, nil
}

func nonblockingError(errorc chan<- error, err error) {
	select {
	case errorc <- err:
	default:
	}
}

func joinLogPathWithInstanceID(path string, instanceID string) string {
	if strings.HasSuffix(path, ".log") {
		index := len(path) - 4
		path = path[:index]
	}
	path = path + "-" + instanceID + ".log"
	return path
}