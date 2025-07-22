# Terminal Controller CORS Configuration Fix Summary

## 问题描述
Terminal Controller 中的 CORS 配置使用了通配符域名（如 `https://*.cloud.sealos.io`），这与 Adminer Controller 的精确匹配策略不一致。

## 修复内容

### 1. 修改 istio_networking.go
更新了 `buildNetworkingSpec` 方法，使用新的 `buildCorsOrigins` 方法来生成精确的 terminal 子域名：

```go
// 构建 CORS 源 - 使用精确匹配的 terminal 子域名
corsOrigins := r.buildCorsOrigins()
```

添加了 `buildCorsOrigins` 方法，生成精确的 terminal 子域名而不是通配符：

```go
// buildCorsOrigins 构建CORS源 - 使用精确匹配的terminal子域名
func (r *IstioNetworkingReconciler) buildCorsOrigins() []string {
    corsOrigins := []string{}
    
    if r.config.TLSEnabled {
        // 添加精确的 terminal 子域名
        corsOrigins = append(corsOrigins, fmt.Sprintf("https://terminal.%s", r.config.BaseDomain))
        
        // 处理公共域名配置
        if len(r.config.PublicDomains) > 0 {
            for _, publicDomain := range r.config.PublicDomains {
                // 处理通配符域名 (如 *.cloud.sealos.io)
                if len(publicDomain) > 2 && publicDomain[0:2] == "*." {
                    baseDomain := publicDomain[2:]
                    corsOrigins = append(corsOrigins, fmt.Sprintf("https://terminal.%s", baseDomain))
                } else {
                    // 精确域名
                    corsOrigins = append(corsOrigins, fmt.Sprintf("https://terminal.%s", publicDomain))
                }
            }
        }
    }
    // ... HTTP 模式处理
    
    // 去重
    return uniqueOrigins
}
```

### 2. 修改 terminal_controller.go
更新了 `buildTerminalCorsOrigins` 方法，同样使用精确匹配的 terminal 子域名：

```go
// buildTerminalCorsOrigins 构建Terminal的CORS源 - 使用精确匹配的terminal子域名
func (r *TerminalReconciler) buildTerminalCorsOrigins() []string {
    corsOrigins := []string{}
    
    // 检查是否启用了 TLS
    tlsEnabled := r.CtrConfig.Global.CloudPort == "" || r.CtrConfig.Global.CloudPort == "443"
    
    if tlsEnabled {
        // 添加精确的 terminal 子域名
        corsOrigins = append(corsOrigins, fmt.Sprintf("https://terminal.%s", r.CtrConfig.Global.CloudDomain))
        
        // 如果使用了 istioReconciler，获取公共域名配置
        if r.istioReconciler != nil && r.istioReconciler.config != nil {
            for _, publicDomain := range r.istioReconciler.config.PublicDomains {
                // 处理通配符域名
                if len(publicDomain) > 2 && publicDomain[0:2] == "*." {
                    baseDomain := publicDomain[2:]
                    corsOrigins = append(corsOrigins, fmt.Sprintf("https://terminal.%s", baseDomain))
                } else {
                    // 精确域名
                    corsOrigins = append(corsOrigins, fmt.Sprintf("https://terminal.%s", publicDomain))
                }
            }
        }
    }
    // ... HTTP 模式处理
    
    // 去重
    return uniqueOrigins
}
```

## 测试验证
创建了 `cors_test.go` 文件，包含两个测试：

1. **TestTerminalCorsOrigins** - 验证 CORS 源生成逻辑：
   - 测试 TLS 启用/禁用情况
   - 测试单个域名、通配符域名和多个域名的情况
   - 验证没有通配符出现在生成的 CORS 源中

2. **TestTerminalNetworkingSpec** - 验证完整的网络配置规范：
   - 验证 CORS 策略正确设置
   - 验证 WebSocket 协议正确配置
   - 验证 SecretHeader 正确传递

## 影响和好处

1. **安全性提升**：精确的域名匹配比通配符更安全，减少了潜在的跨域攻击风险
2. **一致性**：Terminal Controller 现在与 Adminer Controller 使用相同的 CORS 策略
3. **可维护性**：统一的模式使得未来的维护和更新更容易

## 示例输出
修复前的 CORS 源：
```
https://*.cloud.sealos.io
```

修复后的 CORS 源：
```
https://terminal.cloud.sealos.io
```

当配置了多个公共域名时：
```
https://terminal.cloud.sealos.io
https://terminal.example.com
https://terminal.custom.example.org
```