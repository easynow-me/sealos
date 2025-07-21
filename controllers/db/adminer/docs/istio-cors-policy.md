# Adminer Controller Istio CORS 策略配置

## 概述

Adminer Controller 已更新，为 Adminer 应用的 VirtualService 添加了特定的 CORS 策略配置，以支持跨域访问。

## 主要更改

### 1. CORS Origins 配置

修改了 `buildNetworkingSpec` 方法中的 CORS 源配置：

- **之前**：使用通配符格式（如 `https://*.cloud.sealos.io`）
- **现在**：使用精确匹配的 adminer 子域名（如 `https://adminer.cloud.sealos.io`）

### 2. 实现细节

```go
// 构建 CORS 源 - 使用精确的 adminer 域名
corsOrigins := []string{}
if r.tlsEnabled {
    // 添加精确的 adminer 域名
    corsOrigins = append(corsOrigins, fmt.Sprintf("https://adminer.%s", r.adminerDomain))
    
    // 如果配置了公共域名，添加它们的 adminer 子域名
    if r.config != nil && len(r.config.PublicDomains) > 0 {
        for _, publicDomain := range r.config.PublicDomains {
            // 处理通配符域名 (如 *.cloud.sealos.io)
            if len(publicDomain) > 2 && publicDomain[0:2] == "*." {
                baseDomain := publicDomain[2:]
                corsOrigins = append(corsOrigins, fmt.Sprintf("https://adminer.%s", baseDomain))
            } else {
                // 精确域名
                corsOrigins = append(corsOrigins, fmt.Sprintf("https://adminer.%s", publicDomain))
            }
        }
    }
}
```

### 3. CORS 策略配置

更新后的 CORS 策略：

```go
CorsPolicy: &istio.CorsPolicy{
    AllowOrigins:     corsOrigins,
    AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
    AllowHeaders:     []string{"content-type", "authorization", "cookie", "x-requested-with"},
    AllowCredentials: true, // Adminer 需要凭据支持
},
```

### 4. 生成的 VirtualService 示例

当公共域名配置为 `cloud.sealos.io` 时，生成的 VirtualService 将包含：

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: adminer-instance
  namespace: ns-example
spec:
  hosts:
    - adminer-instance.cloud.sealos.io
  gateways:
    - adminer-gateway
  http:
    - match:
        - uri:
            prefix: /
      route:
        - destination:
            host: adminer-instance
            port:
              number: 8080
      corsPolicy:
        allowOrigins:
          - exact: "https://adminer.cloud.sealos.io"
        allowMethods:
          - GET
          - POST
          - PUT
          - DELETE
          - PATCH
          - OPTIONS
        allowHeaders:
          - content-type
          - authorization
          - cookie
          - x-requested-with
        allowCredentials: true
      timeout: 86400s  # 24 小时超时
```

## 配置要求

1. **NetworkConfig 配置**：确保在创建 `AdminerIstioNetworkingReconciler` 时传入正确的 `NetworkConfig`，特别是 `PublicDomains` 字段。

2. **TLS 配置**：根据 `tlsEnabled` 标志，CORS origins 会自动使用 `https://` 或 `http://` 前缀。

3. **公共域名**：支持以下格式：
   - 精确域名：`cloud.sealos.io`
   - 通配符域名：`*.cloud.sealos.io`

## 注意事项

1. **凭据支持**：`AllowCredentials` 设置为 `true`，允许 Adminer 应用处理带凭据的跨域请求。

2. **超时配置**：设置了 24 小时的超时时间，以支持长时间的数据库操作。

3. **安全头部**：通过 `buildSecurityHeaders` 方法设置了额外的安全头部，包括 CSP 策略。

## 测试验证

创建 Adminer 实例后，可以通过以下方式验证 CORS 配置：

```bash
# 检查 VirtualService 的 CORS 配置
kubectl get virtualservice <adminer-name> -n <namespace> -o yaml | grep -A 10 corsPolicy
```

预期输出应包含正确的 `allowOrigins` 配置，使用 `exact` 匹配而非通配符。