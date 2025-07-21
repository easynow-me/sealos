package controllers

import (
	"testing"

	adminerv1 "github.com/labring/sealos/controllers/db/adminer/api/v1"
	"github.com/labring/sealos/controllers/pkg/istio"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildNetworkingSpec_CorsPolicy(t *testing.T) {
	tests := []struct {
		name          string
		config        *istio.NetworkConfig
		tlsEnabled    bool
		adminerDomain string
		wantOrigins   []string
	}{
		{
			name: "TLS enabled with public domains",
			config: &istio.NetworkConfig{
				PublicDomains: []string{"cloud.sealos.io", "*.example.com"},
			},
			tlsEnabled:    true,
			adminerDomain: "cloud.sealos.io",
			wantOrigins: []string{
				"https://adminer.cloud.sealos.io",
				"https://adminer.cloud.sealos.io",
				"https://adminer.example.com",
			},
		},
		{
			name: "TLS disabled with public domains",
			config: &istio.NetworkConfig{
				PublicDomains: []string{"cloud.sealos.io"},
			},
			tlsEnabled:    false,
			adminerDomain: "cloud.sealos.io",
			wantOrigins: []string{
				"http://adminer.cloud.sealos.io",
				"http://adminer.cloud.sealos.io",
			},
		},
		{
			name:          "TLS enabled without public domains",
			config:        &istio.NetworkConfig{},
			tlsEnabled:    true,
			adminerDomain: "cloud.sealos.io",
			wantOrigins: []string{
				"https://adminer.cloud.sealos.io",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &AdminerIstioNetworkingReconciler{
				config:        tt.config,
				tlsEnabled:    tt.tlsEnabled,
				adminerDomain: tt.adminerDomain,
			}

			adminer := &adminerv1.Adminer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-adminer",
					Namespace: "test-namespace",
				},
			}

			spec := r.buildNetworkingSpec(adminer, "test-host")

			// Check CORS policy
			if spec.CorsPolicy == nil {
				t.Fatal("CorsPolicy should not be nil")
			}

			// Check AllowOrigins
			if len(spec.CorsPolicy.AllowOrigins) != len(tt.wantOrigins) {
				t.Errorf("AllowOrigins length = %d, want %d", len(spec.CorsPolicy.AllowOrigins), len(tt.wantOrigins))
			}

			// Check each origin
			originMap := make(map[string]bool)
			for _, origin := range spec.CorsPolicy.AllowOrigins {
				originMap[origin] = true
			}

			for _, wantOrigin := range tt.wantOrigins {
				if !originMap[wantOrigin] {
					t.Errorf("Missing expected origin: %s", wantOrigin)
				}
			}

			// Check other CORS settings
			if !spec.CorsPolicy.AllowCredentials {
				t.Error("AllowCredentials should be true for Adminer")
			}

			expectedMethods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"}
			if len(spec.CorsPolicy.AllowMethods) != len(expectedMethods) {
				t.Errorf("AllowMethods length = %d, want %d", len(spec.CorsPolicy.AllowMethods), len(expectedMethods))
			}

			expectedHeaders := []string{"content-type", "authorization", "cookie", "x-requested-with"}
			if len(spec.CorsPolicy.AllowHeaders) != len(expectedHeaders) {
				t.Errorf("AllowHeaders length = %d, want %d", len(spec.CorsPolicy.AllowHeaders), len(expectedHeaders))
			}
		})
	}
}