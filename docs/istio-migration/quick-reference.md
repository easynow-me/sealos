# Istio 配置快速参考

## AppLaunchpad 运行时配置

### 最小配置（/app/data/config.yaml）
```yaml
istio:
  enabled: true
```

### 完整配置
```yaml
istio:
  enabled: true                    # 必需：启用 Istio
  publicDomains:                  # 可选：公共域名列表
    - 'cloud.sealos.io'
    - '*.cloud.sealos.io'
  sharedGateway: 'sealos-gateway' # 可选：共享网关名称
  enableTracing: false            # 可选：分布式追踪
```

## 环境变量对照表

| 功能 | AppLaunchpad (构建时) | Devbox (待实现) | 说明 |
|------|---------------------|----------------|------|
| 启用 Istio | `NEXT_PUBLIC_USE_ISTIO=true` | `USE_ISTIO=true` | 主开关 |
| 备用开关 | `NEXT_PUBLIC_ENABLE_ISTIO=true` | `ISTIO_ENABLED=true` | 兼容性 |
| 公共域名 | 通过配置文件设置 | `ISTIO_PUBLIC_DOMAINS` | 逗号分隔 |
| 共享网关 | 通过配置文件设置 | `ISTIO_SHARED_GATEWAY` | 默认：sealos-gateway |
| 链路追踪 | `NEXT_PUBLIC_ENABLE_TRACING=true` | `ISTIO_ENABLE_TRACING` | 默认：false |

## 部署配置示例

### Kubernetes ConfigMap
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-istio-config
data:
  config.yaml: |
    istio:
      enabled: true
      publicDomains: ["cloud.sealos.io"]
      sharedGateway: "sealos-gateway"
```

### Docker 运行
```bash
# AppLaunchpad (运行时配置)
docker run -d \
  -v ./config.yaml:/app/data/config.yaml \
  sealos/applaunchpad:latest

# Devbox (环境变量)
docker run -d \
  -e USE_ISTIO=true \
  -e ISTIO_PUBLIC_DOMAINS=cloud.sealos.io \
  sealos/devbox:latest
```

## 验证命令

```bash
# 检查是否创建了 VirtualService
kubectl get virtualservice -n ns-xxx

# 检查是否未创建 Ingress
kubectl get ingress -n ns-xxx

# 查看当前配置
kubectl exec deployment/applaunchpad -- cat /app/data/config.yaml | grep -A5 istio
```

## 常见问题

1. **Q: 设置环境变量后仍创建 Ingress？**
   - A: AppLaunchpad 需要使用配置文件，环境变量仅在构建时生效

2. **Q: 如何知道使用的是共享还是独立 Gateway？**
   - A: 公共域名使用共享 Gateway，自定义域名创建独立 Gateway

3. **Q: 需要重启应用吗？**
   - A: 是的，修改配置后需要重启：`kubectl rollout restart deployment/app-name`