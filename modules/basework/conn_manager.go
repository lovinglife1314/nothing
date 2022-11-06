package basework

import (
	"context"
	"fmt"

	"github.com/lovinglife1314/nothing/grpc/naming"
	"google.golang.org/grpc"
	"sync"
)

// ConnManager caches grpc connection
type ConnManager struct {
	dialer *Dialer

	mu    sync.RWMutex // protect conns map
	conns map[string]*conn
}

type conn struct {
	mu    sync.RWMutex // used to guard only one gconn is created
	gconn *grpc.ClientConn
}

// NewConnManager create ConnManager from dialer
func NewConnManager(dialer *Dialer) *ConnManager {
	return &ConnManager{
		dialer: dialer,
		conns:  make(map[string]*conn),
	}
}

// Dial dial target and cache connection for future use
// Deprecated: use DialService instead.
func (cm *ConnManager) Dial(target string, selector naming.Selector, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	return cm.DialContext(context.Background(), target, selector, opts...)
}

// DialContext dial target within context and cache connection for future use
// Deprecated: use DialService instead.
func (cm *ConnManager) DialContext(ctx context.Context, target string, selector naming.Selector, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	key := cm.mkKey(target, selector)
	//speed up result
	cm.mu.RLock()
	c, ok := cm.conns[key]
	if ok {
		cm.mu.RUnlock()
		c.mu.RLock()
		if c.gconn != nil {
			gconn := c.gconn
			c.mu.RUnlock()
			return gconn, nil
		}
		c.mu.RUnlock()
	} else {
		cm.mu.RUnlock()
	}

	cm.mu.Lock()
	c, ok = cm.conns[key]
	if !ok {
		c = &conn{}
		cm.conns[key] = c
	}
	cm.mu.Unlock()

	// make sure only one grpc conn is created
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gconn == nil {
		gconn, err := cm.dialer.DialContext(ctx, target, selector, opts...)
		if err != nil {
			return nil, err
		}
		c.gconn = gconn
	}
	return c.gconn, nil
}

func (cm *ConnManager) getOrCreateClientConnect(key string) *conn {
	//speed up result
	cm.mu.RLock()
	c, ok := cm.conns[key]
	cm.mu.RUnlock()
	if ok {
		return c
	}

	cm.mu.Lock()
	c, ok = cm.conns[key]
	if !ok {
		c = &conn{}
		cm.conns[key] = c
	}
	cm.mu.Unlock()

	return c
}

func (cm *ConnManager) mkKey(target string, selector naming.Selector) string {
	return fmt.Sprintf("%s-[%#v]", target, selector)
}

// todo, let ServiceDialerOptions to generate a key
func serviceKey(serviceName string, dialOptions ServiceDialerOptions) string {
	key := "v2-" + serviceName

	key += "{"
	for k, v := range dialOptions.Metadata {
		key += fmt.Sprintf("%s,%s;", k, v)
	}
	key += "}"

	return key
}

type DialServiceOptions struct {
	// Name is provided to create global connection, if provide as empty, manager will create Name by itself
	Name string

	// ServicePath is used to select which service(aka. module) will be selected out
	//  ServicePath: "/" + hostName + "/" + moduleName
	ServicePath string

	// ExtraDialOptions is used to target more specific (service) instance
	ExtraDialOptions ServiceDialerOptions

	GRPCOptions []grpc.DialOption
}

// DialService to create a grpc ClientConn by providing fx service path & other filter options
func (cm *ConnManager) DialService(ctx context.Context, opts DialServiceOptions) (*grpc.ClientConn, error) {
	key := opts.Name
	if len(opts.Name) == 0 {
		key = serviceKey(opts.ServicePath, opts.ExtraDialOptions)
	}

	c := cm.getOrCreateClientConnect(key)

	// make sure only one grpc conn is created
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gconn == nil {
		gconn, err := cm.dialer.DialService(ctx, opts.ServicePath, opts.ExtraDialOptions, opts.GRPCOptions...)
		if err != nil {
			return nil, err
		}
		c.gconn = gconn
	}
	return c.gconn, nil
}

