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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMakeSafeForDeepCopy(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name:     "int to int64",
			input:    42,
			expected: int64(42),
		},
		{
			name:     "int32 to int64",
			input:    int32(80),
			expected: int64(80),
		},
		{
			name:     "uint32 to int64",
			input:    uint32(443),
			expected: int64(443),
		},
		{
			name:     "float32 to float64",
			input:    float32(3.5),
			expected: float64(3.5),
		},
		{
			name:     "bool unchanged",
			input:    true,
			expected: true,
		},
		{
			name:     "string unchanged",
			input:    "test",
			expected: "test",
		},
		{
			name:     "[]string to []interface{}",
			input:    []string{"host1", "host2"},
			expected: []interface{}{"host1", "host2"},
		},
		{
			name: "map[string]string to map[string]interface{}",
			input: map[string]string{
				"header1": "value1",
				"header2": "value2",
			},
			expected: map[string]interface{}{
				"header1": "value1",
				"header2": "value2",
			},
		},
		{
			name: "nested map",
			input: map[string]interface{}{
				"port": map[string]interface{}{
					"number":   80,
					"protocol": "HTTP",
				},
			},
			expected: map[string]interface{}{
				"port": map[string]interface{}{
					"number":   int64(80),
					"protocol": "HTTP",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := makeSafeForDeepCopy(tt.input)
			
			// For []interface{} comparison
			if resultSlice, ok := result.([]interface{}); ok {
				expectedSlice, ok := tt.expected.([]interface{})
				if !ok {
					t.Errorf("Expected slice but got %T", tt.expected)
					return
				}
				if len(resultSlice) != len(expectedSlice) {
					t.Errorf("Slice length mismatch: got %d, want %d", len(resultSlice), len(expectedSlice))
					return
				}
				for i := range resultSlice {
					if resultSlice[i] != expectedSlice[i] {
						t.Errorf("Element %d: got %v, want %v", i, resultSlice[i], expectedSlice[i])
					}
				}
				return
			}
			
			// For map comparison
			if resultMap, ok := result.(map[string]interface{}); ok {
				expectedMap, ok := tt.expected.(map[string]interface{})
				if !ok {
					t.Errorf("Expected map but got %T", tt.expected)
					return
				}
				
				// Check if this is the simple header map test case
				if _, hasHeader1 := resultMap["header1"]; hasHeader1 {
					// Compare simple string map
					if len(resultMap) != len(expectedMap) {
						t.Errorf("Map length mismatch: got %d, want %d", len(resultMap), len(expectedMap))
					}
					for k, expectedVal := range expectedMap {
						if resultVal, exists := resultMap[k]; !exists || resultVal != expectedVal {
							t.Errorf("Key %s: got %v, want %v", k, resultVal, expectedVal)
						}
					}
					return
				}
				
				// Compare nested structure (original test)
				portResult := resultMap["port"].(map[string]interface{})
				portExpected := expectedMap["port"].(map[string]interface{})
				
				if portResult["number"] != portExpected["number"] {
					t.Errorf("Port number: got %v (%T), want %v (%T)", 
						portResult["number"], portResult["number"],
						portExpected["number"], portExpected["number"])
				}
				if portResult["protocol"] != portExpected["protocol"] {
					t.Errorf("Port protocol: got %v, want %v", portResult["protocol"], portExpected["protocol"])
				}
				return
			}
			
			// For primitive types
			if result != tt.expected {
				t.Errorf("makeSafeForDeepCopy() = %v (%T), want %v (%T)", result, result, tt.expected, tt.expected)
			}
		})
	}
}

func TestGatewayControllerCreate(t *testing.T) {
	// Create a fake client
	scheme := newTestScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	config := &NetworkConfig{
		BaseDomain:     "example.com",
		DefaultGateway: "istio-gateway",
		TLSEnabled:     true,
	}

	controller := NewGatewayController(client, config)

	gatewayConfig := &GatewayConfig{
		Name:      "test-gateway",
		Namespace: "test-namespace",
		Hosts:     []string{"test.example.com"},
		TLSConfig: &TLSConfig{
			SecretName: "test-tls",
			Hosts:      []string{"test.example.com"},
		},
		Labels: map[string]string{
			"app": "test",
		},
	}

	ctx := context.Background()
	
	// Test that Create doesn't panic due to deep copy issues
	err := controller.Create(ctx, gatewayConfig)
	if err != nil {
		// We expect this to fail because we're using a fake client without proper setup
		// But it should not panic with "cannot deep copy int"
		if err.Error() == "panic: cannot deep copy int [recovered]" {
			t.Errorf("Deep copy panic occurred: %v", err)
		}
		t.Logf("Expected error (fake client): %v", err)
	}
}

func TestGatewaySpecBuilding(t *testing.T) {
	config := &NetworkConfig{
		GatewaySelector: map[string]string{
			"istio": "ingressgateway",
		},
	}

	controller := &gatewayController{
		config: config,
	}

	gatewayConfig := &GatewayConfig{
		Name:      "test-gateway",
		Namespace: "test-namespace",
		Hosts:     []string{"test.example.com"},
		TLSConfig: &TLSConfig{
			SecretName: "test-tls",
			Hosts:      []string{"test.example.com"},
		},
	}

	spec := controller.buildGatewaySpec(gatewayConfig)
	
	// Verify the spec can be safely deep copied
	safeSpec := makeSafeForDeepCopy(spec)
	
	// Test that we can create an unstructured object without panic
	gateway := &unstructured.Unstructured{}
	gateway.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "Gateway",
	})
	
	// This should not panic
	err := unstructured.SetNestedMap(gateway.Object, safeSpec.(map[string]interface{}), "spec")
	if err != nil {
		t.Errorf("Failed to set nested map: %v", err)
	}
	
	// Verify that port numbers are int64
	servers, found, err := unstructured.NestedSlice(gateway.Object, "spec", "servers")
	if err != nil || !found {
		t.Errorf("Failed to get servers: %v", err)
		return
	}
	
	for i, serverInterface := range servers {
		server := serverInterface.(map[string]interface{})
		port := server["port"].(map[string]interface{})
		number := port["number"]
		
		if _, ok := number.(int64); !ok {
			t.Errorf("Server %d port number is not int64: %T", i, number)
		}
	}
}

// Helper function to create a test scheme
func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	// Add any required schemes here if needed
	return scheme
}