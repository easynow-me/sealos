# Adminer Controller CORS 策略修复 - syncOptimizedIstioNetworking 方法

## 修复概述

已成功修复 Adminer Controller 中 `syncOptimizedIstioNetworking` 方法的 CORS 策略配置，使其生成精确匹配的 CORS origins 而非通配符。

## 问题分析

### 修复前的问题
`buildCorsOrigins()` 方法生成的 CORS origins 使用通配符格式：

```go
corsOrigins = []string{
    fmt.Sprintf("https://%s", r.adminerDomain),         // https://cloud.sealos.io
    fmt.Sprintf("https://*.%s", r.adminerDomain),       // https://*.cloud.sealos.io
}
```

这会导致生成的 VirtualService 包含不够精确的 CORS 配置。

### 修复后的实现
现在 `buildCorsOrigins()` 方法生成精确的 adminer 子域名：

```go
// 添加精确的 adminer 子域名
corsOrigins = append(corsOrigins, fmt.Sprintf("https://adminer.%s", r.adminerDomain))

// 处理配置的公共域名
for _, publicDomain := range r.istioReconciler.config.PublicDomains {
    // 处理通配符域名 (如 *.cloud.sealos.io)
    if len(publicDomain) > 2 && publicDomain[0:2] == "*." {
        baseDomain := publicDomain[2:]
        corsOrigins = append(corsOrigins, fmt.Sprintf("https://adminer.%s", baseDomain))
    } else {
        // 精确域名
        corsOrigins = append(corsOrigins, fmt.Sprintf("https://adminer.%s", publicDomain))
    }
}
```

## 主要更改

### 1. `buildCorsOrigins()` 方法重写

**文件**: `controllers/db/adminer/controllers/adminer_controller.go`

- ✅ 使用精确的 `adminer.domain.com` 格式
- ✅ 支持多个公共域名配置
- ✅ 处理通配符域名模式 (`*.domain.com`)
- ✅ 自动去重复的 origins
- ✅ 支持 HTTP 和 HTTPS 模式

### 2. CORS 策略增强

更新了 `syncOptimizedIstioNetworking` 中的 CORS 配置：

```go
CorsPolicy: &istio.CorsPolicy{
    AllowOrigins:     r.buildCorsOrigins(),
    AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
    AllowHeaders:     []string{"content-type", "authorization", "cookie", "x-requested-with"},
    AllowCredentials: true,
},
```

### 3. 单元测试覆盖

创建了全面的测试 `cors_origins_test.go`：
- 测试不同 TLS 配置
- 测试通配符域名处理
- 测试多域名场景
- 测试去重功能

## 生成的 VirtualService 示例

### 修复后（正确配置）

当 `PublicDomains: ["cloud.sealos.io"]` 时：

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
spec:
  http:
    - corsPolicy:
        allowOrigins:
          - exact: "https://adminer.cloud.sealos.io"
        allowCredentials: true
        allowMethods: [GET, POST, PUT, DELETE, PATCH, OPTIONS]
        allowHeaders: [content-type, authorization, cookie, x-requested-with]
```

### 修复前（通配符配置）

```yaml
corsPolicy:
  allowOrigins:
    - exact: "https://cloud.sealos.io"
    - regex: "https://.*\\.cloud\\.sealos\\.io"  # 过于宽泛
  allowCredentials: true
```

## 配置支持

### 环境变量
- `DOMAIN`: 设置 adminer 域名
- `TLS_ENABLED`: 启用/禁用 TLS
- `ISTIO_PUBLIC_DOMAINS`: 配置公共域名列表

### 运行时配置
通过 `NetworkConfig.PublicDomains` 配置公共域名：

```go
config := &istio.NetworkConfig{
    PublicDomains: []string{
        "cloud.sealos.io",
        "*.example.com",
        "test.org",
    },
}
```

## 验证结果

### 单元测试通过
```
=== RUN   TestBuildCorsOrigins
    --- PASS: TestBuildCorsOrigins/TLS_enabled_with_single_domain (0.00s)
    --- PASS: TestBuildCorsOrigins/TLS_enabled_with_wildcard_domain (0.00s)
    --- PASS: TestBuildCorsOrigins/TLS_enabled_with_multiple_domains (0.00s)
    --- PASS: TestBuildCorsOrigins/TLS_disabled (0.00s)
    --- PASS: TestBuildCorsOrigins/No_config_provided (0.00s)
=== RUN   TestBuildCorsOrigins_Deduplication
    --- PASS: TestBuildCorsOrigins_Deduplication (0.00s)
```

### 编译验证
```bash
go build -o /tmp/adminer-controller-test ./main.go  # ✅ 编译通过
```

## 部署建议

1. **配置验证**: 确保 `ISTIO_PUBLIC_DOMAINS` 环境变量正确设置
2. **测试验证**: 部署后检查生成的 VirtualService 的 CORS 配置
3. **监控**: 观察 Adminer 应用的跨域请求是否正常工作

## 总结

此次修复确保了 Adminer Controller 的 `syncOptimizedIstioNetworking` 方法生成正确的精确匹配 CORS 策略，提高了安全性和准确性，同时保持了配置的灵活性和向后兼容性。