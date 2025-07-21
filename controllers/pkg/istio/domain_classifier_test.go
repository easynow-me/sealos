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
	"strings"
	"testing"
)

func TestDomainClassifier_IsPublicDomain(t *testing.T) {
	tests := []struct {
		name           string
		config         *NetworkConfig
		host           string
		expectedPublic bool
	}{
		{
			name: "exact match base domain",
			config: &NetworkConfig{
				BaseDomain: "cloud.sealos.io",
			},
			host:           "cloud.sealos.io",
			expectedPublic: true,
		},
		{
			name: "subdomain of base domain",
			config: &NetworkConfig{
				BaseDomain: "cloud.sealos.io",
			},
			host:           "app.cloud.sealos.io",
			expectedPublic: true,
		},
		{
			name: "public domain from list",
			config: &NetworkConfig{
				BaseDomain:    "example.com",
				PublicDomains: []string{"cloud.sealos.io", "sealos.io"},
			},
			host:           "cloud.sealos.io",
			expectedPublic: true,
		},
		{
			name: "wildcard pattern match",
			config: &NetworkConfig{
				BaseDomain:           "example.com",
				PublicDomainPatterns: []string{"*.cloud.sealos.io"},
			},
			host:           "app.cloud.sealos.io",
			expectedPublic: true,
		},
		{
			name: "wildcard pattern exact base match",
			config: &NetworkConfig{
				BaseDomain:           "example.com",
				PublicDomainPatterns: []string{"*.cloud.sealos.io"},
			},
			host:           "cloud.sealos.io",
			expectedPublic: true,
		},
		{
			name: "custom domain not in public list",
			config: &NetworkConfig{
				BaseDomain:    "cloud.sealos.io",
				PublicDomains: []string{"public.example.com"},
			},
			host:           "custom.domain.com",
			expectedPublic: false,
		},
		{
			name: "reserved domain treated as public",
			config: &NetworkConfig{
				BaseDomain:      "cloud.sealos.io",
				ReservedDomains: []string{"reserved.sealos.io"},
			},
			host:           "reserved.sealos.io",
			expectedPublic: true,
		},
		{
			name:           "empty host",
			config:         &NetworkConfig{BaseDomain: "cloud.sealos.io"},
			host:           "",
			expectedPublic: false,
		},
		{
			name: "case insensitive match",
			config: &NetworkConfig{
				BaseDomain: "cloud.sealos.io",
			},
			host:           "APP.CLOUD.SEALOS.IO",
			expectedPublic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dc := NewDomainClassifier(tt.config)
			result := dc.IsPublicDomain(tt.host)
			if result != tt.expectedPublic {
				t.Errorf("IsPublicDomain(%s) = %v, want %v", tt.host, result, tt.expectedPublic)
			}
		})
	}
}

func TestDomainClassifier_ClassifyHosts(t *testing.T) {
	config := &NetworkConfig{
		BaseDomain:           "cloud.sealos.io",
		PublicDomains:        []string{"api.sealos.io"},
		PublicDomainPatterns: []string{"*.public.sealos.io"},
	}

	dc := NewDomainClassifier(config)

	tests := []struct {
		name                 string
		hosts                []string
		expectedPublicCount  int
		expectedCustomCount  int
		expectedHasPublic    bool
		expectedHasCustom    bool
		expectedAllPublic    bool
	}{
		{
			name:                "all public domains",
			hosts:               []string{"cloud.sealos.io", "app.cloud.sealos.io", "api.sealos.io"},
			expectedPublicCount: 3,
			expectedCustomCount: 0,
			expectedHasPublic:   true,
			expectedHasCustom:   false,
			expectedAllPublic:   true,
		},
		{
			name:                "all custom domains",
			hosts:               []string{"custom1.com", "custom2.org"},
			expectedPublicCount: 0,
			expectedCustomCount: 2,
			expectedHasPublic:   false,
			expectedHasCustom:   true,
			expectedAllPublic:   false,
		},
		{
			name:                "mixed domains",
			hosts:               []string{"cloud.sealos.io", "custom.com", "app.public.sealos.io"},
			expectedPublicCount: 2,
			expectedCustomCount: 1,
			expectedHasPublic:   true,
			expectedHasCustom:   true,
			expectedAllPublic:   false,
		},
		{
			name:                "empty hosts",
			hosts:               []string{},
			expectedPublicCount: 0,
			expectedCustomCount: 0,
			expectedHasPublic:   false,
			expectedHasCustom:   false,
			expectedAllPublic:   true, // 空列表被认为是"全部公共"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dc.ClassifyHosts(tt.hosts)
			
			if len(result.PublicHosts) != tt.expectedPublicCount {
				t.Errorf("PublicHosts count = %d, want %d", len(result.PublicHosts), tt.expectedPublicCount)
			}
			if len(result.CustomHosts) != tt.expectedCustomCount {
				t.Errorf("CustomHosts count = %d, want %d", len(result.CustomHosts), tt.expectedCustomCount)
			}
			if (len(result.PublicHosts) > 0) != tt.expectedHasPublic {
				t.Errorf("HasPublicDomain = %v, want %v", len(result.PublicHosts) > 0, tt.expectedHasPublic)
			}
			if (len(result.CustomHosts) > 0) != tt.expectedHasCustom {
				t.Errorf("HasCustomDomain = %v, want %v", len(result.CustomHosts) > 0, tt.expectedHasCustom)
			}
			if result.AllPublic != tt.expectedAllPublic {
				t.Errorf("AllPublic = %v, want %v", result.AllPublic, tt.expectedAllPublic)
			}
		})
	}
}

func TestDomainClassifier_GetGatewayReference(t *testing.T) {
	config := &NetworkConfig{
		BaseDomain:     "cloud.sealos.io",
		DefaultGateway: "istio-system/sealos-gateway",
	}

	dc := NewDomainClassifier(config)

	tests := []struct {
		name     string
		spec     *AppNetworkingSpec
		expected string
	}{
		{
			name: "public domain uses system gateway",
			spec: &AppNetworkingSpec{
				Name:      "app1",
				Namespace: "ns1",
				Hosts:     []string{"app.cloud.sealos.io"},
			},
			expected: "istio-system/sealos-gateway",
		},
		{
			name: "custom domain uses user gateway",
			spec: &AppNetworkingSpec{
				Name:      "app2",
				Namespace: "ns2",
				Hosts:     []string{"custom.domain.com"},
			},
			expected: "ns2/app2-gateway",
		},
		{
			name: "mixed domains uses user gateway",
			spec: &AppNetworkingSpec{
				Name:      "app3",
				Namespace: "ns3",
				Hosts:     []string{"app.cloud.sealos.io", "custom.domain.com"},
			},
			expected: "ns3/app3-gateway",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dc.GetGatewayReference(tt.spec)
			if result != tt.expected {
				t.Errorf("GetGatewayReference() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestDomainClassifier_BuildOptimizedGatewayConfig(t *testing.T) {
	config := &NetworkConfig{
		BaseDomain:       "cloud.sealos.io",
		DefaultGateway:   "istio-system/sealos-gateway",
		DefaultTLSSecret: "wildcard-cert",
		TLSEnabled:       true,
	}

	dc := NewDomainClassifier(config)

	tests := []struct {
		name      string
		spec      *AppNetworkingSpec
		expectNil bool
	}{
		{
			name: "all public domains returns nil",
			spec: &AppNetworkingSpec{
				Name:      "app1",
				Namespace: "ns1",
				Hosts:     []string{"app.cloud.sealos.io", "api.cloud.sealos.io"},
			},
			expectNil: true,
		},
		{
			name: "custom domains returns gateway config",
			spec: &AppNetworkingSpec{
				Name:      "app2",
				Namespace: "ns2",
				Hosts:     []string{"custom1.com", "custom2.com"},
				TLSConfig: &TLSConfig{
					SecretName: "custom-tls",
					Hosts:      []string{"custom1.com", "custom2.com"},
				},
			},
			expectNil: false,
		},
		{
			name: "mixed domains returns only custom domains in gateway",
			spec: &AppNetworkingSpec{
				Name:      "app3",
				Namespace: "ns3",
				Hosts:     []string{"app.cloud.sealos.io", "custom.com"},
				TLSConfig: &TLSConfig{
					SecretName: "custom-tls",
					Hosts:      []string{"app.cloud.sealos.io", "custom.com"},
				},
			},
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dc.BuildOptimizedGatewayConfig(tt.spec)
			
			if tt.expectNil {
				if result != nil {
					t.Errorf("BuildOptimizedGatewayConfig() = %v, want nil", result)
				}
			} else {
				if result == nil {
					t.Fatal("BuildOptimizedGatewayConfig() = nil, want non-nil")
				}
				if result.Name != tt.spec.Name+"-gateway" {
					t.Errorf("Gateway name = %s, want %s", result.Name, tt.spec.Name+"-gateway")
				}
				if result.Namespace != tt.spec.Namespace {
					t.Errorf("Gateway namespace = %s, want %s", result.Namespace, tt.spec.Namespace)
				}
			}
		})
	}
}

func TestDomainClassifier_BuildOptimizedVirtualServiceConfig(t *testing.T) {
	config := &NetworkConfig{
		BaseDomain:     "cloud.sealos.io",
		DefaultGateway: "istio-system/sealos-gateway",
	}

	dc := NewDomainClassifier(config)

	tests := []struct {
		name             string
		spec             *AppNetworkingSpec
		expectedGateways []string
		expectedLabels   map[string]string
	}{
		{
			name: "public domains use system gateway",
			spec: &AppNetworkingSpec{
				Name:        "app1",
				Namespace:   "ns1",
				Hosts:       []string{"app.cloud.sealos.io"},
				ServiceName: "app1-svc",
				ServicePort: 8080,
				Protocol:    ProtocolHTTP,
			},
			expectedGateways: []string{"istio-system/sealos-gateway"},
			expectedLabels: map[string]string{
				"app.kubernetes.io/name":        "app1",
				"app.kubernetes.io/managed-by":  "sealos-istio",
				"network.sealos.io/gateway-type": "shared",
			},
		},
		{
			name: "custom domains use user gateway",
			spec: &AppNetworkingSpec{
				Name:        "app2",
				Namespace:   "ns2",
				Hosts:       []string{"custom.com"},
				ServiceName: "app2-svc",
				ServicePort: 8080,
				Protocol:    ProtocolHTTP,
			},
			expectedGateways: []string{"ns2/app2-gateway"},
			expectedLabels: map[string]string{
				"app.kubernetes.io/name":        "app2",
				"app.kubernetes.io/managed-by":  "sealos-istio",
				"network.sealos.io/gateway-type": "dedicated",
			},
		},
		{
			name: "mixed domains use both gateways",
			spec: &AppNetworkingSpec{
				Name:        "app3",
				Namespace:   "ns3",
				Hosts:       []string{"app.cloud.sealos.io", "custom.com"},
				ServiceName: "app3-svc",
				ServicePort: 8080,
				Protocol:    ProtocolHTTP,
			},
			expectedGateways: []string{"istio-system/sealos-gateway", "ns3/app3-gateway"},
			expectedLabels: map[string]string{
				"app.kubernetes.io/name":        "app3",
				"app.kubernetes.io/managed-by":  "sealos-istio",
				"network.sealos.io/gateway-type": "mixed",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dc.BuildOptimizedVirtualServiceConfig(tt.spec)
			
			if result == nil {
				t.Fatal("BuildOptimizedVirtualServiceConfig() = nil, want non-nil")
			}
			
			// 验证基本属性
			if result.Name != tt.spec.Name+"-vs" {
				t.Errorf("VirtualService name = %s, want %s", result.Name, tt.spec.Name+"-vs")
			}
			
			// 验证网关引用
			if len(result.Gateways) != len(tt.expectedGateways) {
				t.Errorf("Gateways count = %d, want %d", len(result.Gateways), len(tt.expectedGateways))
			}
			for i, gw := range tt.expectedGateways {
				if i >= len(result.Gateways) || result.Gateways[i] != gw {
					t.Errorf("Gateway[%d] = %s, want %s", i, result.Gateways[i], gw)
				}
			}
			
			// 验证标签
			for k, v := range tt.expectedLabels {
				if result.Labels[k] != v {
					t.Errorf("Label[%s] = %s, want %s", k, result.Labels[k], v)
				}
			}
		})
	}
}

func TestDomainClassifier_ValidateCustomDomainCertificates(t *testing.T) {
	config := &NetworkConfig{
		BaseDomain: "cloud.sealos.io",
	}

	dc := NewDomainClassifier(config)

	tests := []struct {
		name        string
		spec        *AppNetworkingSpec
		expectError bool
		errorMsg    string
	}{
		{
			name: "no custom domains, no error",
			spec: &AppNetworkingSpec{
				Hosts: []string{"app.cloud.sealos.io"},
			},
			expectError: false,
		},
		{
			name: "custom domains without TLS config",
			spec: &AppNetworkingSpec{
				Hosts: []string{"custom.com"},
			},
			expectError: true,
			errorMsg:    "TLS configuration is required",
		},
		{
			name: "custom domains with empty secret name",
			spec: &AppNetworkingSpec{
				Hosts: []string{"custom.com"},
				TLSConfig: &TLSConfig{
					SecretName: "",
					Hosts:      []string{"custom.com"},
				},
			},
			expectError: true,
			errorMsg:    "TLS secret name is required",
		},
		{
			name: "custom domains with mismatched hosts",
			spec: &AppNetworkingSpec{
				Hosts: []string{"custom1.com", "custom2.com"},
				TLSConfig: &TLSConfig{
					SecretName: "tls-secret",
					Hosts:      []string{"custom1.com"},
				},
			},
			expectError: true,
			errorMsg:    "TLS hosts must cover all custom domains",
		},
		{
			name: "valid custom domains with TLS",
			spec: &AppNetworkingSpec{
				Hosts: []string{"custom.com"},
				TLSConfig: &TLSConfig{
					SecretName: "tls-secret",
					Hosts:      []string{"custom.com"},
				},
			},
			expectError: false,
		},
		{
			name: "mixed domains with valid TLS for custom",
			spec: &AppNetworkingSpec{
				Hosts: []string{"app.cloud.sealos.io", "custom.com"},
				TLSConfig: &TLSConfig{
					SecretName: "tls-secret",
					Hosts:      []string{"custom.com"},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := dc.ValidateCustomDomainCertificates(tt.spec)
			
			if tt.expectError {
				if err == nil {
					t.Error("ValidateCustomDomainCertificates() = nil, want error")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("ValidateCustomDomainCertificates() error = %v, want containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateCustomDomainCertificates() = %v, want nil", err)
				}
			}
		})
	}
}