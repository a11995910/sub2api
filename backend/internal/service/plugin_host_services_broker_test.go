package service

import (
	"context"
	"sync"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	hcplugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// brokerProbePlugin 是仅用于测试的插件传输实现：它实现 HostBrokerReceiver 以拿到
// broker，并在 InitHostServices 中用宿主下发的 broker id 拨号回宿主的 HostService，
// 走一次真实 gRPC 的 KVSet/KVGet 往返。
type brokerProbePlugin struct {
	pluginv1.UnimplementedTransportPluginServer
	broker *hcplugin.GRPCBroker

	mu       sync.Mutex
	ready    bool
	getFound bool
	getValue []byte
	failure  string
}

func (p *brokerProbePlugin) SetHostBroker(broker *hcplugin.GRPCBroker) { p.broker = broker }

func (p *brokerProbePlugin) InitHostServices(ctx context.Context, req *pluginv1.InitHostServicesRequest) (*pluginv1.InitHostServicesResponse, error) {
	if p.broker == nil {
		return &pluginv1.InitHostServicesResponse{Ready: false, Message: "broker 未注入"}, nil
	}
	conn, err := p.broker.Dial(req.HostServiceId)
	if err != nil {
		p.record("dial: " + err.Error())
		return &pluginv1.InitHostServicesResponse{Ready: false, Message: err.Error()}, nil
	}
	defer func() { _ = conn.Close() }()
	client := pluginv1.NewHostServiceClient(conn)
	if _, err := client.KVSet(ctx, &pluginv1.KVSetRequest{Namespace: "state", Key: "probe", Value: []byte("hello"), TtlSeconds: 60}); err != nil {
		p.record("set: " + err.Error())
		return &pluginv1.InitHostServicesResponse{Ready: false, Message: err.Error()}, nil
	}
	got, err := client.KVGet(ctx, &pluginv1.KVGetRequest{Namespace: "state", Key: "probe"})
	if err != nil {
		p.record("get: " + err.Error())
		return &pluginv1.InitHostServicesResponse{Ready: false, Message: err.Error()}, nil
	}
	p.mu.Lock()
	p.ready = true
	p.getFound = got.Found
	p.getValue = got.Value
	p.mu.Unlock()
	return &pluginv1.InitHostServicesResponse{Ready: true}, nil
}

func (p *brokerProbePlugin) record(failure string) {
	p.mu.Lock()
	p.failure = failure
	p.mu.Unlock()
}

// noHostServicesPlugin 模拟老插件：不实现 InitHostServices（返回 Unimplemented），
// 也不接收 broker。
type noHostServicesPlugin struct {
	pluginv1.UnimplementedTransportPluginServer
}

func dispenseTransportClient(t *testing.T, impl pluginv1.TransportPluginServer) *pluginv1.TransportClient {
	t.Helper()
	client, _ := hcplugin.TestPluginGRPCConn(t, false, map[string]hcplugin.Plugin{
		pluginv1.TransportPluginName: &pluginv1.GRPCPlugin{Impl: impl},
	})
	t.Cleanup(func() { _ = client.Close() })
	dispensed, err := client.Dispense(pluginv1.TransportPluginName)
	require.NoError(t, err)
	tc, ok := dispensed.(*pluginv1.TransportClient)
	require.True(t, ok)
	require.NotNil(t, tc.Broker)
	return tc
}

// 端到端验证 broker 反向通道：宿主服务 AcceptAndServe + 插件 Dial 回来，经真实 gRPC
// 完成 KV 往返，且写入落到宿主注入的 pluginKey 命名空间下。
func TestOfferPluginHostServices_BrokerRoundtrip(t *testing.T) {
	probe := &brokerProbePlugin{}
	tc := dispenseTransportClient(t, probe)

	store := newFakePluginKVStore()
	hostServer := newPluginHostServiceServer("test.plugin", store, nil, PluginAccountScope{})
	offerPluginHostServices(context.Background(), &PluginInstallation{PluginKey: "test.plugin"}, tc.TransportPluginClient, tc.Broker, hostServer, 5*time.Second)

	probe.mu.Lock()
	defer probe.mu.Unlock()
	require.Empty(t, probe.failure)
	require.True(t, probe.ready)
	assert.True(t, probe.getFound)
	assert.Equal(t, []byte("hello"), probe.getValue)

	stored, found, err := store.Get(context.Background(), "test.plugin", "state", "probe")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []byte("hello"), stored)
}

// 老插件不实现 InitHostServices 时，offerPluginHostServices 必须优雅降级、不 panic、
// 不写入任何状态。
func TestOfferPluginHostServices_UnimplementedIsGraceful(t *testing.T) {
	tc := dispenseTransportClient(t, &noHostServicesPlugin{})

	store := newFakePluginKVStore()
	hostServer := newPluginHostServiceServer("test.plugin", store, nil, PluginAccountScope{})
	require.NotPanics(t, func() {
		offerPluginHostServices(context.Background(), &PluginInstallation{PluginKey: "test.plugin"}, tc.TransportPluginClient, tc.Broker, hostServer, 5*time.Second)
	})

	_, found, err := store.Get(context.Background(), "test.plugin", "state", "probe")
	require.NoError(t, err)
	assert.False(t, found)
}

// hostServices 为 nil（未配置键值存储）时不得触发任何 broker 交互。
func TestOfferPluginHostServices_NilHostServicesNoop(t *testing.T) {
	tc := dispenseTransportClient(t, &noHostServicesPlugin{})
	require.NotPanics(t, func() {
		offerPluginHostServices(context.Background(), &PluginInstallation{PluginKey: "test.plugin"}, tc.TransportPluginClient, tc.Broker, nil, 5*time.Second)
	})
}

// versionedAccountProbe 模拟只接受指定版本的第三方插件，经真实反向 gRPC
// 查询账号，避免只验证重试次数而漏掉账号接口仍不可用的问题。
type versionedAccountProbe struct {
	pluginv1.UnimplementedTransportPluginServer
	broker          *hcplugin.GRPCBroker
	acceptedVersion uint32
	initErr         error
	mu              sync.Mutex
	versions        []uint32
	brokerIDs       []uint32
	accountIDs      []int64
}

func (p *versionedAccountProbe) SetHostBroker(broker *hcplugin.GRPCBroker) {
	p.broker = broker
}

func (p *versionedAccountProbe) InitHostServices(ctx context.Context, req *pluginv1.InitHostServicesRequest) (*pluginv1.InitHostServicesResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.versions = append(p.versions, req.HostServiceApiVersion)
	p.brokerIDs = append(p.brokerIDs, req.HostServiceId)
	if p.initErr != nil {
		return nil, p.initErr
	}
	if req.HostServiceApiVersion != p.acceptedVersion {
		return &pluginv1.InitHostServicesResponse{Message: "不支持此宿主接口版本"}, nil
	}
	conn, err := p.broker.Dial(req.HostServiceId)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	accounts, err := pluginv1.NewHostServiceClient(conn).ListAccounts(ctx, &pluginv1.ListAccountsRequest{
		Platform: "openai", AccountType: "oauth",
	})
	if err != nil {
		return nil, err
	}
	p.accountIDs = accounts.AccountIds
	return &pluginv1.InitHostServicesResponse{Ready: true}, nil
}

func TestOfferPluginHostServices_VersionNegotiation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		version  uint32
		initErr  error
		versions []uint32
		accounts []int64
	}{
		{name: "v2直接连接", version: 2, versions: []uint32{2}, accounts: []int64{7}},
		{name: "严格v1插件回退后能读取账号", version: 1, versions: []uint32{2, 1}, accounts: []int64{7}},
		{name: "两次拒绝后停止", version: 99, versions: []uint32{2, 1}},
		{name: "RPC失败不降级", initErr: status.Error(codes.Unavailable, "连接不可用"), versions: []uint32{2}},
		{name: "旧插件未实现不降级", initErr: status.Error(codes.Unimplemented, "未实现"), versions: []uint32{2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &versionedAccountProbe{acceptedVersion: tc.version, initErr: tc.initErr}
			client := dispenseTransportClient(t, probe)
			scope := newPluginAccountScope(pluginAccountScopeEntry{Platform: "openai", AccountType: "oauth"})
			directory := &fakeAccountDirectory{infos: []PluginAccountInfo{{ID: 7, Platform: "openai", AccountType: "oauth", Status: "active", Schedulable: true}}}
			host := newPluginHostServiceServer("test.plugin", newFakePluginKVStore(), directory, scope)
			offerPluginHostServices(context.Background(), &PluginInstallation{PluginKey: "test.plugin"}, client.TransportPluginClient, client.Broker, host, 5*time.Second)

			probe.mu.Lock()
			defer probe.mu.Unlock()
			require.Equal(t, tc.versions, probe.versions)
			assert.Equal(t, tc.accounts, probe.accountIDs)
			for _, id := range probe.brokerIDs {
				assert.Equal(t, probe.brokerIDs[0], id, "降级必须复用同一宿主服务与权限范围")
			}
			if len(tc.accounts) > 0 {
				assert.Equal(t, scope, directory.lastScope)
			}
		})
	}
}
