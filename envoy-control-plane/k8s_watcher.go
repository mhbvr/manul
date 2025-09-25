package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Endpoint struct {
	Address string
	Port    int32
}

type K8sWatcher struct {
	ctx         context.Context
	clientset   *kubernetes.Clientset
	namespace   string
	serviceName string

	mu        sync.RWMutex
	endpoints map[string][]Endpoint // key: endpointslice name
	notifs    []chan struct{}
}

func NewK8sWatcher(ctx context.Context, namespace, serviceName, kubeconfig string) (*K8sWatcher, error) {
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %v", err)
	}

	res := &K8sWatcher{
		ctx:         ctx,
		clientset:   clientset,
		namespace:   namespace,
		serviceName: serviceName,
		endpoints:   make(map[string][]Endpoint),
	}

	go res.watchEndpointSlices()
	return res, nil
}

func (kw *K8sWatcher) NotifChan() chan struct{} {
	res := make(chan struct{}, 1)
	// Set inital notification
	res <- struct{}{}
	kw.mu.Lock()
	defer kw.mu.Unlock()
	kw.notifs = append(kw.notifs, res)
	return res
}

func (kw *K8sWatcher) GetEndpoints() []Endpoint {
	var allEndpoints []Endpoint
	kw.mu.RLock()
	defer kw.mu.RUnlock()

	for _, endpoints := range kw.endpoints {
		allEndpoints = append(allEndpoints, endpoints...)
	}

	return allEndpoints
}

func (kw *K8sWatcher) watchEndpointSlices() {
	var err error
	var watcher watch.Interface

	for {
		if watcher == nil {
			selector := fmt.Sprintf("kubernetes.io/service-name=%s", kw.serviceName)
			watcher, err = kw.clientset.DiscoveryV1().EndpointSlices(kw.namespace).Watch(kw.ctx, metav1.ListOptions{
				LabelSelector: selector,
			})
			if err != nil {
				log.Printf("Failed to create endpointslice watcher: %v, retry in 5 sec", err)
				timer := time.NewTimer(5 * time.Second)
				select {
				case <-kw.ctx.Done():
					return
				case <-timer.C:
					continue
				}
			}
			log.Printf("Watching EndpointSlices with selector: %s", selector)
		}

		select {
		case <-kw.ctx.Done():
			return
		case event, ok := <-watcher.ResultChan():
			if !ok {
				log.Printf("Watcher closed channel, restarting")
				watcher.Stop()
				watcher = nil
				continue
			}

			if event.Type == watch.Bookmark {
				continue
			}

			if event.Type == watch.Error {
				log.Printf("Error object received, objecttype:%v",
					event.Object.GetObjectKind().GroupVersionKind().String())
				watcher.Stop()
				watcher = nil
				continue
			}

			endpointSlice, ok := event.Object.(*discoveryv1.EndpointSlice)
			if !ok {
				log.Printf("Unexpected object type in endpointslice watch: %T", event.Object)
				continue
			}

			log.Printf("EndpointSlice event: %s for %s", event.Type, endpointSlice.Name)

			switch event.Type {
			case watch.Added, watch.Modified:
				kw.handleEndpointSliceUpdate(endpointSlice)
			case watch.Deleted:
				kw.handleEndpointSliceDeletion(endpointSlice.Name)
			}
		}
	}
}

func (kw *K8sWatcher) handleEndpointSliceUpdate(endpointSlice *discoveryv1.EndpointSlice) {
	var endpoints []Endpoint

	for _, endpoint := range endpointSlice.Endpoints {
		if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
			for _, address := range endpoint.Addresses {
				if len(endpointSlice.Ports) > 0 && endpointSlice.Ports[0].Port != nil {
					endpoints = append(endpoints, Endpoint{
						Address: address,
						Port:    *endpointSlice.Ports[0].Port,
					})
				}
			}
		}
	}

	kw.mu.Lock()
	kw.endpoints[endpointSlice.Name] = endpoints
	kw.mu.Unlock()
	kw.notify()
}

func (kw *K8sWatcher) handleEndpointSliceDeletion(endpointSliceName string) {
	kw.mu.Lock()
	delete(kw.endpoints, endpointSliceName)
	kw.mu.Unlock()
	kw.notify()
}

func (kw *K8sWatcher) notify() {
	allEndpoints := kw.GetEndpoints()
	log.Printf("Updated endpoints for service %s: %d total endpoints", kw.serviceName, len(allEndpoints))
	for _, ep := range allEndpoints {
		log.Printf("  - %s:%d", ep.Address, ep.Port)
	}

	kw.mu.RLock()
	defer kw.mu.RUnlock()
	for _, c := range kw.notifs {
		select {
		case c <- struct{}{}:
		default:
			log.Printf("Notif still penfing")
		}
	}
}
