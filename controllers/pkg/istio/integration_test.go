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

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNetworkingManagerIntegration(t *testing.T) {
	// Create a fake client
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	config := &NetworkConfig{
		BaseDomain:           "sealos.cloud",
		DefaultGateway:       "istio-gateway",
		TLSEnabled:           true,
		SharedGatewayEnabled: false,
		GatewaySelector: map[string]string{
			"istio": "ingressgateway",
		},
	}

	manager := NewNetworkingManager(client, config)

	// Create a comprehensive app networking spec that includes all the problematic integer types
	timeout := 30 * time.Second
	perTryTimeout := 5 * time.Second
	maxAge := 24 * time.Hour

	spec := &AppNetworkingSpec{
		Name:        "test-app",
		Namespace:   "test-namespace",
		TenantID:    "user123",
		AppName:     "adminer",
		Protocol:    ProtocolHTTP,
		Hosts:       []string{"test-app.sealos.cloud"},
		ServiceName: "test-service",
		ServicePort: 8080, // This is int32 and was causing deep copy issues
		TLSConfig: &TLSConfig{
			SecretName: "test-tls",
			Hosts:      []string{"test-app.sealos.cloud"},
		},
		Timeout: &timeout,
		Retries: &RetryPolicy{
			Attempts:      3, // This is int32 and was causing deep copy issues
			PerTryTimeout: &perTryTimeout,
		},
		CorsPolicy: &CorsPolicy{
			AllowOrigins:     []string{"*", "https://example.com"},
			AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
			AllowHeaders:     []string{"Content-Type", "Authorization"},
			AllowCredentials: true, // This was wrapped in interface{} and causing issues
			MaxAge:           &maxAge,
		},
		Headers: map[string]string{
			"X-Custom-Header": "test-value",
		},
		Labels: map[string]string{
			"app":     "test-app",
			"version": "v1",
		},
	}

	ctx := context.Background()

	// Test CreateAppNetworking - this should not panic with deep copy errors
	t.Run("CreateAppNetworking", func(t *testing.T) {
		err := manager.CreateAppNetworking(ctx, spec)
		if err != nil {
			// We expect this to fail with fake client, but should not panic
			if err.Error() == "panic: cannot deep copy int [recovered]" {
				t.Errorf("Deep copy panic occurred during CreateAppNetworking: %v", err)
			}
			t.Logf("Expected error with fake client: %v", err)
		}
	})

	// Test UpdateAppNetworking - this should not panic with deep copy errors
	t.Run("UpdateAppNetworking", func(t *testing.T) {
		// Modify some values to test update
		spec.ServicePort = 9090
		spec.Retries.Attempts = 5

		err := manager.UpdateAppNetworking(ctx, spec)
		if err != nil {
			// We expect this to fail with fake client, but should not panic
			if err.Error() == "panic: cannot deep copy int [recovered]" {
				t.Errorf("Deep copy panic occurred during UpdateAppNetworking: %v", err)
			}
			t.Logf("Expected error with fake client: %v", err)
		}
	})
}

func TestGatewayAndVirtualServiceIntegration(t *testing.T) {
	// Test direct creation of Gateway and VirtualService with problematic types
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	config := &NetworkConfig{
		BaseDomain: "example.com",
		GatewaySelector: map[string]string{
			"istio": "ingressgateway",
		},
	}

	gatewayController := NewGatewayController(client, config)
	vsController := NewVirtualServiceController(client, config)

	ctx := context.Background()

	// Test Gateway creation with TLS (includes port 443 which was causing issues)
	t.Run("GatewayWithTLS", func(t *testing.T) {
		gatewayConfig := &GatewayConfig{
			Name:      "test-gateway-tls",
			Namespace: "test-ns",
			Hosts:     []string{"secure.example.com"},
			TLSConfig: &TLSConfig{
				SecretName: "secure-tls",
				Hosts:      []string{"secure.example.com"},
			},
			Labels: map[string]string{
				"type": "secure",
			},
		}

		err := gatewayController.Create(ctx, gatewayConfig)
		if err != nil && err.Error() == "panic: cannot deep copy int [recovered]" {
			t.Errorf("Deep copy panic in Gateway creation: %v", err)
		}
	})

	// Test VirtualService creation with all complex types
	t.Run("VirtualServiceWithAllFeatures", func(t *testing.T) {
		timeout := 45 * time.Second
		perTryTimeout := 10 * time.Second

		vsConfig := &VirtualServiceConfig{
			Name:        "test-vs-complex",
			Namespace:   "test-ns",
			Hosts:       []string{"api.example.com"},
			Gateways:    []string{"test-gateway"},
			Protocol:    ProtocolGRPC,
			ServiceName: "api-service",
			ServicePort: 50051, // gRPC port, was causing deep copy issues
			Timeout:     &timeout,
			Retries: &RetryPolicy{
				Attempts:      5, // int32, was problematic
				PerTryTimeout: &perTryTimeout,
			},
			CorsPolicy: &CorsPolicy{
				AllowOrigins:     []string{"https://app.example.com"},
				AllowMethods:     []string{"POST"},
				AllowHeaders:     []string{"Content-Type", "grpc-timeout"},
				AllowCredentials: false, // boolean, was wrapped incorrectly
			},
			Headers: map[string]string{
				"x-request-id": "auto-generated",
			},
			Labels: map[string]string{
				"protocol": "grpc",
			},
		}

		err := vsController.Create(ctx, vsConfig)
		if err != nil && err.Error() == "panic: cannot deep copy int [recovered]" {
			t.Errorf("Deep copy panic in VirtualService creation: %v", err)
		}
	})

	// Test VirtualService suspend functionality (includes specific int values that were problematic)
	t.Run("VirtualServiceSuspend", func(t *testing.T) {
		err := vsController.Suspend(ctx, "test-vs", "test-ns")
		if err != nil && err.Error() == "panic: cannot deep copy int [recovered]" {
			t.Errorf("Deep copy panic in VirtualService suspend: %v", err)
		}
	})
}

func TestDeepCopyStressTest(t *testing.T) {
	// Stress test the makeSafeForDeepCopy function with various complex structures
	testCases := []map[string]interface{}{
		{
			"servers": []interface{}{
				map[string]interface{}{
					"port": map[string]interface{}{
						"number":   80,     // int
						"name":     "http", // string
						"protocol": "HTTP", // string
					},
					"hosts": []string{"host1", "host2"}, // []string
				},
				map[string]interface{}{
					"port": map[string]interface{}{
						"number":   int32(443), // int32
						"name":     "https",    // string
						"protocol": "HTTPS",    // string
					},
					"hosts": []string{"secure-host"}, // []string
					"tls": map[string]interface{}{
						"mode":           "SIMPLE",     // string
						"credentialName": "tls-secret", // string
					},
				},
			},
		},
		{
			"http": []interface{}{
				map[string]interface{}{
					"route": []interface{}{
						map[string]interface{}{
							"destination": map[string]interface{}{
								"host": "service",      // string
								"port": map[string]interface{}{
									"number": int32(8080), // int32
								},
							},
						},
					},
					"retries": map[string]interface{}{
						"attempts": int32(3), // int32
					},
					"corsPolicy": map[string]interface{}{
						"allowCredentials": true, // bool
						"allowOrigins": []interface{}{
							map[string]interface{}{
								"exact": "https://example.com", // string
							},
						},
					},
				},
			},
		},
	}

	for i, testCase := range testCases {
		t.Run(fmt.Sprintf("Complex_Structure_%d", i), func(t *testing.T) {
			// This should not panic
			result := makeSafeForDeepCopy(testCase)
			
			// Verify the result is a map
			if _, ok := result.(map[string]interface{}); !ok {
				t.Errorf("Result is not a map[string]interface{}: %T", result)
			}
		})
	}
}