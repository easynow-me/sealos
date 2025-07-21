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
	"regexp"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestUniversalIstioNetworkingHelper_GetOptimalDomain(t *testing.T) {
	config := &NetworkConfig{
		BaseDomain: "cloud.sealos.io",
		DomainTemplates: map[string]string{
			"terminal": "terminal-{{.Hash}}.{{.TenantID}}.{{.BaseDomain}}",
			"database": "db-{{.Hash}}.{{.TenantID}}.{{.BaseDomain}}",
		},
	}

	tests := []struct {
		name            string
		helper          *UniversalIstioNetworkingHelper
		params          *AppNetworkingParams
		expectedPattern string // 使用正则表达式匹配模式
		exactMatch      bool   // 是否精确匹配
		expectedDomain  string // 精确匹配时使用
	}{
		{
			name: "use custom domain if provided",
			helper: &UniversalIstioNetworkingHelper{
				config:  config,
				appType: "terminal",
			},
			params: &AppNetworkingParams{
				Name:         "test-app",
				Namespace:    "ns-test",
				CustomDomain: "custom.example.com",
			},
			exactMatch:     true,
			expectedDomain: "custom.example.com",
		},
		{
			name: "use hosts if provided",
			helper: &UniversalIstioNetworkingHelper{
				config:  config,
				appType: "terminal",
			},
			params: &AppNetworkingParams{
				Name:      "test-app",
				Namespace: "ns-test",
				Hosts:     []string{"host1.example.com", "host2.example.com"},
			},
			exactMatch:     true,
			expectedDomain: "host1.example.com",
		},
		{
			name: "generate terminal domain",
			helper: &UniversalIstioNetworkingHelper{
				config:  config,
				appType: "terminal",
			},
			params: &AppNetworkingParams{
				Name:      "test-terminal",
				Namespace: "ns-user123",
				AppType:   "terminal",
			},
			expectedPattern: `^terminal-test-terminal-[a-f0-9]{6}\.user123\.cloud\.sealos\.io$`,
		},
		{
			name: "generate database domain",
			helper: &UniversalIstioNetworkingHelper{
				config:  config,
				appType: "adminer",
			},
			params: &AppNetworkingParams{
				Name:      "test-db",
				Namespace: "ns-user456",
				AppType:   "adminer",
			},
			expectedPattern: `^db-test-db-[a-f0-9]{6}\.user456\.cloud\.sealos\.io$`,
		},
		{
			name: "extract tenant ID from namespace",
			helper: &UniversalIstioNetworkingHelper{
				config:  config,
				appType: "terminal",
			},
			params: &AppNetworkingParams{
				Name:      "myapp",
				Namespace: "ns-tenant123-extra",
				AppType:   "terminal",
			},
			expectedPattern: `^terminal-myapp-[a-f0-9]{6}\.tenant123-extra\.cloud\.sealos\.io$`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domain := tt.helper.GetOptimalDomain(tt.params)
			
			if tt.exactMatch {
				if domain != tt.expectedDomain {
					t.Errorf("GetOptimalDomain() = %s, want %s", domain, tt.expectedDomain)
				}
			} else {
				matched, err := regexp.MatchString(tt.expectedPattern, domain)
				if err != nil {
					t.Fatalf("Invalid regex pattern: %v", err)
				}
				if !matched {
					t.Errorf("GetOptimalDomain() = %s, want to match pattern %s", domain, tt.expectedPattern)
				}
			}
		})
	}
}

func TestUniversalIstioNetworkingHelper_AnalyzeDomainRequirements(t *testing.T) {
	config := &NetworkConfig{
		BaseDomain:     "cloud.sealos.io",
		DefaultGateway: "istio-system/sealos-gateway",
		TLSEnabled:     true,
		PublicDomains:  []string{"cloud.sealos.io"},
		PublicDomainPatterns: []string{"*.cloud.sealos.io"},
	}

	helper := NewUniversalIstioNetworkingHelper(nil, config, "terminal")

	tests := []struct {
		name     string
		params   *AppNetworkingParams
		expected *DomainAnalysis
	}{
		{
			name: "public domain analysis",
			params: &AppNetworkingParams{
				Name:      "test-app",
				Namespace: "ns-test",
				Hosts:     []string{"app.cloud.sealos.io"},
			},
			expected: &DomainAnalysis{
				Domain:           "app.cloud.sealos.io",
				IsPublicDomain:   true,
				NeedsGateway:     false,
				UseSystemGateway: true,
				GatewayReference: "istio-system/sealos-gateway",
			},
		},
		{
			name: "custom domain analysis",
			params: &AppNetworkingParams{
				Name:         "test-app",
				Namespace:    "ns-test",
				CustomDomain: "custom.example.com",
			},
			expected: &DomainAnalysis{
				Domain:           "custom.example.com",
				IsPublicDomain:   false,
				NeedsGateway:     true,
				UseSystemGateway: false,
				GatewayReference: "ns-test/test-app-gateway",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := helper.AnalyzeDomainRequirements(tt.params)
			
			if result.Domain != tt.expected.Domain {
				t.Errorf("Domain = %s, want %s", result.Domain, tt.expected.Domain)
			}
			if result.IsPublicDomain != tt.expected.IsPublicDomain {
				t.Errorf("IsPublicDomain = %v, want %v", result.IsPublicDomain, tt.expected.IsPublicDomain)
			}
			if result.NeedsGateway != tt.expected.NeedsGateway {
				t.Errorf("NeedsGateway = %v, want %v", result.NeedsGateway, tt.expected.NeedsGateway)
			}
			if result.UseSystemGateway != tt.expected.UseSystemGateway {
				t.Errorf("UseSystemGateway = %v, want %v", result.UseSystemGateway, tt.expected.UseSystemGateway)
			}
			if result.GatewayReference != tt.expected.GatewayReference {
				t.Errorf("GatewayReference = %s, want %s", result.GatewayReference, tt.expected.GatewayReference)
			}
		})
	}
}

func TestUniversalIstioNetworkingHelper_CreateOrUpdateNetworking(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	config := &NetworkConfig{
		BaseDomain:     "cloud.sealos.io",
		DefaultGateway: "istio-system/sealos-gateway",
		TLSEnabled:     true,
		PublicDomains:  []string{"cloud.sealos.io"},
		PublicDomainPatterns: []string{"*.cloud.sealos.io"},
	}

	// 创建一个 mock NetworkingManager
	mockManager := &mockNetworkingManager{
		createCalled: false,
		updateCalled: false,
	}

	helper := &UniversalIstioNetworkingHelper{
		client:            client,
		networkingManager: mockManager,
		domainClassifier:  NewDomainClassifier(config),
		config:            config,
		appType:           "terminal",
	}

	tests := []struct {
		name         string
		params       *AppNetworkingParams
		expectCreate bool
		expectUpdate bool
		expectError  bool
	}{
		{
			name: "create new networking",
			params: &AppNetworkingParams{
				Name:        "test-app",
				Namespace:   "ns-test",
				ServiceName: "test-svc",
				ServicePort: 8080,
				Protocol:    ProtocolHTTP,
			},
			expectCreate: true,
			expectUpdate: false,
			expectError:  false,
		},
		{
			name: "create with owner reference",
			params: &AppNetworkingParams{
				Name:        "test-app",
				Namespace:   "ns-test",
				ServiceName: "test-svc",
				ServicePort: 8080,
				Protocol:    ProtocolHTTP,
				OwnerObject: &mockOwner{name: "test-owner", namespace: "ns-test"},
			},
			expectCreate: true,
			expectUpdate: false,
			expectError:  false,
		},
		{
			name: "create with timeout and cors",
			params: &AppNetworkingParams{
				Name:        "test-app",
				Namespace:   "ns-test",
				ServiceName: "test-svc",
				ServicePort: 8080,
				Protocol:    ProtocolHTTP,
				Timeout:     &[]time.Duration{30 * time.Second}[0],
				CorsPolicy: &CorsPolicy{
					AllowOrigins: []string{"https://example.com"},
					AllowMethods: []string{"GET", "POST"},
				},
			},
			expectCreate: true,
			expectUpdate: false,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset mock state
			mockManager.createCalled = false
			mockManager.updateCalled = false
			mockManager.existsReturns = false

			err := helper.CreateOrUpdateNetworking(context.Background(), tt.params)

			if tt.expectError && err == nil {
				t.Error("CreateOrUpdateNetworking() expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("CreateOrUpdateNetworking() unexpected error: %v", err)
			}
			if tt.expectCreate && !mockManager.createCalled {
				t.Error("Expected CreateAppNetworking to be called")
			}
			if tt.expectUpdate && !mockManager.updateCalled {
				t.Error("Expected UpdateAppNetworking to be called")
			}
		})
	}
}

// Mock implementations for testing

type mockNetworkingManager struct {
	createCalled  bool
	updateCalled  bool
	existsReturns bool
	lastSpec      *AppNetworkingSpec
}

func (m *mockNetworkingManager) CreateAppNetworking(ctx context.Context, spec *AppNetworkingSpec) error {
	m.createCalled = true
	m.lastSpec = spec
	return nil
}

func (m *mockNetworkingManager) UpdateAppNetworking(ctx context.Context, spec *AppNetworkingSpec) error {
	m.updateCalled = true
	m.lastSpec = spec
	return nil
}

func (m *mockNetworkingManager) DeleteAppNetworking(ctx context.Context, name, namespace string) error {
	return nil
}

func (m *mockNetworkingManager) SuspendNetworking(ctx context.Context, namespace string) error {
	return nil
}

func (m *mockNetworkingManager) ResumeNetworking(ctx context.Context, namespace string) error {
	return nil
}

func (m *mockNetworkingManager) GetNetworkingStatus(ctx context.Context, name, namespace string) (*NetworkingStatus, error) {
	if m.existsReturns {
		return &NetworkingStatus{VirtualServiceReady: true}, nil
	}
	return nil, fmt.Errorf("not found")
}

type mockOwner struct {
	name      string
	namespace string
}

func (m *mockOwner) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind
}

func (m *mockOwner) DeepCopyObject() runtime.Object {
	return m
}

func (m *mockOwner) GetNamespace() string {
	return m.namespace
}

func (m *mockOwner) SetNamespace(namespace string) {
	m.namespace = namespace
}

func (m *mockOwner) GetName() string {
	return m.name
}

func (m *mockOwner) SetName(name string) {
	m.name = name
}

func (m *mockOwner) GetGenerateName() string {
	return ""
}

func (m *mockOwner) SetGenerateName(name string) {}

func (m *mockOwner) GetUID() types.UID {
	return "test-uid"
}

func (m *mockOwner) SetUID(uid types.UID) {}

func (m *mockOwner) GetResourceVersion() string {
	return "1"
}

func (m *mockOwner) SetResourceVersion(version string) {}

func (m *mockOwner) GetGeneration() int64 {
	return 1
}

func (m *mockOwner) SetGeneration(generation int64) {}

func (m *mockOwner) GetSelfLink() string {
	return ""
}

func (m *mockOwner) SetSelfLink(selfLink string) {}

func (m *mockOwner) GetCreationTimestamp() metav1.Time {
	return metav1.Time{}
}

func (m *mockOwner) SetCreationTimestamp(timestamp metav1.Time) {}

func (m *mockOwner) GetDeletionTimestamp() *metav1.Time {
	return nil
}

func (m *mockOwner) SetDeletionTimestamp(timestamp *metav1.Time) {}

func (m *mockOwner) GetDeletionGracePeriodSeconds() *int64 {
	return nil
}

func (m *mockOwner) SetDeletionGracePeriodSeconds(gracePeriodSeconds *int64) {}

func (m *mockOwner) GetLabels() map[string]string {
	return nil
}

func (m *mockOwner) SetLabels(labels map[string]string) {}

func (m *mockOwner) GetAnnotations() map[string]string {
	return nil
}

func (m *mockOwner) SetAnnotations(annotations map[string]string) {}

func (m *mockOwner) GetFinalizers() []string {
	return nil
}

func (m *mockOwner) SetFinalizers(finalizers []string) {}

func (m *mockOwner) GetOwnerReferences() []metav1.OwnerReference {
	return nil
}

func (m *mockOwner) SetOwnerReferences([]metav1.OwnerReference) {}

func (m *mockOwner) GetManagedFields() []metav1.ManagedFieldsEntry {
	return nil
}

func (m *mockOwner) SetManagedFields(managedFields []metav1.ManagedFieldsEntry) {}