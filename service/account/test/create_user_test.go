package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/labring/sealos/service/account/helper"
)

// TestAdminCreateUser tests the AdminCreateUser API endpoint
func TestAdminCreateUser(t *testing.T) {
	// Test configuration
	baseURL := "http://localhost:2333"
	adminToken := "your-admin-jwt-token" // Replace with a valid admin JWT token

	// Test cases
	testCases := []struct {
		name           string
		request        helper.CreateUserReq
		expectedStatus int
		description    string
	}{
		{
			name: "Create user successfully",
			request: helper.CreateUserReq{
				Username:       "testuser1",
				UserID:         "",  // Will be auto-generated
				InitialBalance: 1000000000, // 1 unit
				AuthBase: helper.AuthBase{
					Auth: &helper.Auth{
						// Admin auth will be set via header
					},
				},
			},
			expectedStatus: http.StatusOK,
			description:    "Should create a new user successfully",
		},
		{
			name: "Create user with custom ID",
			request: helper.CreateUserReq{
				Username:       "testuser2",
				UserID:         "custom-user-id-123",
				InitialBalance: 5000000000, // 5 units
				AuthBase: helper.AuthBase{
					Auth: &helper.Auth{
						// Admin auth will be set via header
					},
				},
			},
			expectedStatus: http.StatusOK,
			description:    "Should create a new user with custom ID",
		},
		{
			name: "Create duplicate user",
			request: helper.CreateUserReq{
				Username:       "testuser1", // Already exists
				InitialBalance: 1000000000,
				AuthBase: helper.AuthBase{
					Auth: &helper.Auth{
						// Admin auth will be set via header
					},
				},
			},
			expectedStatus: http.StatusBadRequest,
			description:    "Should fail when creating duplicate user",
		},
		{
			name: "Create user without username",
			request: helper.CreateUserReq{
				Username:       "", // Empty username
				InitialBalance: 1000000000,
				AuthBase: helper.AuthBase{
					Auth: &helper.Auth{
						// Admin auth will be set via header
					},
				},
			},
			expectedStatus: http.StatusBadRequest,
			description:    "Should fail when username is empty",
		},
	}

	// Run test cases
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Prepare request body
			reqBody, err := json.Marshal(tc.request)
			if err != nil {
				t.Fatalf("Failed to marshal request: %v", err)
			}

			// Create HTTP request
			req, err := http.NewRequest("POST", baseURL+"/admin/v1alpha1/create-user", bytes.NewBuffer(reqBody))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			// Set headers
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+adminToken)

			// Send request
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Failed to send request: %v", err)
			}
			defer resp.Body.Close()

			// Check status code
			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, resp.StatusCode)
			}

			// Parse response
			if resp.StatusCode == http.StatusOK {
				var createResp helper.CreateUserResp
				if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
					t.Errorf("Failed to decode response: %v", err)
				} else {
					// Verify response
					if createResp.Username != tc.request.Username {
						t.Errorf("Expected username %s, got %s", tc.request.Username, createResp.Username)
					}
					if createResp.Balance != tc.request.InitialBalance {
						t.Errorf("Expected balance %d, got %d", tc.request.InitialBalance, createResp.Balance)
					}
					t.Logf("Created user: %+v", createResp)
				}
			}
		})
	}
}

// Example of how to use the API from other services
func ExampleCreateUserFromService() {
	// API endpoint
	url := "http://account-service.sealos-system.svc.cluster.local:2333/admin/v1alpha1/create-user"
	
	// Prepare request
	createReq := helper.CreateUserReq{
		Username:       "service-created-user",
		InitialBalance: 10000000000, // 10 units
	}

	reqBody, _ := json.Marshal(createReq)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	
	// Set admin token (obtained from your service's auth mechanism)
	req.Header.Set("Authorization", "Bearer your-service-admin-token")
	req.Header.Set("Content-Type", "application/json")

	// Send request
	client := &http.Client{}
	resp, _ := client.Do(req)
	defer resp.Body.Close()

	// Handle response
	if resp.StatusCode == http.StatusOK {
		var result helper.CreateUserResp
		json.NewDecoder(resp.Body).Decode(&result)
		// User created successfully
	}
}