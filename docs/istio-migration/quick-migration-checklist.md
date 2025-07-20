# Istio 迁移快速执行清单

## 🚀 快速迁移脚本（一键执行）

如果您已经熟悉流程，可以使用此脚本快速完成迁移：

```bash
#!/bin/bash
# quick-migrate-to-istio.sh

set -e

echo "Starting Sealos Istio Migration..."

# 1. 备份
echo "Step 1: Creating backup..."
BACKUP_DIR="/tmp/sealos-backup-$(date +%Y%m%d-%H%M%S)"
mkdir -p $BACKUP_DIR
kubectl get ingress --all-namespaces -o yaml > $BACKUP_DIR/all-ingress.yaml
kubectl get services --all-namespaces -o yaml > $BACKUP_DIR/all-services.yaml
echo "Backup saved to: $BACKUP_DIR"

# 2. 安装 Istio（如果未安装）
if ! kubectl get namespace istio-system >/dev/null 2>&1; then
  echo "Step 2: Installing Istio..."
  curl -L https://istio.io/downloadIstio | ISTIO_VERSION=1.20.1 sh -
  cd istio-1.20.1
  export PATH=$PWD/bin:$PATH
  
  # 使用生产配置安装
  istioctl install --set profile=production -y
  cd ..
else
  echo "Step 2: Istio already installed"
fi

# 3. 启用自动注入
echo "Step 3: Enabling istio-injection..."
for ns in $(kubectl get namespaces -o name | grep "namespace/ns-" | cut -d/ -f2); do
  kubectl label namespace $ns istio-injection=enabled --overwrite
done

# 4. 更新控制器
echo "Step 4: Updating controllers..."
CONTROLLERS=("terminal-controller" "adminer-controller" "resources-controller")
for controller in "${CONTROLLERS[@]}"; do
  kubectl set env deployment/$controller -n sealos-system \
    USE_ISTIO_NETWORKING=true \
    ISTIO_ENABLED=true \
    ISTIO_GATEWAY=sealos-gateway.istio-system
  kubectl rollout restart deployment/$controller -n sealos-system
done

# 5. 等待控制器就绪
echo "Step 5: Waiting for controllers..."
for controller in "${CONTROLLERS[@]}"; do
  kubectl rollout status deployment/$controller -n sealos-system --timeout=300s
done

# 6. 执行迁移
echo "Step 6: Running migration..."
cd /path/to/sealos  # 替换为实际路径
./scripts/istio-migration/phase6-full-cutover.sh --step all --force

echo "Migration completed!"
```

## 📋 手动执行清单

### 前置准备（10分钟）

- [ ] 确认集群管理员权限
- [ ] 确认维护窗口时间
- [ ] 通知相关团队
- [ ] 准备回滚方案

### 第一阶段：环境准备（20分钟）

```bash
# 1. 备份当前配置
kubectl get ingress --all-namespaces -o yaml > backup-ingress-$(date +%Y%m%d).yaml

# 2. 安装 Istio
curl -L https://istio.io/downloadIstio | ISTIO_VERSION=1.20.1 sh -
cd istio-1.20.1 && export PATH=$PWD/bin:$PATH
istioctl install --set profile=production -y

# 3. 为用户命名空间启用注入
kubectl label namespace --all istio-injection=enabled --overwrite
```

### 第二阶段：配置更新（15分钟）

```bash
# 1. 创建默认 Gateway
kubectl apply -f - <<EOF
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: sealos-gateway
  namespace: istio-system
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
  - port:
      number: 443
      name: https
      protocol: HTTPS
    hosts:
    - "*.cloud.sealos.io"
    tls:
      mode: SIMPLE
      credentialName: wildcard-cert
EOF

# 2. 更新 Webhooks
kubectl set env deployment/admission-webhook -n sealos-system ENABLE_ISTIO_WEBHOOKS=true

# 3. 更新控制器
for ctrl in terminal-controller adminer-controller resources-controller; do
  kubectl set env deployment/$ctrl -n sealos-system \
    USE_ISTIO_NETWORKING=true \
    ISTIO_ENABLED=true
  kubectl rollout restart deployment/$ctrl -n sealos-system
done
```

### 第三阶段：执行迁移（30分钟）

```bash
# 1. 禁用新 Ingress
./scripts/istio-migration/phase6-full-cutover.sh --step disable-ingress

# 2. 迁移存量资源
./scripts/istio-migration/phase6-full-cutover.sh --step migrate-existing

# 3. 验证功能
./scripts/istio-migration/phase6-full-cutover.sh --step validate

# 4. 清理旧资源
./scripts/istio-migration/phase6-full-cutover.sh --step cleanup
```

### 第四阶段：验证监控（15分钟）

```bash
# 1. 检查迁移结果
echo "VirtualServices: $(kubectl get virtualservices --all-namespaces | grep ns- | wc -l)"
echo "Remaining Ingress: $(kubectl get ingress --all-namespaces | grep ns- | wc -l)"

# 2. 检查服务状态
istioctl proxy-status

# 3. 测试流量
kubectl get virtualservice --all-namespaces -o json | \
  jq -r '.items[0].spec.hosts[0]' | \
  xargs -I {} curl -s -o /dev/null -w "%{http_code}\n" https://{}
```

## ⚡ 紧急命令速查

### 查看状态
```bash
# Istio 组件状态
kubectl get pods -n istio-system

# 代理状态
istioctl proxy-status

# 配置分析
istioctl analyze --all-namespaces
```

### 故障排查
```bash
# 查看 Istiod 日志
kubectl logs -n istio-system deployment/istiod --tail=50

# 查看网关日志
kubectl logs -n istio-system deployment/istio-ingressgateway --tail=50

# 查看特定服务配置
istioctl proxy-config all deploy/<deployment> -n <namespace>
```

### 紧急回滚
```bash
# 完全回滚到 Ingress
./scripts/istio-migration/emergency-rollback.sh --mode full

# 部分回滚（保持双模式）
./scripts/istio-migration/emergency-rollback.sh --mode partial
```

## 📊 成功指标

- ✅ 所有 VirtualService 创建成功
- ✅ 流量测试返回 200/300 状态码
- ✅ P95 延迟 < 原来的 1.15 倍
- ✅ 错误率 < 0.1%
- ✅ 无用户投诉

## 🔧 常见问题处理

### 1. 证书问题
```bash
# 检查证书状态
kubectl get certificate -n istio-system

# 手动创建自签名证书（临时方案）
kubectl create secret tls wildcard-cert \
  --cert=path/to/cert.pem \
  --key=path/to/key.pem \
  -n istio-system
```

### 2. 流量不通
```bash
# 检查 VirtualService 配置
kubectl get virtualservice <name> -n <namespace> -o yaml

# 检查 Gateway 关联
kubectl get gateway -n istio-system

# 测试内部连通性
kubectl exec -it <pod> -n <namespace> -- curl http://service-name
```

### 3. 性能问题
```bash
# 调整代理资源
kubectl set resources deployment/<deployment> -n <namespace> \
  -c istio-proxy \
  --requests=cpu=100m,memory=128Mi \
  --limits=cpu=500m,memory=512Mi
```

## 📞 支持联系

- Slack: #istio-migration
- 紧急电话: xxx-xxxx-xxxx
- 邮箱: istio-support@sealos.io

---

**提示**：建议打印此清单，在执行时逐项勾选确认。