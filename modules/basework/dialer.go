package basework

import (
	"context"

	etcd "github.com/coreos/etcd/clientv3"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/lovinglife1314/nothing/grpc/naming"
)

type ServiceDialerOptions struct {
	// Metadata will be used filter which endpoint has same metadata
	Metadata map[string]string
}

// ServiceDialer is the interface to dial to service
type ServiceDialer interface {
	DialService(ctx context.Context, serviceName string, dialOptions ServiceDialerOptions, opts ...grpc.DialOption) (*grpc.ClientConn, error)
}

var _ ServiceDialer = (*Dialer)(nil)

type DialerOptions struct {
	EtcdClient *etcd.Client
	Logger     *zap.Logger

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

func NewDialer(opts DialerOptions) (*Dialer, error) {
	//nolint gosimple
	d := Dialer{
		EtcdClient: opts.EtcdClient,
		Logger:     opts.Logger,

		EnableTLS:        opts.EnableTLS,
		CAFile:           opts.CAFile,
		ServerName:       opts.ServerName,
		EnableClientAuth: opts.EnableClientAuth,
		CertFile:         opts.CertFile,
		KeyFile:          opts.KeyFile,
	}

	return &d, nil
}

// DialService used to dial to service with specific ServiceDialerOptions
//  could provide metadata matcher in ServiceDialerOptions
//   servicePath: "/" + hostName + "/" + moduleName
func (d *Dialer) DialService(ctx context.Context, servicePath string, dialOptions ServiceDialerOptions, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	// create matcher
	var matcher naming.Matcher
	if len(dialOptions.Metadata) > 0 {
		matcher = naming.EtcdMatcher{
			MetadataMap: dialOptions.Metadata,
		}
	}

	target := servicePath

	return d.DialWithMatcher(ctx, target, matcher, opts...)

}

// DialWithMatcher is core method to create grpc.ClientConn
//   it is maybe changed in the future, don't rely on this method
func (d *Dialer) DialWithMatcher(ctx context.Context, target string, matcher naming.Matcher, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
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

	// create resolver
	resolver := &naming.EtcdResolver{
		Client:  d.EtcdClient,
		Matcher: matcher,
	}
	balancer := grpc.RoundRobin(resolver)
	dialopts = append(dialopts, grpc.WithBalancer(balancer))

	// override using client provided opts
	dialopts = append(dialopts, opts...)

	return grpc.DialContext(ctx, target, dialopts...)
}

