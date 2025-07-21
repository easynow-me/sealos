# AppLaunchpad Istio 智能 Gateway 配置指南

## 概述

AppLaunchpad 现在支持智能 Gateway 优化，能够根据域名类型自动选择使用共享 Gateway 还是创建独立 Gateway，从而大幅减少 Gateway 资源消耗。

## 🎯 核心优化策略

1. **公共域名** (如 `*.cloud.sealos.io`) → 使用系统共享 Gateway (`istio-system/sealos-gateway`)
2. **自定义域名** (如 `my-app.example.com`) → 创建用户独立 Gateway
3. **混合域名** → 智能分析，优化资源配置

## 📋 配置方法

### 方法一：运行时配置（推荐）

通过配置文件启用 Istio 模式，无需重新构建应用即可修改配置。

#### 开发环境
创建或修改 `frontend/providers/applaunchpad/data/config.yaml.local`：

```yaml
istio:
  enabled: true                    # 启用 Istio 模式
  publicDomains:                  # 公共域名列表
    - 'cloud.sealos.io'
    - '*.cloud.sealos.io'
  sharedGateway: 'sealos-gateway' # 共享 Gateway 名称
  enableTracing: false            # 启用链路追踪
```

#### 生产环境
修改容器内的 `/app/data/config.yaml`：

```yaml
istio:
  enabled: true
  publicDomains:
    - 'your-domain.com'
    - '*.your-domain.com'
  sharedGateway: 'your-shared-gateway'
  enableTracing: false
```

完整示例见 `data/config.yaml.istio-example`。

### 方法二：构建时环境变量（旧版）

在构建应用前设置环境变量：

```bash
# .env.local 或环境变量
NEXT_PUBLIC_USE_ISTIO=true
NEXT_PUBLIC_ENABLE_ISTIO=true
NEXT_PUBLIC_ISTIO_ENABLED=true

# 高级配置
NEXT_PUBLIC_ENABLE_TRACING=true
NEXT_PUBLIC_PUBLIC_DOMAINS=cloud.sealos.io
NEXT_PUBLIC_SHARED_GATEWAY=istio-system/sealos-gateway
```

**注意**：构建时配置需要重新构建应用才能修改设置。

## 🔧 配置文件示例

### Docker Compose

```yaml
services:
  applaunchpad:
    image: sealos/applaunchpad:latest
    environment:
      - NEXT_PUBLIC_USE_ISTIO=true
      - NEXT_PUBLIC_ENABLE_ISTIO=true
      - NEXT_PUBLIC_ISTIO_ENABLED=true
      - NEXT_PUBLIC_ENABLE_TRACING=false
      - NEXT_PUBLIC_PUBLIC_DOMAINS=cloud.sealos.io
```

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: applaunchpad
spec:
  template:
    spec:
      containers:
      - name: applaunchpad
        env:
        - name: NEXT_PUBLIC_USE_ISTIO
          value: "true"
        - name: NEXT_PUBLIC_ENABLE_ISTIO
          value: "true"
        - name: NEXT_PUBLIC_ISTIO_ENABLED
          value: "true"
        - name: NEXT_PUBLIC_PUBLIC_DOMAINS
          value: "cloud.sealos.io"
```

## 📊 智能 Gateway 逻辑

### 域名分类规则

```typescript
// 公共域名示例
const publicDomains = [
  'cloud.sealos.io'
];

// 智能分类逻辑
function classifyDomain(host: string) {
  for (const domain of publicDomains) {
    if (host.endsWith(domain)) {
      return 'public';  // 使用共享 Gateway
    }
  }
  return 'custom';  // 创建独立 Gateway
}
```

### 资源生成策略

| 域名类型 | Gateway 策略 | VirtualService 配置 | 证书管理 |
|---------|-------------|-------------------|----------|
| 纯公共域名 | 使用 `istio-system/sealos-gateway` | 引用共享 Gateway | 通配符证书 |
| 纯自定义域名 | 创建独立 Gateway | 引用独立 Gateway | Let's Encrypt |
| 混合域名 | 创建包含所有域名的 Gateway | 引用独立 Gateway | 混合证书 |

## 🎮 使用示例

### 1. 创建使用公共域名的应用

```typescript
// 应用配置
const appData = {
  appName: 'my-app',
  networks: [
    {
      networkName: 'web',
      port: 3000,
      openPublicDomain: true,
      publicDomain: 'my-app',
      domain: 'cloud.sealos.io'  // 公共域名
    }
  ]
};

// 生成的资源
formData2Yamls(appData, { 
  networkingMode: 'istio',
  enableSmartGateway: true 
});

// 结果：只生成 VirtualService，使用共享 Gateway
// 主机名：my-app.cloud.sealos.io
// Gateway：istio-system/sealos-gateway
```

### 2. 创建使用自定义域名的应用

```typescript
// 应用配置
const appData = {
  appName: 'my-app',
  networks: [
    {
      networkName: 'web',
      port: 3000,
      openPublicDomain: true,
      customDomain: 'my-app.example.com'  // 自定义域名
    }
  ]
};

// 结果：生成独立 Gateway + VirtualService + Certificate
// 主机名：my-app.example.com
// Gateway：my-app-gateway
// 证书：Let's Encrypt 自动申请
```

## 📈 性能优化效果

### 资源减少统计

根据 Gateway 优化计划，智能 Gateway 可以实现：

- **Gateway 数量减少 81%**：从 240 个减少到 46 个
- **内存使用减少约 60%**：共享 Gateway 资源复用
- **管理复杂度降低**：统一配置，减少维护成本

### 监控指标

```bash
# 检查 Gateway 使用情况
kubectl get gateway --all-namespaces

# 检查 VirtualService 优化情况
kubectl get virtualservice --all-namespaces -o json | \
  jq -r '.items[] | 
  select(.metadata.namespace | startswith("ns-")) | 
  "\(.metadata.namespace)/\(.metadata.name): \(.spec.gateways[])"'

# 统计共享 Gateway 使用数量
kubectl get virtualservice --all-namespaces -o json | \
  jq '[.items[] | select(.spec.gateways[]? == "istio-system/sealos-gateway")] | length'
```

## 🔧 故障排查

### 1. Istio 模式未生效

**症状**：仍然创建 Ingress 资源而非 VirtualService

**解决方案**：

#### 运行时配置检查
```bash
# 检查配置文件是否存在
ls -la /app/data/config.yaml

# 验证配置内容
cat /app/data/config.yaml | grep -A5 "istio:"

# 重启应用加载新配置
kubectl rollout restart deployment/applaunchpad
```

#### 构建时配置检查
```bash
# 检查环境变量是否设置正确
echo $NEXT_PUBLIC_USE_ISTIO
echo $NEXT_PUBLIC_ENABLE_ISTIO

# 重新构建镜像
npm run build
docker build -t applaunchpad:istio .
```

### 2. VirtualService 未使用共享 Gateway

**症状**：公共域名仍创建独立 Gateway

**解决方案**：
```bash
# 检查公共域名配置
echo $NEXT_PUBLIC_PUBLIC_DOMAINS

# 手动优化现有 VirtualService
kubectl patch virtualservice my-app -n ns-xxx --type=merge -p \
  '{"spec":{"gateways":["istio-system/sealos-gateway"]}}'
```

### 3. 证书管理问题

**症状**：HTTPS 访问失败

**解决方案**：
```bash
# 检查证书状态
kubectl get certificate --all-namespaces

# 检查 cert-manager 日志
kubectl logs -n cert-manager deploy/cert-manager

# 手动触发证书申请
kubectl annotate certificate my-cert -n ns-xxx \
  cert-manager.io/issue-temporary-certificate=""
```

## 🔄 迁移指南

### 从 Ingress 迁移到 Istio

1. **设置环境变量**：启用 Istio 模式
2. **更新应用配置**：重新部署应用
3. **验证资源**：检查 VirtualService 和 Gateway 创建
4. **清理旧资源**：删除不需要的 Ingress 和 Gateway

### 批量迁移脚本

```bash
# 使用强制迁移工具
./scripts/istio-migration/migrate-and-optimize-fast.sh --force

# 验证迁移结果
kubectl get ingress,virtualservice,gateway --all-namespaces | grep "ns-"
```

## 📚 相关文档

- [Gateway 优化计划](../docs/istio-migration/gateway-optimization-plan.md)
- [Istio VirtualService 配置](./src/utils/istioYaml.ts)
- [域名分类逻辑](../../controllers/pkg/istio/domain_classifier.go)
- [迁移脚本使用指南](../scripts/istio-migration/PARALLEL_MIGRATION_GUIDE.md)

## 🤝 贡献指南

如需添加新的公共域名或修改智能逻辑：

1. 更新环境变量配置
2. 修改 `istioYaml.ts` 中的逻辑
3. 测试并验证功能
4. 更新相关文档