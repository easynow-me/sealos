/*
Copyright 2025 labring.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package istio

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestOptimizedNetworkingManager_CreateAppNetworking(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	config := &NetworkConfig{
		BaseDomain:           "cloud.sealos.io",
		DefaultGateway:       "istio-system/sealos-gateway",
		TLSEnabled:           true,
		PublicDomains:        []string{"cloud.sealos.io"},
		PublicDomainPatterns: []string{"*.cloud.sealos.io"},
	}

	// Create mocks
	mockGatewayCtrl := &mockGatewayController{}
	mockVSCtrl := &mockVirtualServiceController{}
	mockCertMgr := &mockCertificateManager{}

	manager := &optimizedNetworkingManager{
		client:            client,
		scheme:            scheme,
		config:            config,
		gatewayController: mockGatewayCtrl,
		vsController:      mockVSCtrl,
		domainAllocator:   &mockDomainAllocator{},
		certManager:       mockCertMgr,
		domainClassifier:  NewDomainClassifier(config),
	}

	tests := []struct {
		name             string
		spec             *AppNetworkingSpec
		expectGateway    bool
		expectVS         bool
		expectError      bool
		testDescription  string
	}{
		{
			name: "public domain only - no gateway needed",
			spec: &AppNetworkingSpec{
				Name:        "app1",
				Namespace:   "ns1",
				TenantID:    "tenant1",
				AppName:     "testapp",
				Protocol:    ProtocolHTTP,
				Hosts:       []string{"app.cloud.sealos.io"},
				ServiceName: "app1-svc",
				ServicePort: 8080,
			},
			expectGateway:   false,
			expectVS:        true,
			expectError:     false,
			testDescription: "公共域名应该不创建Gateway，只创建VirtualService",
		},
		{
			name: "custom domain - needs gateway",
			spec: &AppNetworkingSpec{
				Name:        "app2",
				Namespace:   "ns2",
				TenantID:    "tenant2",
				AppName:     "testapp",
				Protocol:    ProtocolHTTP,
				Hosts:       []string{"custom.example.com"},
				ServiceName: "app2-svc",
				ServicePort: 8080,
				TLSConfig: &TLSConfig{
					SecretName: "custom-tls",
					Hosts:      []string{"custom.example.com"},
				},
			},
			expectGateway:   true,
			expectVS:        true,
			expectError:     false,
			testDescription: "自定义域名应该创建Gateway和VirtualService",
		},
		{
			name: "mixed domains - needs gateway",
			spec: &AppNetworkingSpec{
				Name:        "app3",
				Namespace:   "ns3",
				TenantID:    "tenant3",
				AppName:     "testapp",
				Protocol:    ProtocolHTTP,
				Hosts:       []string{"app.cloud.sealos.io", "custom.example.com"},
				ServiceName: "app3-svc",
				ServicePort: 8080,
				TLSConfig: &TLSConfig{
					SecretName: "custom-tls",
					Hosts:      []string{"custom.example.com"},
				},
			},
			expectGateway:   true,
			expectVS:        true,
			expectError:     false,
			testDescription: "混合域名应该创建Gateway（仅包含自定义域名）和VirtualService",
		},
		{
			name: "with owner reference",
			spec: &AppNetworkingSpec{
				Name:        "app4",
				Namespace:   "ns4",
				TenantID:    "tenant4",
				AppName:     "testapp",
				Protocol:    ProtocolHTTP,
				Hosts:       []string{"custom.example.com"},
				ServiceName: "app4-svc",
				ServicePort: 8080,
				TLSConfig: &TLSConfig{
					SecretName: "custom-tls",
					Hosts:      []string{"custom.example.com"},
				},
				OwnerObject: &mockOwner{name: "owner1", namespace: "ns4"},
			},
			expectGateway:   true,
			expectVS:        true,
			expectError:     false,
			testDescription: "带有OwnerObject的配置应该正确设置OwnerReference",
		},
		{
			name: "custom domain without TLS config",
			spec: &AppNetworkingSpec{
				Name:        "app5",
				Namespace:   "ns5",
				TenantID:    "tenant5",
				AppName:     "testapp",
				Protocol:    ProtocolHTTP,
				Hosts:       []string{"custom.example.com"},
				ServiceName: "app5-svc",
				ServicePort: 8080,
			},
			expectGateway:   false,
			expectVS:        false,
			expectError:     true,
			testDescription: "自定义域名没有TLS配置应该报错",
		},
		{
			name: "terminal app with websocket",
			spec: &AppNetworkingSpec{
				Name:         "terminal1",
				Namespace:    "ns6",
				TenantID:     "tenant6",
				AppName:      "terminal",
				Protocol:     ProtocolWebSocket,
				Hosts:        []string{"terminal.cloud.sealos.io"},
				ServiceName:  "terminal-svc",
				ServicePort:  8080,
				SecretHeader: "X-SEALOS-ABC123",
				Timeout:      &[]time.Duration{86400 * time.Second}[0],
			},
			expectGateway:   false,
			expectVS:        true,
			expectError:     false,
			testDescription: "Terminal应用使用WebSocket协议和安全头",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset mock states
			mockGatewayCtrl.reset()
			mockVSCtrl.reset()
			mockCertMgr.reset()

			err := manager.CreateAppNetworking(context.Background(), tt.spec)

			if tt.expectError && err == nil {
				t.Errorf("CreateAppNetworking() expected error but got nil. %s", tt.testDescription)
			}
			if !tt.expectError && err != nil {
				t.Errorf("CreateAppNetworking() unexpected error: %v. %s", err, tt.testDescription)
			}

			if tt.expectGateway && !mockGatewayCtrl.createOrUpdateCalled {
				t.Errorf("Expected Gateway to be created but it wasn't. %s", tt.testDescription)
			}
			if !tt.expectGateway && mockGatewayCtrl.createOrUpdateCalled {
				t.Errorf("Gateway was created but shouldn't be. %s", tt.testDescription)
			}

			if tt.expectVS && !mockVSCtrl.createOrUpdateCalled {
				t.Errorf("Expected VirtualService to be created but it wasn't. %s", tt.testDescription)
			}
			if !tt.expectVS && mockVSCtrl.createOrUpdateCalled {
				t.Errorf("VirtualService was created but shouldn't be. %s", tt.testDescription)
			}

			// 验证 owner reference 是否正确传递
			if tt.spec.OwnerObject != nil {
				if !mockGatewayCtrl.ownerSet && tt.expectGateway {
					t.Errorf("Expected Gateway OwnerReference to be set but it wasn't. %s", tt.testDescription)
				}
				if !mockVSCtrl.ownerSet && tt.expectVS {
					t.Errorf("Expected VirtualService OwnerReference to be set but it wasn't. %s", tt.testDescription)
				}
			}
		})
	}
}

func TestOptimizedNetworkingManager_DeleteAppNetworking(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	config := &NetworkConfig{
		BaseDomain:     "cloud.sealos.io",
		DefaultGateway: "istio-system/sealos-gateway",
	}

	mockGatewayCtrl := &mockGatewayController{}
	mockVSCtrl := &mockVirtualServiceController{}

	manager := &optimizedNetworkingManager{
		client:            client,
		scheme:            scheme,
		config:            config,
		gatewayController: mockGatewayCtrl,
		vsController:      mockVSCtrl,
		domainAllocator:   &mockDomainAllocator{},
		certManager:       &mockCertificateManager{},
		domainClassifier:  NewDomainClassifier(config),
	}

	tests := []struct {
		name              string
		appName           string
		namespace         string
		gatewayExists     bool
		expectVSDelete    bool
		expectGWDelete    bool
		testDescription   string
	}{
		{
			name:              "delete with existing gateway",
			appName:           "app1",
			namespace:         "ns1",
			gatewayExists:     true,
			expectVSDelete:    true,
			expectGWDelete:    true,
			testDescription:   "应该删除VirtualService和Gateway",
		},
		{
			name:              "delete without gateway",
			appName:           "app2",
			namespace:         "ns2",
			gatewayExists:     false,
			expectVSDelete:    true,
			expectGWDelete:    false,
			testDescription:   "只删除VirtualService，不尝试删除不存在的Gateway",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset and configure mocks
			mockGatewayCtrl.reset()
			mockVSCtrl.reset()
			mockGatewayCtrl.existsReturns = tt.gatewayExists

			err := manager.DeleteAppNetworking(context.Background(), tt.appName, tt.namespace)

			if err != nil {
				t.Errorf("DeleteAppNetworking() unexpected error: %v", err)
			}

			if tt.expectVSDelete && !mockVSCtrl.deleteCalled {
				t.Errorf("Expected VirtualService to be deleted but it wasn't. %s", tt.testDescription)
			}

			if tt.expectGWDelete && !mockGatewayCtrl.deleteCalled {
				t.Errorf("Expected Gateway to be deleted but it wasn't. %s", tt.testDescription)
			}

			if !tt.expectGWDelete && mockGatewayCtrl.deleteCalled {
				t.Errorf("Gateway was deleted but shouldn't be. %s", tt.testDescription)
			}
		})
	}
}

// Mock implementations

type mockGatewayController struct {
	createCalled         bool
	createOrUpdateCalled bool
	deleteCalled         bool
	existsReturns        bool
	ownerSet             bool
	lastConfig           *GatewayConfig
}

func (m *mockGatewayController) reset() {
	m.createCalled = false
	m.createOrUpdateCalled = false
	m.deleteCalled = false
	m.existsReturns = false
	m.ownerSet = false
	m.lastConfig = nil
}

func (m *mockGatewayController) Create(ctx context.Context, config *GatewayConfig) error {
	m.createCalled = true
	m.createOrUpdateCalled = true
	m.lastConfig = config
	return nil
}

func (m *mockGatewayController) Update(ctx context.Context, config *GatewayConfig) error {
	return nil
}

func (m *mockGatewayController) Delete(ctx context.Context, name, namespace string) error {
	m.deleteCalled = true
	return nil
}

func (m *mockGatewayController) Get(ctx context.Context, name, namespace string) (*Gateway, error) {
	return nil, nil
}

func (m *mockGatewayController) Exists(ctx context.Context, name, namespace string) (bool, error) {
	return m.existsReturns, nil
}

func (m *mockGatewayController) CreateOrUpdateWithOwner(ctx context.Context, config *GatewayConfig, owner metav1.Object, scheme *runtime.Scheme) error {
	m.createOrUpdateCalled = true
	m.lastConfig = config
	if owner != nil {
		m.ownerSet = true
	}
	return nil
}

type mockVirtualServiceController struct {
	createCalled         bool
	createOrUpdateCalled bool
	deleteCalled         bool
	ownerSet             bool
	lastConfig           *VirtualServiceConfig
}

func (m *mockVirtualServiceController) reset() {
	m.createCalled = false
	m.createOrUpdateCalled = false
	m.deleteCalled = false
	m.ownerSet = false
	m.lastConfig = nil
}

func (m *mockVirtualServiceController) Create(ctx context.Context, config *VirtualServiceConfig) error {
	m.createCalled = true
	m.createOrUpdateCalled = true
	m.lastConfig = config
	return nil
}

func (m *mockVirtualServiceController) Update(ctx context.Context, config *VirtualServiceConfig) error {
	return nil
}

func (m *mockVirtualServiceController) Delete(ctx context.Context, name, namespace string) error {
	m.deleteCalled = true
	return nil
}

func (m *mockVirtualServiceController) Get(ctx context.Context, name, namespace string) (*VirtualService, error) {
	return nil, nil
}

func (m *mockVirtualServiceController) Suspend(ctx context.Context, name, namespace string) error {
	return nil
}

func (m *mockVirtualServiceController) Resume(ctx context.Context, name, namespace string) error {
	return nil
}

func (m *mockVirtualServiceController) CreateOrUpdateWithOwner(ctx context.Context, config *VirtualServiceConfig, owner metav1.Object, scheme *runtime.Scheme) error {
	m.createOrUpdateCalled = true
	m.lastConfig = config
	if owner != nil {
		m.ownerSet = true
	}
	return nil
}

type mockCertificateManager struct {
	createOrUpdateCalled bool
	deleteCalled         bool
}

func (m *mockCertificateManager) reset() {
	m.createOrUpdateCalled = false
	m.deleteCalled = false
}

func (m *mockCertificateManager) CreateOrUpdate(ctx context.Context, host, namespace string) error {
	m.createOrUpdateCalled = true
	return nil
}

func (m *mockCertificateManager) Delete(ctx context.Context, host, namespace string) error {
	m.deleteCalled = true
	return nil
}

func (m *mockCertificateManager) CheckExpiration(ctx context.Context, secretName string, namespace string) (time.Time, error) {
	// 返回一个未来的时间，表示证书尚未过期
	return time.Now().Add(30 * 24 * time.Hour), nil
}

func (m *mockCertificateManager) Rotate(ctx context.Context, secretName string, namespace string) error {
	return nil
}

type mockDomainAllocator struct {
	validateReturns error
}

func (m *mockDomainAllocator) GenerateAppDomain(tenantID, appName string) string {
	return fmt.Sprintf("%s.%s.cloud.sealos.io", appName, tenantID)
}

func (m *mockDomainAllocator) ValidateCustomDomain(domain string) error {
	// 测试环境不进行真实的DNS验证
	return m.validateReturns
}

func (m *mockDomainAllocator) IsDomainAvailable(domain string) (bool, error) {
	return true, nil
}