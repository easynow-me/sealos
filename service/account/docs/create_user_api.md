# Create User API Documentation

## Overview

The Create User API allows other services to programmatically create new users in the Sealos platform. This is an admin-only endpoint that requires proper authentication.

## Endpoint

```
POST /admin/v1alpha1/create-user
```

## Authentication

This endpoint requires admin authentication. You must include a valid admin JWT token in the Authorization header.

```
Authorization: Bearer <admin-jwt-token>
```

The user making the request must have the username `sealos-admin` encoded in the JWT token.

## Request Body

```json
{
  "username": "testuser",
  "userID": "user-123",           // Optional, will be auto-generated if not provided
  "initialBalance": 1000000000    // Optional, default is 0 (in smallest unit)
}
```

### Fields

- **username** (string, required): The username for the new user. Must be unique.
- **userID** (string, optional): Custom user ID. If not provided, a UUID will be generated.
- **initialBalance** (int64, optional): Initial account balance in the smallest unit (e.g., 1000000000 = 1 unit).

## Response

### Success Response (200 OK)

```json
{
  "userID": "user-123",
  "username": "testuser",
  "balance": 1000000000,
  "createdAt": "2024-01-01T00:00:00Z",
  "message": "User created successfully"
}
```

### Error Responses

#### 400 Bad Request

```json
{
  "error": "user already exists"
}
```

Common reasons:
- Username already exists
- Invalid request format
- Missing required fields

#### 401 Unauthorized

```json
{
  "error": "authenticate error: user is not admin"
}
```

Reasons:
- Missing or invalid JWT token
- User is not an admin

#### 500 Internal Server Error

```json
{
  "error": "failed to create user: <error details>"
}
```

## Usage Examples

### Using curl

```bash
curl -X POST http://localhost:2333/admin/v1alpha1/create-user \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{
    "username": "newuser",
    "initialBalance": 5000000000
  }'
```

### Using Go

```go
package main

import (
    "bytes"
    "encoding/json"
    "net/http"
)

func createUser(adminToken string) error {
    url := "http://account-service:2333/admin/v1alpha1/create-user"
    
    reqBody, _ := json.Marshal(map[string]interface{}{
        "username": "newuser",
        "initialBalance": 5000000000,
    })
    
    req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
    if err != nil {
        return err
    }
    
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer " + adminToken)
    
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    // Handle response...
    return nil
}
```

### Using JavaScript/Node.js

```javascript
async function createUser(adminToken) {
    const response = await fetch('http://account-service:2333/admin/v1alpha1/create-user', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${adminToken}`
        },
        body: JSON.stringify({
            username: 'newuser',
            initialBalance: 5000000000
        })
    });
    
    if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
    }
    
    return await response.json();
}
```

## What Happens When a User is Created

When the API successfully creates a user, the following resources are initialized:

1. **User Record**: Basic user information is stored
2. **Account**: An account with the specified initial balance is created
3. **Workspace**: A default workspace is created for the user
4. **User-Workspace Relationship**: The user is assigned as the owner of the workspace
5. **Kubernetes Resources**: User namespace and RBAC resources will be created when the user first accesses the system

## Notes

1. This API is intended for internal service use only and should not be exposed to end users.
2. The admin token must be properly secured and rotated regularly.
3. Consider implementing rate limiting to prevent abuse.
4. All monetary values are in the smallest unit (e.g., cents, satoshis) to avoid floating-point precision issues.
5. User creation is transactional - if any step fails, the entire operation is rolled back.

## Integration with Other Services

When integrating this API with other services:

1. **Service Account**: Create a service account with admin privileges for your service
2. **Token Management**: Implement proper token refresh and rotation
3. **Error Handling**: Implement retry logic with exponential backoff for transient errors
4. **Monitoring**: Log all user creation attempts for audit purposes
5. **Validation**: Validate usernames and other inputs before calling the API

## Security Considerations

1. **Authentication**: Always use HTTPS in production
2. **Authorization**: Limit admin access to trusted services only
3. **Audit Logging**: Log all user creation attempts with timestamp, requester, and outcome
4. **Rate Limiting**: Implement rate limiting to prevent abuse
5. **Input Validation**: Validate all inputs to prevent injection attacks