package service

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"strings"
)


type ModuleProvider interface {
	DefaultName() string
	Create(Host) (Module, error)
}

func ModuleProviderFromFunc(name string, cf func(Host) (Module, error)) ModuleProvider {
	return &moduleProvider{name, cf}
}

type moduleProvider struct {
	name string
	cf   func(Host) (Module, error)
}

func (m *moduleProvider) DefaultName() string              { return m.name }
func (m *moduleProvider) Create(host Host) (Module, error) { return m.cf(host) }


type ModuleCloser interface {
	Close(context.Context) error
}

type ModuleServer interface {
	Serve(context.Context) error
}

type Module interface {
	ModuleServer
}

type HostModule interface {
	// GetHost return the host belong to
	GetHost() Host

	// GetModuleHandler return the module handler created during build
	GetModuleHandler() interface{}
}

type ModuleOption func(*moduleOptions)

type moduleOptions struct {
	serviceName    string
	moduleName     string
	roles          []string
}

func WithName(name string) ModuleOption {
	return func(o *moduleOptions) {
		o.serviceName = name
	}
}

func WithModuleName(name string) ModuleOption {
	return func(o *moduleOptions) {
		o.moduleName = name
	}
}

func WithRole(role string) ModuleOption {
	return func(o *moduleOptions) {
		if role == "" {
			return
		}
		for _, r := range o.roles {
			if r == role {
				return
			}
		}

		o.roles = append(o.roles, role)
	}
}

type hostedModule struct {
	options    moduleOptions
	module     Module
	scopedHost *scopedHost
}


func newHostedModule(host *host, mi moduleInfo, tlsConfig tlsConfig, forkName string) (*hostedModule, error) {
	mos := moduleOptions{
		serviceName:    host.Name(),
		moduleName:     mi.provider.DefaultName(),
		roles:          []string{},
	}

	for _, o := range mi.options {
		o(&mos)
	}

	scoped := &scopedHost{
		Host:      host,
		options:   mos,
		tlsConfig: tlsConfig,
	}

	module, err := mi.provider.Create(scoped)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	err = scoped.initModuleConfig(host.viper, host.configVariables, forkName)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &hostedModule{
		options:    mos,
		module:     module,
		scopedHost: scoped,
	}, nil
}

func (m *hostedModule) Serve(ctx context.Context) error {
	err := m.module.Serve(ctx)
	return errors.WithStack(err)
}

type scopedHost struct {
	Host
	options      moduleOptions
	tlsConfig    tlsConfig
	moduleConfig *moduleConfigReader
}

func (h *scopedHost) Name() string {
	return h.options.serviceName
}

func (h *scopedHost) ModuleName() string {
	return h.options.moduleName
}

func (h *scopedHost) Roles() []string {
	return h.options.roles
}

func (h *scopedHost) Logger() *zap.Logger {
	logger := h.Host.Logger()
	return logger.With(
		zap.String("service", h.options.serviceName),
		zap.String("module", h.options.moduleName),
	)
}

func (h *scopedHost) initModuleConfig(core *viper.Viper, configVariables map[string]*viper.Viper, forkName string) error {
	var sub *viper.Viper

	// verify host config has submodule config
	moduleName := h.ModuleName()
	subModuleConfig := core.Sub("modules").Sub(moduleName)
	if subModuleConfig == nil {
		return fmt.Errorf("failed to load module config for [%s], maybe it is not configed in config file", moduleName)
	}

	if forkName == "" {
		sub = subModuleConfig
	} else {
		sub = subModuleConfig.Sub("forks").Sub(forkName)

		for _, key := range subModuleConfig.AllKeys() {
			if !strings.HasPrefix(key, "forks.") {
				if !sub.IsSet(key) {
					sub.Set(key, subModuleConfig.Get(key))
				}
			}
		}
	}

	if !sub.IsSet("enable_tls") {
		sub.Set("enable_tls", h.tlsConfig.EnableTLS)
	}

	if !sub.IsSet("ca_file") {
		sub.Set("ca_file", h.tlsConfig.CAFile)
	}

	moduleConfig := newModuleConfigReader(sub, configVariables)
	h.moduleConfig = moduleConfig

	return nil
}

func (h *scopedHost) ModuleConfig() ModuleConfigReader {
	return h.moduleConfig
}
