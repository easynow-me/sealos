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
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestVirtualServiceControllerCreate(t *testing.T) {
	// Create a fake client
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	config := &NetworkConfig{
		BaseDomain:     "example.com",
		DefaultGateway: "istio-gateway",
		TLSEnabled:     true,
	}

	controller := NewVirtualServiceController(client, config)

	timeout := 30 * time.Second
	vsConfig := &VirtualServiceConfig{
		Name:        "test-vs",
		Namespace:   "test-namespace",
		Hosts:       []string{"test.example.com"},
		Gateways:    []string{"test-gateway"},
		Protocol:    ProtocolHTTP,
		ServiceName: "test-service",
		ServicePort: 8080,
		Timeout:     &timeout,
		Retries: &RetryPolicy{
			Attempts: 3,
		},
		CorsPolicy: &CorsPolicy{
			AllowOrigins:     []string{"*"},
			AllowMethods:     []string{"GET", "POST"},
			AllowHeaders:     []string{"Content-Type"},
			AllowCredentials: true,
		},
		Labels: map[string]string{
			"app": "test",
		},
	}

	ctx := context.Background()
	
	// Test that Create doesn't panic due to deep copy issues
	err := controller.Create(ctx, vsConfig)
	if err != nil {
		// We expect this to fail because we're using a fake client without proper setup
		// But it should not panic with "cannot deep copy int"
		if err.Error() == "panic: cannot deep copy int [recovered]" {
			t.Errorf("Deep copy panic occurred: %v", err)
		}
		t.Logf("Expected error (fake client): %v", err)
	}
}

func TestVirtualServiceSpecBuilding(t *testing.T) {
	config := &NetworkConfig{
		BaseDomain:     "example.com",
		DefaultGateway: "istio-gateway",
	}

	controller := &virtualServiceController{
		config: config,
	}

	timeout := 30 * time.Second
	vsConfig := &VirtualServiceConfig{
		Name:        "test-vs",
		Namespace:   "test-namespace",
		Hosts:       []string{"test.example.com"},
		Gateways:    []string{"test-gateway"},
		Protocol:    ProtocolHTTP,
		ServiceName: "test-service",
		ServicePort: 8080,
		Timeout:     &timeout,
		Retries: &RetryPolicy{
			Attempts: 3,
		},
		CorsPolicy: &CorsPolicy{
			AllowOrigins:     []string{"*"},
			AllowMethods:     []string{"GET", "POST"},
			AllowHeaders:     []string{"Content-Type"},
			AllowCredentials: true,
		},
	}

	spec := controller.buildVirtualServiceSpec(vsConfig)
	
	// Test that we can create an unstructured object without panic
	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualService",
	})
	
	// This should not panic
	err := unstructured.SetNestedMap(vs.Object, spec, "spec")
	if err != nil {
		t.Errorf("Failed to set nested map: %v", err)
		return
	}
	
	// Verify that port numbers are int64
	httpRoutes, found, err := unstructured.NestedSlice(vs.Object, "spec", "http")
	if err != nil || !found {
		t.Errorf("Failed to get http routes: %v", err)
		return
	}
	
	for i, routeInterface := range httpRoutes {
		route := routeInterface.(map[string]interface{})
		
		// Check destination port
		routeSlice := route["route"].([]interface{})
		destination := routeSlice[0].(map[string]interface{})
		dest := destination["destination"].(map[string]interface{})
		port := dest["port"].(map[string]interface{})
		number := port["number"]
		
		if _, ok := number.(int64); !ok {
			t.Errorf("Route %d destination port number is not int64: %T", i, number)
		}
		
		// Check retry attempts if present
		if retriesInterface, exists := route["retries"]; exists {
			retries := retriesInterface.(map[string]interface{})
			attempts := retries["attempts"]
			
			if _, ok := attempts.(int64); !ok {
				t.Errorf("Route %d retry attempts is not int64: %T", i, attempts)
			}
		}
	}
}

func TestVirtualServiceSuspendSpec(t *testing.T) {
	// Test the suspend functionality to ensure it uses correct types
	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualService",
	})
	
	// Build suspended route
	suspendedRoute := []interface{}{
		map[string]interface{}{
			"match": []interface{}{
				map[string]interface{}{
					"uri": map[string]interface{}{
						"prefix": "/",
					},
				},
			},
			"fault": map[string]interface{}{
				"abort": map[string]interface{}{
					"percentage": map[string]interface{}{
						"value": int64(100),
					},
					"httpStatus": int64(503),
				},
			},
		},
	}
	
	// This should not panic
	err := unstructured.SetNestedSlice(vs.Object, suspendedRoute, "spec", "http")
	if err != nil {
		t.Errorf("Failed to set suspended route: %v", err)
		return
	}
	
	// Verify the values are correct types
	httpRoutes, found, err := unstructured.NestedSlice(vs.Object, "spec", "http")
	if err != nil || !found {
		t.Errorf("Failed to get suspended http routes: %v", err)
		return
	}
	
	route := httpRoutes[0].(map[string]interface{})
	fault := route["fault"].(map[string]interface{})
	abort := fault["abort"].(map[string]interface{})
	percentage := abort["percentage"].(map[string]interface{})
	
	if value := percentage["value"]; value != int64(100) {
		t.Errorf("Percentage value is not int64(100): got %v (%T)", value, value)
	}
	
	if status := abort["httpStatus"]; status != int64(503) {
		t.Errorf("HTTP status is not int64(503): got %v (%T)", status, status)
	}
}

func TestBuildCorsPolicy(t *testing.T) {
	controller := &virtualServiceController{}
	
	cors := &CorsPolicy{
		AllowOrigins:     []string{"https://example.com", "*"},
		AllowMethods:     []string{"GET", "POST", "PUT"},
		AllowHeaders:     []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           &[]time.Duration{5 * time.Minute}[0],
	}
	
	policy := controller.buildCorsPolicy(cors)
	
	// Verify allowCredentials is boolean (not wrapped in interface{})
	if creds := policy["allowCredentials"]; creds != true {
		t.Errorf("allowCredentials should be true, got %v (%T)", creds, creds)
	}
	
	// Verify origins structure
	origins := policy["allowOrigins"].([]interface{})
	if len(origins) != 2 {
		t.Errorf("Expected 2 origins, got %d", len(origins))
	}
	
	// Check wildcard origin
	wildcardOrigin := origins[1].(map[string]interface{})
	if wildcardOrigin["regex"] != ".*" {
		t.Errorf("Wildcard origin should have regex: .*, got %v", wildcardOrigin["regex"])
	}
}