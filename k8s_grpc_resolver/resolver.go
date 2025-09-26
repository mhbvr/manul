package k8s_grpc_resolver

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mhbvr/manul/k8s_watcher"
	"google.golang.org/grpc/resolver"
)

const k8sScheme = "k8s"

type k8sResolverBuilder struct{}

func (k *k8sResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	r := &k8sResolver{
		target: target,
		cc:     cc,
		ctx:    context.Background(),
	}

	if err := r.parseTarget(); err != nil {
		return nil, err
	}

	go r.start()
	// Dirty hack. Remove it and implement proper watcher
	time.Sleep(5 * time.Second)
	return r, nil
}

func (k *k8sResolverBuilder) Scheme() string {
	return k8sScheme
}

type k8sResolver struct {
	target      resolver.Target
	cc          resolver.ClientConn
	ctx         context.Context
	cancel      context.CancelFunc
	serviceName string
	namespace   string
	port        int
	watcher     *k8s_watcher.K8sWatcher
}

func (r *k8sResolver) parseTarget() error {
	host := r.target.URL.Hostname()
	portStr := r.target.URL.Port()

	if portStr == "" {
		return fmt.Errorf("port is required in target %s", r.target.Endpoint())
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("invalid port %s in target %s: %v", portStr, r.target.Endpoint(), err)
	}

	parts := strings.Split(host, ".")
	if len(parts) != 2 {
		return fmt.Errorf("invalid format: expected service.namespace, got %s", host)
	}

	r.serviceName = parts[0]
	r.namespace = parts[1]
	r.port = port

	return nil
}

func (r *k8sResolver) start() {
	ctx, cancel := context.WithCancel(r.ctx)
	r.cancel = cancel

	var err error
	r.watcher, err = k8s_watcher.NewK8sWatcher(ctx, r.namespace, r.serviceName, "")
	if err != nil {
		r.cc.ReportError(fmt.Errorf("failed to create k8s watcher: %v", err))
		return
	}

	notifChan := r.watcher.NotifChan()

	for {
		select {
		case <-ctx.Done():
			return
		case <-notifChan:
			r.updateEndpoints()
		}
	}
}

func (r *k8sResolver) updateEndpoints() {
	endpoints := r.watcher.GetEndpoints()
	var addrs []resolver.Address

	for _, ep := range endpoints {
		addrs = append(addrs, resolver.Address{
			Addr: fmt.Sprintf("%s:%d", ep.Address, r.port),
		})
	}

	state := resolver.State{
		Addresses: addrs,
	}

	r.cc.UpdateState(state)
}

func (r *k8sResolver) ResolveNow(resolver.ResolveNowOptions) {
	r.updateEndpoints()
}

func (r *k8sResolver) Close() {
	if r.cancel != nil {
		r.cancel()
	}
}

// Package is a placeholder to ensure the resolver is registered when imported
var Package struct{}

func init() {
	resolver.Register(&k8sResolverBuilder{})
}
