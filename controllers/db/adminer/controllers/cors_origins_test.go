package controllers

import (
	"testing"

	"github.com/labring/sealos/controllers/pkg/istio"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildCorsOrigins(t *testing.T) {
	tests := []struct {
		name          string
		tlsEnabled    bool
		adminerDomain string
		config        *istio.NetworkConfig
		wantOrigins   []string
	}{
		{
			name:          "TLS enabled with single domain",
			tlsEnabled:    true,
			adminerDomain: "cloud.sealos.io",
			config: &istio.NetworkConfig{
				PublicDomains: []string{"cloud.sealos.io"},
			},
			wantOrigins: []string{
				"https://adminer.cloud.sealos.io",
			},
		},
		{
			name:          "TLS enabled with wildcard domain",
			tlsEnabled:    true,
			adminerDomain: "cloud.sealos.io",
			config: &istio.NetworkConfig{
				PublicDomains: []string{"*.cloud.sealos.io"},
			},
			wantOrigins: []string{
				"https://adminer.cloud.sealos.io",
			},
		},
		{
			name:          "TLS enabled with multiple domains",
			tlsEnabled:    true,
			adminerDomain: "cloud.sealos.io",
			config: &istio.NetworkConfig{
				PublicDomains: []string{"cloud.sealos.io", "*.example.com", "test.org"},
			},
			wantOrigins: []string{
				"https://adminer.cloud.sealos.io",
				"https://adminer.example.com",
				"https://adminer.test.org",
			},
		},
		{
			name:          "TLS disabled",
			tlsEnabled:    false,
			adminerDomain: "cloud.sealos.io",
			config: &istio.NetworkConfig{
				PublicDomains: []string{"cloud.sealos.io"},
			},
			wantOrigins: []string{
				"http://adminer.cloud.sealos.io",
			},
		},
		{
			name:          "No config provided",
			tlsEnabled:    true,
			adminerDomain: "cloud.sealos.io",
			config:        nil,
			wantOrigins: []string{
				"https://adminer.cloud.sealos.io",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().Build()
			r := &AdminerReconciler{
				Client:        fakeClient,
				tlsEnabled:    tt.tlsEnabled,
				adminerDomain: tt.adminerDomain,
			}

			// Set up istioReconciler with config if provided
			if tt.config != nil {
				r.istioReconciler = &AdminerIstioNetworkingReconciler{
					config: tt.config,
				}
			}

			origins := r.buildCorsOrigins()

			if len(origins) != len(tt.wantOrigins) {
				t.Errorf("buildCorsOrigins() returned %d origins, want %d", len(origins), len(tt.wantOrigins))
				t.Errorf("Got: %v", origins)
				t.Errorf("Want: %v", tt.wantOrigins)
				return
			}

			// Check that all expected origins are present
			originMap := make(map[string]bool)
			for _, origin := range origins {
				originMap[origin] = true
			}

			for _, wantOrigin := range tt.wantOrigins {
				if !originMap[wantOrigin] {
					t.Errorf("buildCorsOrigins() missing expected origin: %s", wantOrigin)
				}
			}

			// Verify no wildcards are used (should all be exact matches)
			for _, origin := range origins {
				if len(origin) > 8 && origin[8:10] == "//*" { // Check for "https://*" or "http://*"
					t.Errorf("buildCorsOrigins() should not return wildcard origins, got: %s", origin)
				}
			}
		})
	}
}

func TestBuildCorsOrigins_Deduplication(t *testing.T) {
	fakeClient := fake.NewClientBuilder().Build()
	r := &AdminerReconciler{
		Client:        fakeClient,
		tlsEnabled:    true,
		adminerDomain: "cloud.sealos.io",
		istioReconciler: &AdminerIstioNetworkingReconciler{
			config: &istio.NetworkConfig{
				// This should result in duplicate origins that need deduplication
				PublicDomains: []string{"cloud.sealos.io", "*.cloud.sealos.io"},
			},
		},
	}

	origins := r.buildCorsOrigins()

	// Should only have one unique origin
	expectedOrigin := "https://adminer.cloud.sealos.io"
	if len(origins) != 1 {
		t.Errorf("buildCorsOrigins() should deduplicate origins, got %d origins: %v", len(origins), origins)
	}

	if origins[0] != expectedOrigin {
		t.Errorf("buildCorsOrigins() = %v, want [%s]", origins, expectedOrigin)
	}
}