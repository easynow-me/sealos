# checkPermission API 故障排查指南

## API 概述

`/api/platform/checkPermission` 接口用于检查用户是否有权限操作指定的应用。它通过尝试修补(patch) deployment 或 statefulset 来验证权限。

## 常见错误及解决方案

### 1. "appName is required" 错误

**原因**: 请求中没有提供 appName 参数

**解决方案**: 确保调用时传递了 appName 参数
```typescript
await checkPermission({ appName: 'your-app-name' });
```

### 2. 403 Permission Denied 错误

**可能原因**:
- 用户的 kubeconfig 无效或过期
- 用户没有该命名空间的访问权限
- 用户没有修改 deployment/statefulset 的权限

**解决方案**:
1. 检查用户的 kubeconfig 是否有效
2. 验证用户是否有该命名空间的权限
3. 检查 RBAC 配置

### 3. "insufficient_funds" 响应

**原因**: 用户账户余额不足

**解决方案**: 提醒用户充值

### 4. 404 Not Found 错误

**原因**: 指定的应用(deployment/statefulset)不存在

**解决方案**: 
- 对于新应用，这是正常的，可以忽略
- 对于现有应用，检查应用名称是否正确

## 调试步骤

1. **检查请求参数**
   - 查看浏览器开发者工具的 Network 标签
   - 确认请求 URL: `/api/platform/checkPermission?appName=xxx`
   - 确认 Authorization header 存在

2. **查看服务器日志**
   - 查看 console.log 输出的 appName
   - 查看错误详情

3. **验证 Kubernetes 访问**
   ```bash
   kubectl auth can-i patch deployments -n <namespace>
   kubectl auth can-i patch statefulsets -n <namespace>
   ```

## API 改进建议

考虑将权限检查改为更轻量级的方式，例如：
- 使用 `kubectl auth can-i` 等效的 API
- 使用 SelfSubjectAccessReview API
- 只检查读权限而不是写权限