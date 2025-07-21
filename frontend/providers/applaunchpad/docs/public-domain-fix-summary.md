# 公共域名判断逻辑修复总结

## 问题描述
用户报告：点击变更时，开启外网访问对于公共域名的判断逻辑错误，导致创建了自定义gateway（应该使用共享gateway）。

## 根本原因
1. **静态变量问题**：`ISTIO_PUBLIC_DOMAINS` 是从 `static.ts` 导入的静态变量，初始值为空数组
2. **配置加载时机**：虽然在 `_app.tsx` 中通过 `loadInitData()` 加载了配置并设置到全局状态，但 `index.tsx` 中使用的是静态变量而非全局状态
3. **结果**：`isPublicDomain()` 函数始终返回 false，因为 `ISTIO_PUBLIC_DOMAINS` 是空数组

## 实施的修复

### 1. 使用全局状态代替静态变量
- 从 `useGlobalStore` 获取 `istioConfig`
- 将 `istioConfig` 传递给判断函数

### 2. 重构域名判断逻辑
创建了工厂函数模式：
- `createDomainChecker(istioPublicDomains)` - 创建域名检查器
- `createGatewayOptionsGetter(istioConfig)` - 创建网关选项获取器

### 3. 更新 formData2Yamls 函数
- 添加 `istioConfig` 参数
- 使用传入的配置而非静态变量

### 4. 添加调试日志
- 记录域名匹配过程
- 记录配置状态
- 便于问题追踪

## 修复后的行为

现在系统会：
1. 正确加载公共域名列表（如 `*.cloud.sealos.io`）
2. 对于匹配公共域名模式的域名，使用共享网关
3. 只有真正的自定义域名才创建专用网关

## 测试验证

1. **公共域名测试**
   - 创建应用，使用默认域名（如 `myapp.cloud.sealos.io`）
   - 期望：使用共享网关 `sealos-gateway`

2. **自定义域名测试**
   - 创建应用，设置自定义域名（如 `myapp.example.com`）
   - 期望：创建专用网关

3. **查看日志**
   - 浏览器控制台会显示详细的域名匹配过程
   - 可以看到每个域名是被判断为 PUBLIC 还是 CUSTOM

## 配置示例

```yaml
istio:
  enabled: true
  publicDomains:
    - 'cloud.sealos.io'
    - '*.cloud.sealos.io'  # 重要：支持通配符匹配
    - 'sealos.io'
    - '*.sealos.io'
  sharedGateway: 'sealos-gateway'
```

## 注意事项

1. 确保配置文件中包含正确的 `publicDomains` 列表
2. 通配符域名格式为 `*.domain.com`
3. 配置更改需要重启前端应用才能生效