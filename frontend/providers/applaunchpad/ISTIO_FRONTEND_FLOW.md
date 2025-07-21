# AppLaunchpad Istio Frontend Flow

This document explains how Istio resources (VirtualService/Gateway) are created and updated in the applaunchpad frontend.

## Overview

When Istio mode is enabled, applaunchpad creates VirtualService and Gateway resources instead of traditional Ingress resources. This happens transparently based on runtime configuration.

## Configuration Flow

### 1. Configuration Loading

```
Application Start (_app.tsx)
    ↓
loadInitData() (store/static.ts)
    ↓
GET /api/platform/getInitData
    ↓
Reads config.yaml
    ↓
Returns ISTIO_ENABLED, ISTIO_PUBLIC_DOMAINS, etc.
    ↓
Updates static store variables
```

### 2. Resource Generation

When creating or updating an app:

```
Form Submit (pages/app/edit/index.tsx)
    ↓
formData2Yamls(data)
    ↓
Checks ISTIO_ENABLED from static store
    ↓
If enabled:
  → generateNetworkingResources() with mode='istio'
  → Creates VirtualService + Gateway YAMLs
If disabled:
  → json2Ingress()
  → Creates Ingress YAML
    ↓
Returns yamlList[]
```

### 3. Resource Application

```
yamlList[]
    ↓
postDeployApp(yamlList) or putApp(patch)
    ↓
POST /api/applyApp or PUT /api/updateApp
    ↓
Kubernetes API applies resources
```

## Key Components

### Static Store (`store/static.ts`)

Stores runtime configuration:
- `ISTIO_ENABLED`: Whether to use Istio mode
- `ISTIO_PUBLIC_DOMAINS`: List of public domains for shared gateway
- `ISTIO_SHARED_GATEWAY`: Name of shared gateway
- `ISTIO_ENABLE_TRACING`: Enable distributed tracing

### Form Data to YAML (`pages/app/edit/index.tsx`)

The `formData2Yamls` function:
1. Reads `ISTIO_ENABLED` from static store
2. Calls appropriate generation function based on mode
3. Returns array of YAML resources

### Update API (`pages/api/updateApp.ts`)

Handles both Ingress and Istio resources:
- Has handlers for VirtualService and Gateway
- Supports patch and delete operations
- Works transparently with both resource types

## Configuration Examples

### Enable Istio Mode

Create `/app/data/config.yaml`:
```yaml
istio:
  enabled: true
  publicDomains:
    - 'cloud.sealos.io'
    - '*.cloud.sealos.io'
  sharedGateway: 'sealos-gateway'
```

### Result with Istio Enabled

When creating an app with public domain:
- Creates Service resource
- Creates VirtualService for routing
- Creates Gateway (or uses shared gateway for public domains)
- No Ingress resource created

### Result with Istio Disabled

When creating an app with public domain:
- Creates Service resource
- Creates Ingress resource
- No VirtualService/Gateway created

## Testing

1. Enable Istio in configuration
2. Create/update an app with public domain
3. Check created resources:
   ```bash
   kubectl get virtualservice,gateway -n ns-xxx
   ```

## Troubleshooting

### App still creates Ingress

1. Check if configuration is loaded:
   ```javascript
   // In browser console
   fetch('/api/platform/getInitData').then(r => r.json()).then(console.log)
   ```

2. Verify ISTIO_ENABLED is true in response

3. Restart the application to reload configuration

### VirtualService not found

1. Check if the update API supports VirtualService:
   - Look for `YamlKindEnum.VirtualService` in updateApp.ts
   - Verify handlers are configured

2. Check namespace permissions for istio resources

## Migration Notes

- Existing apps with Ingress will continue to work
- New apps will use VirtualService/Gateway based on configuration
- No code changes needed in frontend components
- Configuration is runtime, not build-time