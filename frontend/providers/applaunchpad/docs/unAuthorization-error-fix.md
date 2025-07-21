# 解决 unAuthorization 错误

## 错误现象
```
checkPermission - request query: { appName: 'hello-world' }
checkPermission - appName: hello-world
checkPermission error: unAuthorization
===jsonRes===
Internal server error
```

## 可能原因

### 1. 用户未登录
- 用户会话已过期
- 浏览器 localStorage 中没有有效的 session

**解决方法**：
1. 检查浏览器控制台，查看是否有 "No kubeconfig found in session" 错误
2. 刷新页面重新登录
3. 清除浏览器缓存后重新登录

### 2. Authorization Header 传递问题
- 请求没有正确设置 Authorization header
- Header 编码有问题

**调试方法**：
1. 在浏览器开发者工具的 Network 标签中查看请求
2. 检查请求头中是否有 Authorization 字段
3. 查看 Authorization 字段的值是否正确编码

### 3. Kubeconfig 问题
- Kubeconfig 为空或无效
- 编码/解码失败

**检查方法**：
```javascript
// 在浏览器控制台执行
const session = localStorage.getItem('session');
const kubeconfig = JSON.parse(session)?.kubeconfig;
console.log('Has kubeconfig:', !!kubeconfig);
console.log('Kubeconfig length:', kubeconfig?.length);
```

## 临时解决方案

1. **使用调试端点**
   访问 `/api/platform/checkSession` 查看会话状态

2. **手动检查权限**
   ```bash
   # 使用 kubectl 直接检查
   kubectl auth can-i update deployments -n <namespace>
   ```

3. **重新获取会话**
   - 登出并重新登录
   - 确保使用有效的用户凭据

## 改进后的错误日志

现在系统会提供更详细的错误信息：
- "unAuthorization: No headers" - 请求没有 headers
- "unAuthorization: No authorization header" - 缺少 authorization header
- "unAuthorization: Invalid kubeconfig" - kubeconfig 无效
- "unAuthorization: Decode failed" - 解码失败

## 长期解决方案

1. 实现会话刷新机制
2. 添加更好的错误提示给用户
3. 实现自动重试机制
4. 使用更安全的认证方式（如 JWT token）