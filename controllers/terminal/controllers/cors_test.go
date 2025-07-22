package controllers

import (
	"testing"

	"github.com/labring/sealos/controllers/pkg/istio"
	terminalv1 "github.com/labring/sealos/controllers/terminal/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestTerminalCorsOrigins(t *testing.T) {
	tests := []struct {
		name           string
		tlsEnabled     bool
		baseDomain     string
		publicDomains  []string
		expectedOrigins []string
	}{
		{
			name:       "TLS enabled with single domain",
			tlsEnabled: true,
			baseDomain: "cloud.sealos.io",
			publicDomains: []string{"cloud.sealos.io"},
			expectedOrigins: []string{
				"https://terminal.cloud.sealos.io",
			},
		},
		{
			name:       "TLS enabled with wildcard domain",
			tlsEnabled: true,
			baseDomain: "cloud.sealos.io",
			publicDomains: []string{"*.cloud.sealos.io"},
			expectedOrigins: []string{
				"https://terminal.cloud.sealos.io",
			},
		},
		{
			name:       "TLS enabled with multiple domains",
			tlsEnabled: true,
			baseDomain: "cloud.sealos.io",
			publicDomains: []string{"cloud.sealos.io", "*.example.com", "custom.example.org"},
			expectedOrigins: []string{
				"https://terminal.cloud.sealos.io",
				"https://terminal.example.com",
				"https://terminal.custom.example.org",
			},
		},
		{
			name:       "TLS disabled",
			tlsEnabled: false,
			baseDomain: "cloud.sealos.io",
			publicDomains: []string{"cloud.sealos.io"},
			expectedOrigins: []string{
				"http://terminal.cloud.sealos.io",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test configuration
			config := &istio.NetworkConfig{
				BaseDomain:       tt.baseDomain,
				PublicDomains:    tt.publicDomains,
				TLSEnabled:       tt.tlsEnabled,
				DefaultGateway:   "istio-system/sealos-gateway",
				DefaultTLSSecret: "wildcard-cert",
			}

			// Create IstioNetworkingReconciler
			scheme := runtime.NewScheme()
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			reconciler := NewIstioNetworkingReconciler(fakeClient, config)

			// Build CORS origins
			origins := reconciler.buildCorsOrigins()

			// Verify expected origins
			if len(origins) != len(tt.expectedOrigins) {
				t.Errorf("Expected %d origins, got %d", len(tt.expectedOrigins), len(origins))
				t.Errorf("Origins: %v", origins)
				return
			}

			// Check each expected origin exists
			for _, expected := range tt.expectedOrigins {
				found := false
				for _, actual := range origins {
					if actual == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected origin %s not found in %v", expected, origins)
				}
			}

			// Verify no wildcard patterns
			for _, origin := range origins {
				if contains(origin, "*") {
					t.Errorf("Found wildcard in origin: %s", origin)
				}
			}
		})
	}
}

func TestTerminalNetworkingSpec(t *testing.T) {
	// Create test configuration
	config := &istio.NetworkConfig{
		BaseDomain:       "cloud.sealos.io",
		PublicDomains:    []string{"cloud.sealos.io", "*.example.com"},
		TLSEnabled:       true,
		DefaultTLSSecret: "wildcard-cert",
		DefaultGateway:   "istio-system/sealos-gateway",
	}

	// Create IstioNetworkingReconciler
	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	reconciler := NewIstioNetworkingReconciler(fakeClient, config)

	// Create test terminal
	terminal := &terminalv1.Terminal{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-terminal",
			Namespace: "ns-test",
		},
		Status: terminalv1.TerminalStatus{
			ServiceName:   "test-terminal-svc",
			SecretHeader:  "X-SEALOS-TEST",
		},
	}

	// Build networking spec
	spec := reconciler.buildNetworkingSpec(terminal, "test-hostname")

	// Verify CORS policy
	if spec.CorsPolicy == nil {
		t.Fatal("CORS policy should not be nil")
	}

	// Check CORS origins
	expectedOrigins := []string{
		"https://terminal.cloud.sealos.io",
		"https://terminal.example.com",
	}

	if len(spec.CorsPolicy.AllowOrigins) != len(expectedOrigins) {
		t.Errorf("Expected %d CORS origins, got %d", len(expectedOrigins), len(spec.CorsPolicy.AllowOrigins))
		t.Errorf("CORS Origins: %v", spec.CorsPolicy.AllowOrigins)
	}

	// Verify no wildcard patterns in CORS origins
	for _, origin := range spec.CorsPolicy.AllowOrigins {
		if contains(origin, "*") {
			t.Errorf("Found wildcard in CORS origin: %s", origin)
		}
	}

	// Verify WebSocket protocol
	if spec.Protocol != istio.ProtocolWebSocket {
		t.Errorf("Expected protocol to be WebSocket, got %s", spec.Protocol)
	}

	// Verify secret header
	if spec.SecretHeader != "X-SEALOS-TEST" {
		t.Errorf("Expected secret header to be X-SEALOS-TEST, got %s", spec.SecretHeader)
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && s[0:len(substr)] == substr || len(s) > len(substr) && s[len(s)-len(substr):] == substr || (len(substr) > 0 && len(s) > len(substr) && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}