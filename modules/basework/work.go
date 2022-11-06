package basework

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/lovinglife1314/nothing/modules"

	"github.com/lovinglife1314/nothing/service"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	defaultCloseTimeout    = 10 * time.Second
	defaultShutdownTimeout = 10 * time.Second
)

type ModuleConfig struct {
	modules.ModuleConfig `mapstructure:",squash"`
}

type WorkServer interface {
	Register(*grpc.Server)
}

type ServiceProviderFunc func(service.Host) (WorkServer, error)

// TODO: support multiple grpc services per grpc module
func New(spf ServiceProviderFunc) service.ModuleProvider {
	return service.ModuleProviderFromFunc("WorkServer", func(host service.Host) (service.Module, error) {
		return new(host, spf)
	})
}

type module struct {
	host           service.Host
	spf            ServiceProviderFunc
	moduleConfig   ModuleConfig
	server         *grpc.Server

	handler WorkServer
}

func new(host service.Host, spf ServiceProviderFunc)(service.Module, error){
	m := &module{
		host: host,
		spf:  spf,
	}

	//todo, find a way to pass this option from builder
	return m, nil
}

func (m * module) Serve(ctx context.Context)  error{

	logger := m.host.Logger()
	grpcServer, err := m.spf(m.host)
	if err != nil {
		return fmt.Errorf("failed to create grpc handler for %s:%s, %w", m.host.Name(), m.host.ModuleName(), err)
	}

	m.handler = grpcServer

	chanBufferSize := 16
	errorc := make(chan error, chanBufferSize)

	if err := m.host.ModuleConfig().Unmarshal(&m.moduleConfig); err != nil {
		return errors.WithStack(err)
	}

	logger.Info("grpc module config", zap.Any("module_config", m.moduleConfig))

	m.server = grpc.NewServer( )

	listenAddr := m.moduleConfig.ListenAddress
	logger.Info("listening tcp", zap.String("addr", listenAddr))
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return errors.WithStack(err)
	}

	grpcServer.Register(m.server)
	reflection.Register(m.server)

	ctx, cancel := context.WithCancel(ctx)
	var running sync.WaitGroup
	moduleServer, ok := grpcServer.(service.ModuleServer)

	if ok {
		running.Add(1)
		go func() {
			logger.Info("enter module serve goroutine")
			defer func() {
				running.Done()
				logger.Info("exit module serve goroutine")
			}()
			if err := moduleServer.Serve(ctx); err != nil {
				logger.Error("module grpc serve failed", zap.Error(err))
				errorc <- err
			}
		}()
	}

	running.Add(1)
	go func() {
		logger.Info("enter grpc goroutine")
		defer func() {
			running.Done()
			logger.Info("exit grpc goroutine")
		}()
		if err := m.server.Serve(listener); err != nil {
			logger.Error("grpc server failed", zap.Error(err))
			errorc <- err
		}
	}()

	registerAddr := listenAddr
	if m.moduleConfig.RegisterAddress != "" {
		registerAddr = m.moduleConfig.RegisterAddress
	}

	if err := m.host.RegisterModule(m.host.ModuleName(), registerAddr, m.moduleConfig.Metadata); err != nil {
		logger.Error("register module failed", zap.Error(err))
		errorc <- err
	}

	select {
	case <-ctx.Done():
		logger.Info("grpc context done")
		err = ctx.Err()
	case e := <-errorc:
		logger.Info("grpc serving failed", zap.Error(err))
		err = e
	}
	cancel()

	m.shutdown()
	m.closeServer(grpcServer)

	logger.Info("waiting grpc goroutine exit")
	running.Wait()
	return err
}

func (m *module) closeServer(server WorkServer) {
	logger := m.host.Logger()
	logger.Info("closing server")
	defer logger.Info("closed server")

	closer, ok := server.(service.ModuleCloser)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultCloseTimeout)
	defer cancel()

	err := closer.Close(ctx)
	if err != nil {
		logger.Warn("failed to close server", zap.Error(err))
	}
}

func (m *module) shutdown() {
	logger := m.host.Logger()
	logger.Info("shutting down grpc server")

	done := make(chan struct{})
	go func() {
		m.server.GracefulStop()
		close(done)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	select {
	case <-ctx.Done():
		logger.Error("graceful shutdown grpc timeout, force shutting down",
			zap.Duration("timeout", defaultShutdownTimeout))
		m.server.Stop()
	case <-done:
	}

	logger.Info("shutted down grpc server")
	cancel()
}