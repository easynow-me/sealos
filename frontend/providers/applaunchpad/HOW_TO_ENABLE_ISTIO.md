# 如何在 AppLaunchpad 中启用 Istio

本指南详细说明如何配置 AppLaunchpad 使用 Istio（VirtualService/Gateway）而不是传统的 Ingress。

## 快速开始

### 方法一：运行时配置（推荐）

1. **创建配置文件**

   开发环境：
   ```bash
   # 创建 frontend/providers/applaunchpad/data/config.yaml.local
   ```

   生产环境：
   ```bash
   # 修改容器内的 /app/data/config.yaml
   ```

2. **添加 Istio 配置**

   ```yaml
   # 最简配置
   istio:
     enabled: true

   # 完整配置示例
   istio:
     enabled: true                    # 启用 Istio 模式
     publicDomains:                  # 公共域名列表（使用共享网关）
       - 'cloud.sealos.io'
       - '*.cloud.sealos.io'
     sharedGateway: 'sealos-gateway' # 共享网关名称
     enableTracing: false            # 分布式追踪
   ```

3. **重启应用**
   ```bash
   # Kubernetes 环境
   kubectl rollout restart deployment/applaunchpad -n sealos
   
   # Docker 环境
   docker restart applaunchpad
   ```

### 方法二：环境变量（构建时）

**注意**：此方法需要重新构建镜像

```bash
# 设置环境变量
export NEXT_PUBLIC_USE_ISTIO=true
export NEXT_PUBLIC_ENABLE_ISTIO=true
export NEXT_PUBLIC_ISTIO_ENABLED=true

# 构建应用
npm run build
```

## 配置示例

### 1. Kubernetes ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: applaunchpad-config
  namespace: sealos
data:
  config.yaml: |
    cloud:
      domain: 'cloud.sealos.io'
      userDomains:
        - name: 'cloud.sealos.io'
          secretName: 'wildcard-cert'
    
    istio:
      enabled: true
      publicDomains:
        - 'cloud.sealos.io'
        - '*.cloud.sealos.io'
      sharedGateway: 'sealos-gateway'
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: applaunchpad
spec:
  template:
    spec:
      containers:
      - name: applaunchpad
        volumeMounts:
        - name: config
          mountPath: /app/data
      volumes:
      - name: config
        configMap:
          name: applaunchpad-config
```

### 2. Docker Compose

```yaml
version: '3.8'
services:
  applaunchpad:
    image: sealos/applaunchpad:latest
    volumes:
      - ./config.yaml:/app/data/config.yaml:ro
    environment:
      - NODE_ENV=production
```

### 3. 直接修改容器配置

```bash
# 进入容器
kubectl exec -it deployment/applaunchpad -n sealos -- sh

# 编辑配置文件
cat > /app/data/config.yaml << EOF
istio:
  enabled: true
  publicDomains:
    - 'cloud.sealos.io'
EOF

# 退出并重启
exit
kubectl rollout restart deployment/applaunchpad -n sealos
```

## 验证配置

### 1. 检查配置是否生效

```bash
# 调用 API 查看配置
curl http://applaunchpad-service/api/platform/getInitData | jq .data.ISTIO_ENABLED

# 应该返回 true
```

### 2. 创建测试应用

创建一个带有公共域名的应用，然后检查资源：

```bash
# 检查是否创建了 VirtualService 和 Gateway
kubectl get virtualservice,gateway -n ns-<username>

# 确认没有创建 Ingress
kubectl get ingress -n ns-<username>
# 应该返回空
```

### 3. 查看日志

```bash
kubectl logs deployment/applaunchpad -n sealos | grep -i istio
```

## 工作原理

### 配置加载流程

```
1. 应用启动 (_app.tsx)
      ↓
2. 调用 loadInitData() 
      ↓
3. GET /api/platform/getInitData
      ↓
4. 读取 config.yaml 文件
      ↓
5. 设置 ISTIO_ENABLED 等变量
      ↓
6. formData2Yamls 使用这些变量决定生成哪种资源
```

### 资源生成逻辑

```typescript
// 在 formData2Yamls 函数中
const networkingMode = getNetworkingMode({
  useIstio: ISTIO_ENABLED,  // 从运行时配置读取
  enableIstio: ISTIO_ENABLED,
  istioEnabled: ISTIO_ENABLED
});

if (networkingMode === 'istio') {
  // 生成 VirtualService 和 Gateway
} else {
  // 生成 Ingress
}
```

## 配置选项说明

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `istio.enabled` | boolean | false | 是否启用 Istio 模式 |
| `istio.publicDomains` | string[] | [] | 公共域名列表，这些域名使用共享网关 |
| `istio.sharedGateway` | string | 'sealos-gateway' | 共享网关的名称 |
| `istio.enableTracing` | boolean | false | 是否启用分布式追踪 |

## 智能网关选择

系统会根据域名类型自动选择网关策略：

1. **公共域名**（在 publicDomains 列表中）
   - 使用共享网关
   - 不创建新的 Gateway 资源
   - 节省资源

2. **自定义域名**（不在 publicDomains 列表中）
   - 创建独立的 Gateway
   - 自动配置证书

## 故障排查

### 问题：仍然创建 Ingress

1. **检查配置文件位置**
   ```bash
   # 开发环境
   ls -la frontend/providers/applaunchpad/data/config.yaml.local
   
   # 生产环境
   kubectl exec deployment/applaunchpad -- ls -la /app/data/config.yaml
   ```

2. **验证配置内容**
   ```bash
   kubectl exec deployment/applaunchpad -- cat /app/data/config.yaml
   ```

3. **检查 API 响应**
   ```bash
   # 在浏览器控制台执行
   fetch('/api/platform/getInitData')
     .then(r => r.json())
     .then(d => console.log('ISTIO_ENABLED:', d.data.ISTIO_ENABLED))
   ```

### 问题：配置不生效

1. **重启应用**
   ```bash
   kubectl rollout restart deployment/applaunchpad
   ```

2. **清除浏览器缓存**
   - 强制刷新页面 (Ctrl+F5)

3. **检查日志**
   ```bash
   kubectl logs deployment/applaunchpad | grep -E "Config file|istio"
   ```

## 最佳实践

1. **使用运行时配置**
   - 更灵活，无需重新构建
   - 可以在不同环境使用不同配置

2. **配置公共域名**
   - 将所有公共域名加入 `publicDomains` 列表
   - 减少 Gateway 资源使用

3. **测试验证**
   - 先在开发环境测试
   - 验证资源创建正确后再部署生产

## 相关文件

- 配置示例：`data/config.yaml.istio-example`
- 环境变量文档：`docs/istio-migration/environment-variables.md`
- 技术流程：`ISTIO_FRONTEND_FLOW.md`
- Istio 配置指南：`ISTIO_CONFIGURATION.md`