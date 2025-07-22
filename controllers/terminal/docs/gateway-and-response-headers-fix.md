# Terminal Controller Gateway and Response Headers Fix

## 问题分析

检查了 Terminal Controller 存在的两个潜在问题：

1. **Gateway 使用问题** - 是否正确使用公共 gateway
2. **Response Headers 设置问题** - 是否缺少响应头部设置

## 检查结果

### 1. Gateway 使用 ✅ 正确

Terminal Controller 正确实现了智能网关选择：
- `syncOptimizedIstioNetworking` 方法使用了 `istioHelper.CreateOrUpdateNetworking`
- 这会自动根据域名类型选择正确的 Gateway：
  - 公共域名（如 `*.cloud.sealos.io`）→ 使用系统共享网关
  - 自定义域名 → 创建专用网关

### 2. Response Headers ❌ 缺失

发现 Terminal Controller 没有设置响应头部，这可能导致安全问题。

## 修复内容

### 1. 添加响应头部配置

在 `terminal_controller.go` 中：
```go
// CORS 配置
CorsPolicy: &istio.CorsPolicy{
    AllowOrigins:     r.buildTerminalCorsOrigins(),
    AllowMethods:     []string{"PUT", "GET", "POST", "PATCH", "OPTIONS"},
    AllowHeaders:     []string{"content-type", "authorization"},
    AllowCredentials: false,
},

// 响应头部配置（安全头部）
ResponseHeaders: r.buildSecurityResponseHeaders(),
```

### 2. 实现 buildSecurityResponseHeaders 方法

为 Terminal 应用添加了适合 WebSocket 的安全响应头部：

```go
func (r *TerminalReconciler) buildSecurityResponseHeaders() map[string]string {
    headers := make(map[string]string)
    
    // 防止点击劫持
    headers["X-Frame-Options"] = "SAMEORIGIN"
    
    // 防止 MIME 类型嗅探
    headers["X-Content-Type-Options"] = "nosniff"
    
    // XSS 保护
    headers["X-XSS-Protection"] = "1; mode=block"
    
    // Referrer 策略
    headers["Referrer-Policy"] = "strict-origin-when-cross-origin"
    
    // 基本的 CSP，支持 WebSocket
    headers["Content-Security-Policy"] = "default-src 'self'; connect-src 'self' wss:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline' 'unsafe-eval';"
    
    return headers
}
```

### 3. 同步更新 istio_networking.go

确保两种网络配置方式都支持响应头部设置。

## 安全头部说明

为 Terminal（WebSocket 应用）选择的安全头部：

1. **X-Frame-Options: SAMEORIGIN**
   - 防止点击劫持攻击
   - 允许同源 iframe 嵌入

2. **X-Content-Type-Options: nosniff**
   - 防止浏览器 MIME 类型嗅探
   - 强制使用声明的 content-type

3. **X-XSS-Protection: 1; mode=block**
   - 启用浏览器的 XSS 过滤器
   - 检测到攻击时阻止页面加载

4. **Referrer-Policy: strict-origin-when-cross-origin**
   - 控制 Referer 头部发送
   - 跨域时只发送源信息

5. **Content-Security-Policy**
   - 限制资源加载来源
   - 特别允许 `wss:` 协议支持 WebSocket 连接

## 测试验证

创建了完整的测试套件 `response_headers_test.go`：

1. **TestTerminalSecurityResponseHeaders**
   - 验证响应头部正确设置
   - 确保包含所有必要的安全头部

2. **TestTerminalGatewaySelection**
   - 验证公共域名使用系统网关
   - 验证自定义域名创建专用网关

所有测试通过 ✅

## 影响和好处

1. **增强安全性**：添加安全响应头部保护免受常见 Web 攻击
2. **WebSocket 兼容**：CSP 策略专门支持 WebSocket 连接
3. **一致性**：Terminal 和 Adminer 控制器现在都有适当的安全头部
4. **智能网关**：继续使用优化的网关选择策略

## 结论

Terminal Controller 现在：
- ✅ 正确使用智能网关选择（公共域名使用公共网关）
- ✅ 设置了适当的安全响应头部
- ✅ 保持 WebSocket 功能正常工作
- ✅ 与整个 Istio 迁移架构保持一致