# Get User Token API Documentation

## Overview

The Get User Token API allows administrators to generate JWT tokens for specific users. This is useful for service-to-service authentication, debugging, and administrative tasks where you need to act on behalf of a user.

## Endpoint

```
POST /admin/v1alpha1/get-user-token
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
  "username": "testuser",         // Either username or userUID is required
  "userUID": "550e8400-e29b-41d4-a716-446655440000",  // Optional if username is provided
  "workspaceId": "workspace-123"  // Optional, workspace ID to include in token
}
```

### Fields

- **username** (string): The username to generate token for. Either this or userUID must be provided.
- **userUID** (string): The user UID to generate token for. Either this or username must be provided.
- **workspaceId** (string, optional): The workspace ID to include in the token. If provided, the token will be scoped to this workspace.

## Response

### Success Response (200 OK)

```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user": {
    "userId": "user-123",
    "userUid": "550e8400-e29b-41d4-a716-446655440000",
    "username": "testuser",
    "workspaceId": "workspace-123",
    "workspaceUid": "660e8400-e29b-41d4-a716-446655440000"
  },
  "expiresAt": "2024-01-01T00:30:00Z",
  "message": "Token generated successfully"
}
```

### Error Responses

#### 400 Bad Request

```json
{
  "error": "either username or userUID must be provided"
}
```

Common reasons:
- Neither username nor userUID provided
- Invalid userUID format
- Workspace not found

#### 401 Unauthorized

```json
{
  "error": "authenticate error: user is not admin"
}
```

Reasons:
- Missing or invalid JWT token
- User is not an admin

#### 404 Not Found

```json
{
  "error": "user not found"
}
```

Reasons:
- User with the specified username or UID doesn't exist

#### 500 Internal Server Error

```json
{
  "error": "failed to generate token: <error details>"
}
```

## Usage Examples

### Using curl

```bash
# Get token by username
curl -X POST http://localhost:2333/admin/v1alpha1/get-user-token \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{
    "username": "testuser"
  }'

# Get token by user UID with workspace
curl -X POST http://localhost:2333/admin/v1alpha1/get-user-token \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{
    "userUID": "550e8400-e29b-41d4-a716-446655440000",
    "workspaceId": "workspace-123"
  }'
```

### Using Go

```go
package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
)

func getUserToken(adminToken, username string) (string, error) {
    url := "http://account-service:2333/admin/v1alpha1/get-user-token"
    
    reqBody, _ := json.Marshal(map[string]interface{}{
        "username": username,
    })
    
    req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
    if err != nil {
        return "", err
    }
    
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer " + adminToken)
    
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()
    
    var result map[string]interface{}
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return "", err
    }
    
    token, ok := result["token"].(string)
    if !ok {
        return "", fmt.Errorf("token not found in response")
    }
    
    return token, nil
}
```

### Using JavaScript/Node.js

```javascript
async function getUserToken(adminToken, username, workspaceId = null) {
    const requestBody = {
        username: username
    };
    
    if (workspaceId) {
        requestBody.workspaceId = workspaceId;
    }
    
    const response = await fetch('http://account-service:2333/admin/v1alpha1/get-user-token', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${adminToken}`
        },
        body: JSON.stringify(requestBody)
    });
    
    if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
    }
    
    const data = await response.json();
    return data.token;
}
```

## Token Details

The generated token includes the following claims:

- **userUid**: The user's unique identifier
- **userId**: The user's ID
- **userCrName**: The user's name in the system
- **regionUid**: The region UID where the token was generated
- **workspaceId**: The workspace ID (if provided in the request)
- **workspaceUid**: The workspace UID (if workspace ID was provided)
- **exp**: Expiration time (30 minutes from generation)

## Use Cases

1. **Service-to-Service Authentication**: When one service needs to perform actions on behalf of a user
2. **Administrative Tasks**: When an admin needs to debug or fix issues for a specific user
3. **Automation**: Scripts that need to interact with the API as specific users
4. **Testing**: Integration tests that need to simulate user actions

## Security Considerations

1. **Token Lifetime**: Tokens expire after 30 minutes for security
2. **Audit Logging**: All token generation requests should be logged for audit purposes
3. **Admin Access**: Only trusted administrators should have access to this endpoint
4. **Token Scope**: When possible, use workspace-scoped tokens to limit access
5. **Secure Storage**: Generated tokens should be stored securely and never logged in plain text

## Notes

1. This API should only be used by trusted services and administrators
2. Consider implementing additional security measures like IP whitelisting for this endpoint
3. Monitor usage patterns to detect potential abuse
4. The generated token has the same permissions as the user it represents
5. Tokens cannot be revoked once generated; they will expire after 30 minutes