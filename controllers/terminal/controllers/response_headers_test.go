package controllers

import (
	"testing"

	"github.com/labring/sealos/controllers/pkg/config"
	"github.com/labring/sealos/controllers/pkg/istio"
	terminalv1 "github.com/labring/sealos/controllers/terminal/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestTerminalSecurityResponseHeaders(t *testing.T) {
	// Create test configuration
	istioConfig := &istio.NetworkConfig{
		BaseDomain:       "cloud.sealos.io",
		DefaultGateway:   "istio-system/sealos-gateway",
		PublicDomains:    []string{"cloud.sealos.io"},
		TLSEnabled:       true,
		DefaultTLSSecret: "wildcard-cert",
	}

	// Create fake client
	scheme := runtime.NewScheme()
	_ = terminalv1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	t.Run("TerminalReconciler SecurityHeaders", func(t *testing.T) {
		// Mock TerminalReconciler with minimal setup
		reconciler := &TerminalReconciler{
			Client: fakeClient,
			CtrConfig: &Config{
				Global: config.Global{
					CloudDomain: "cloud.sealos.io",
					CloudPort:   "443",
				},
			},
			istioReconciler: NewIstioNetworkingReconciler(fakeClient, istioConfig),
		}

		// Build security headers
		headers := reconciler.buildSecurityResponseHeaders()

		// Verify headers are not nil
		if headers == nil {
			t.Fatal("Response headers should not be nil")
		}

		// Check for security headers
		expectedHeaders := map[string]string{
			"X-Frame-Options":         "SAMEORIGIN",
			"X-Content-Type-Options":  "nosniff",
			"X-XSS-Protection":        "1; mode=block",
			"Referrer-Policy":         "strict-origin-when-cross-origin",
			"Content-Security-Policy": "default-src 'self'; connect-src 'self' wss:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline' 'unsafe-eval';",
		}

		for key, expectedValue := range expectedHeaders {
			if actualValue, exists := headers[key]; !exists {
				t.Errorf("Security header %s should be present", key)
			} else if actualValue != expectedValue {
				t.Errorf("Security header %s should be '%s', got '%s'", key, expectedValue, actualValue)
			}
		}
	})

	t.Run("IstioNetworkingReconciler SecurityHeaders", func(t *testing.T) {
		reconciler := NewIstioNetworkingReconciler(fakeClient, istioConfig)

		// Create test terminal
		terminal := &terminalv1.Terminal{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-terminal",
				Namespace: "test-namespace",
			},
			Status: terminalv1.TerminalStatus{
				ServiceName:   "test-terminal-svc",
				SecretHeader:  "X-SEALOS-TEST",
			},
		}

		// Build networking spec
		spec := reconciler.buildNetworkingSpec(terminal, "test-hostname")

		// Verify that response headers are set
		if spec.ResponseHeaders == nil {
			t.Fatal("Response headers should not be nil")
		}

		// Check for security headers
		expectedHeaders := []string{
			"X-Frame-Options",
			"X-Content-Type-Options",
			"X-XSS-Protection",
			"Referrer-Policy",
			"Content-Security-Policy",
		}

		for _, header := range expectedHeaders {
			if _, exists := spec.ResponseHeaders[header]; !exists {
				t.Errorf("Security header %s should be present in ResponseHeaders", header)
			}
		}

		// Verify CSP contains WebSocket support
		csp := spec.ResponseHeaders["Content-Security-Policy"]
		if csp == "" {
			t.Error("Content-Security-Policy should not be empty")
		}
		if !contains(csp, "wss:") {
			t.Error("CSP should include WebSocket (wss:) support")
		}
	})
}

func TestTerminalGatewaySelection(t *testing.T) {
	// Create test configuration
	istioConfig := &istio.NetworkConfig{
		BaseDomain:       "cloud.sealos.io",
		DefaultGateway:   "istio-system/sealos-gateway",
		PublicDomains:    []string{"cloud.sealos.io"},
		TLSEnabled:       true,
		DefaultTLSSecret: "wildcard-cert",
	}

	// Create fake client
	scheme := runtime.NewScheme()
	_ = terminalv1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Create universal helper
	helper := istio.NewUniversalIstioNetworkingHelper(fakeClient, istioConfig, "terminal")

	t.Run("Public Domain Uses System Gateway", func(t *testing.T) {
		params := &istio.AppNetworkingParams{
			Name:        "test-terminal",
			Namespace:   "test-namespace",
			AppType:     "terminal",
			Hosts:       []string{"test-terminal.cloud.sealos.io"}, // Public domain
			ServiceName: "test-terminal-svc",
			ServicePort: 8080,
			Protocol:    istio.ProtocolWebSocket,
		}

		// Analyze domain requirements
		analysis := helper.AnalyzeDomainRequirements(params)

		// Verify public domain detection
		if !analysis.IsPublicDomain {
			t.Error("Should detect public domain")
		}

		// Verify system gateway usage
		if !analysis.UseSystemGateway {
			t.Error("Should use system gateway for public domain")
		}

		// Verify gateway reference
		if analysis.GatewayReference != istioConfig.DefaultGateway {
			t.Errorf("Expected gateway reference %s, got %s", istioConfig.DefaultGateway, analysis.GatewayReference)
		}
	})

	t.Run("Custom Domain Creates Dedicated Gateway", func(t *testing.T) {
		params := &istio.AppNetworkingParams{
			Name:        "test-terminal",
			Namespace:   "test-namespace",
			AppType:     "terminal",
			Hosts:       []string{"terminal.custom.example.com"}, // Custom domain
			ServiceName: "test-terminal-svc",
			ServicePort: 8080,
			Protocol:    istio.ProtocolWebSocket,
		}

		// Analyze domain requirements
		analysis := helper.AnalyzeDomainRequirements(params)

		// Verify custom domain detection
		if analysis.IsPublicDomain {
			t.Error("Should not detect as public domain")
		}

		// Verify dedicated gateway needed
		if analysis.UseSystemGateway {
			t.Error("Should not use system gateway for custom domain")
		}

		// Verify gateway reference
		expectedGateway := "test-namespace/test-terminal-gateway"
		if analysis.GatewayReference != expectedGateway {
			t.Errorf("Expected gateway reference %s, got %s", expectedGateway, analysis.GatewayReference)
		}
	})
}