package basework

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"time"

	etcd "github.com/coreos/etcd/clientv3"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	"github.com/lovinglife1314/nothing/grpc/naming"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"io/ioutil"
)

var errDialerRequireEtcd = errors.New("dialer require etcd client to dial")

type Dialer struct {
	EtcdClient *etcd.Client
	Logger     *zap.Logger
	Tracer     opentracing.Tracer

	// EnableTLS will turn on tls support while connecting to grpc server
	EnableTLS bool

	// CAFile is root cert file path
	//  that clients use when verifying server certificates.
	//  if set to empty, will use host's root CA set
	CAFile string

	// ServerName is used to determine grpc server name is same in certification
	//  used with EnableTLS
	//  when empty, will use name in certification to compare with
	ServerName string

	// EnableClientAuth enable tls client authentication support at client side
	//  ref to https://blog.cloudflare.com/introducing-tls-client-auth/
	EnableClientAuth bool

	// CertFile used with EnableClientAuth for tls client authentication support
	CertFile string

	// KeyFile used with EnableClientAuth for tls client authentication support
	KeyFile string
}

// Deprecated: use DialService instead.
func (d Dialer) Dial(target string, selector naming.Selector, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	return d.DialContext(context.Background(), target, selector, opts...)
}

// Deprecated: use DialService instead.
func (d Dialer) DialContext(ctx context.Context, target string, selector naming.Selector, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	if d.EtcdClient == nil {
		return nil, errDialerRequireEtcd
	}

	var dialopts []grpc.DialOption

	// tls options
	if d.EnableTLS {
		tlsopt, err := d.tlsDialOption()
		if err != nil {
			return nil, err
		}
		dialopts = append(dialopts, tlsopt)
	} else {
		dialopts = append(dialopts, grpc.WithInsecure())
	}

	// interceptor options
	interceptoropts, err := d.interceptorDialOptions()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	dialopts = append(dialopts, interceptoropts...)

	resolver := &naming.EtcdResolver{
		Client:   d.EtcdClient,
		Selector: selector,
	}
	balancer := grpc.RoundRobin(resolver)
	dialopts = append(dialopts, grpc.WithBalancer(balancer))

	// override using client provided opts
	dialopts = append(dialopts, opts...)

	return grpc.DialContext(ctx, target, dialopts...)
}

func (d Dialer) tlsDialOption() (grpc.DialOption, error) {
	tlsConfig := tls.Config{
		//InsecureSkipVerify: true,
		ServerName: d.ServerName,
	}

	if d.CAFile != "" {
		certPool := x509.NewCertPool()
		ca, err := ioutil.ReadFile(d.CAFile)
		if err != nil {
			return nil, fmt.Errorf("count not read ca certificate: %s", err)
		}

		if ok := certPool.AppendCertsFromPEM(ca); !ok {
			return nil, fmt.Errorf("cound not append ca certificate")
		}

		tlsConfig.RootCAs = certPool
	}

	if d.EnableClientAuth {
		certificate, err := tls.LoadX509KeyPair(d.CertFile, d.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("cound not load client key pair: %s", err)
		}

		tlsConfig.Certificates = []tls.Certificate{certificate}
	}

	creds := credentials.NewTLS(&tlsConfig)

	return grpc.WithTransportCredentials(creds), nil
}

func (d Dialer) interceptorDialOptions() ([]grpc.DialOption, error) {

	callopts := []grpc_retry.CallOption{
		grpc_retry.WithMax(3),
		grpc_retry.WithBackoff(grpc_retry.BackoffLinearWithJitter(500*time.Millisecond, 0.2)),
	}

 	dialOptions := []grpc.DialOption{
		grpc.WithUnaryInterceptor(
			grpc_middleware.ChainUnaryClient(
				grpc_retry.UnaryClientInterceptor(callopts...),
			),
		),
		grpc.WithStreamInterceptor(
			grpc_middleware.ChainStreamClient(
				grpc_retry.StreamClientInterceptor(callopts...),
			),
		),
	}

	return dialOptions, nil
}

