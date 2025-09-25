package main

import (
	"context"
	"fmt"
	"log"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type EDSCallbacks struct{}

func (cb *EDSCallbacks) OnStreamOpen(ctx context.Context, streamID int64, typeURL string) error {
	log.Printf("EDS: Stream opened - ID: %d, TypeURL: %s", streamID, typeURL)
	return nil
}

func (cb *EDSCallbacks) OnStreamClosed(streamID int64, node *core.Node) {
	log.Printf("EDS: Stream closed - ID: %d, Node: %s", streamID, node.GetId())
}

func (cb *EDSCallbacks) OnStreamRequest(streamID int64, req *discoveryv3.DiscoveryRequest) error {
	log.Printf("EDS: Stream request - ID: %d, , Node: %s, ResourceNames: %v",
		streamID, req.Node.GetId(), req.ResourceNames)
	return nil
}

func (cb *EDSCallbacks) OnStreamResponse(ctx context.Context, streamID int64, req *discoveryv3.DiscoveryRequest, resp *discoveryv3.DiscoveryResponse) {
	log.Printf("EDS: Stream response - ID: %d, Node: %s, ResourceNames: %v, Version: %s",
		streamID, req.Node.GetId(), req.ResourceNames, resp.VersionInfo)
}

func (cb *EDSCallbacks) OnFetchRequest(ctx context.Context, req *cache.Request) error {
	log.Printf("EDS: Fetch request - Node: %s, ResourceNames: %v, TypeURL: %s, Version: %s",
		req.Node.GetId(), req.ResourceNames, req.TypeUrl, req.VersionInfo)
	return nil
}

func (cb *EDSCallbacks) OnFetchResponse(req *discoveryv3.DiscoveryRequest, resp *discoveryv3.DiscoveryResponse) {
	log.Printf("EDS: Fetch response - Node: %s, ResourceNames: %v, Version: %s",
		req.Node.GetId(), req.ResourceNames, resp.VersionInfo)
}

func (cb *EDSCallbacks) OnDeltaStreamOpen(ctx context.Context, streamID int64, typeURL string) error {
	log.Printf("EDS: Delta stream opened - ID: %d, TypeURL: %s", streamID, typeURL)
	return nil
}

func (cb *EDSCallbacks) OnDeltaStreamClosed(streamID int64, node *core.Node) {
	log.Printf("EDS: Delta stream closed - ID: %d, Node: %s", streamID, node.GetId())
}

func (cb *EDSCallbacks) OnStreamDeltaRequest(streamID int64, req *cache.DeltaRequest) error {
	log.Printf("EDS: Delta stream request - ID: %d, Node: %s, ResourceNamesSubscribe: %v, ResourceNamesUnsubscribe: %v, TypeURL: %s",
		streamID, req.Node.GetId(), req.ResourceNamesSubscribe, req.ResourceNamesUnsubscribe, req.TypeUrl)
	return nil
}

func (cb *EDSCallbacks) OnStreamDeltaResponse(streamID int64, req *discoveryv3.DeltaDiscoveryRequest, resp *discoveryv3.DeltaDiscoveryResponse) {
	log.Printf("EDS: Delta stream response - ID: %d, Node: %s, TypeURL: %s",
		streamID, req.Node.GetId(), req.TypeUrl)
}

type EDSServer struct {
	cache       cache.SnapshotCache
	server      server.Server
	nodeID      string
	clusterName string
	version     int64
}

func NewEDSServer(nodeID, clusterName string) *EDSServer {
	callbacks := &EDSCallbacks{}
	cache := cache.NewSnapshotCache(false, cache.IDHash{}, nil)
	server := server.NewServer(context.Background(), cache, callbacks)

	return &EDSServer{
		cache:       cache,
		server:      server,
		nodeID:      nodeID,
		clusterName: clusterName,
		version:     1,
	}
}

func (eds *EDSServer) GetServer() server.Server {
	return eds.server
}

func (eds *EDSServer) UpdateEndpoints(endpoints []Endpoint) error {
	clusterLoadAssignment := eds.createClusterLoadAssignment(endpoints)

	snapshot, err := cache.NewSnapshot(
		fmt.Sprintf("%d", eds.version),
		map[resource.Type][]types.Resource{
			resource.EndpointType: {clusterLoadAssignment},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create snapshot: %v", err)
	}

	if err := eds.cache.SetSnapshot(context.Background(), eds.nodeID, snapshot); err != nil {
		return fmt.Errorf("failed to set snapshot: %v", err)
	}

	log.Printf("Updated EDS snapshot version %d with %d endpoints for cluster %s",
		eds.version, len(endpoints), eds.clusterName)

	eds.version++
	return nil
}

func (eds *EDSServer) createClusterLoadAssignment(endpoints []Endpoint) *endpoint.ClusterLoadAssignment {
	var lbEndpoints []*endpoint.LbEndpoint

	for _, ep := range endpoints {
		lbEndpoint := &endpoint.LbEndpoint{
			HostIdentifier: &endpoint.LbEndpoint_Endpoint{
				Endpoint: &endpoint.Endpoint{
					Address: &core.Address{
						Address: &core.Address_SocketAddress{
							SocketAddress: &core.SocketAddress{
								Address: ep.Address,
								PortSpecifier: &core.SocketAddress_PortValue{
									PortValue: uint32(ep.Port),
								},
							},
						},
					},
				},
			},
			HealthStatus:        core.HealthStatus_HEALTHY,
			LoadBalancingWeight: &wrapperspb.UInt32Value{Value: 1},
		}
		lbEndpoints = append(lbEndpoints, lbEndpoint)
	}

	return &endpoint.ClusterLoadAssignment{
		ClusterName: eds.clusterName,
		Endpoints: []*endpoint.LocalityLbEndpoints{
			{
				LbEndpoints: lbEndpoints,
			},
		},
	}
}

func (eds *EDSServer) Start(watcher *K8sWatcher) {
	log.Printf("Starting EDS server for cluster: %s", eds.clusterName)

	// Listen for updates
	notifChan := watcher.NotifChan()
	go func() {
		for range notifChan {
			endpoints := watcher.GetEndpoints()
			if err := eds.UpdateEndpoints(endpoints); err != nil {
				log.Printf("Failed to update endpoints: %v", err)
			}
		}
	}()
}
