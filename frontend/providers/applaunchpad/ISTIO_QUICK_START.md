# AppLaunchpad Istio 快速启用指南

## 🚀 30秒启用 Istio

### 生产环境

```bash
# 1. 创建配置
kubectl create configmap applaunchpad-istio-config -n sealos --from-literal=config.yaml='
istio:
  enabled: true
  publicDomains:
    - "cloud.sealos.io"
    - "*.cloud.sealos.io"
'

# 2. 挂载到 Pod
kubectl patch deployment applaunchpad -n sealos -p '
spec:
  template:
    spec:
      containers:
      - name: applaunchpad
        volumeMounts:
        - name: istio-config
          mountPath: /app/data
      volumes:
      - name: istio-config
        configMap:
          name: applaunchpad-istio-config
'

# 3. 重启应用
kubectl rollout restart deployment/applaunchpad -n sealos
```

### 开发环境

```bash
# 1. 创建配置文件
cat > frontend/providers/applaunchpad/data/config.yaml.local << EOF
istio:
  enabled: true
EOF

# 2. 启动开发服务器
npm run dev
```

## ✅ 验证是否生效

```bash
# 方法1：检查 API
curl localhost:3000/api/platform/getInitData | grep ISTIO_ENABLED
# 应显示: "ISTIO_ENABLED": true

# 方法2：创建应用后检查资源
kubectl get virtualservice,gateway -n ns-xxx
# 应该看到 VirtualService 和 Gateway

kubectl get ingress -n ns-xxx  
# 应该为空（没有 Ingress）
```

## 📝 最简配置

只需要这一行即可启用：

```yaml
istio:
  enabled: true
```

## 🔧 完整配置选项

```yaml
istio:
  enabled: true                    # 启用 Istio
  publicDomains:                  # 公共域名（可选）
    - "your-domain.com"
  sharedGateway: "gateway-name"   # 共享网关（可选）
  enableTracing: false            # 链路追踪（可选）
```

## ❓ 常见问题

**Q: 为什么还在创建 Ingress？**
A: 重启应用：`kubectl rollout restart deployment/applaunchpad`

**Q: 如何知道正在使用 Istio？**
A: 检查 API 响应中的 `ISTIO_ENABLED` 字段

**Q: 需要修改代码吗？**
A: 不需要！只需配置文件

## 🎯 关键点

1. **运行时配置** - 无需重新构建
2. **配置文件位置** - `/app/data/config.yaml`
3. **立即生效** - 重启后新建的应用使用 Istio