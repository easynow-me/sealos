# Sealos Ingress 到 Istio 完整迁移执行手册

本文档提供了从零开始将 Sealos 从 Ingress 迁移到 Istio 的完整执行步骤。请按顺序执行每个步骤。

## 前置条件

- Kubernetes 集群版本 >= 1.27
- kubectl 命令行工具已配置
- 集群管理员权限
- 至少 8 CPU, 16GB 内存的可用资源

## 第一步：环境检查和备份

### 1.1 检查当前环境

```bash
# 检查集群版本
kubectl version --short

# 检查当前 Ingress 数量
echo "Total Ingress resources: $(kubectl get ingress --all-namespaces --no-headers | wc -l)"

# 检查用户命名空间
kubectl get namespaces | grep "^ns-" | wc -l

# 检查 LoadBalancer 服务（应该为 0）
kubectl get services --all-namespaces --field-selector spec.type=LoadBalancer | grep "^ns-"
```

### 1.2 创建完整备份

```bash
# 创建备份目录
BACKUP_DIR="/tmp/sealos-backup-$(date +%Y%m%d-%H%M%S)"
mkdir -p $BACKUP_DIR

# 备份所有 Ingress
kubectl get ingress --all-namespaces -o yaml > $BACKUP_DIR/all-ingress.yaml

# 备份所有 Service
kubectl get services --all-namespaces -o yaml > $BACKUP_DIR/all-services.yaml

# 备份所有 Deployment
kubectl get deployments --all-namespaces -o yaml > $BACKUP_DIR/all-deployments.yaml

# 备份控制器配置
kubectl get deployments -n sealos-system -o yaml > $BACKUP_DIR/sealos-controllers.yaml

echo "Backup completed at: $BACKUP_DIR"
```

## 第二步：安装 Istio

### 2.1 下载 Istio

```bash
# 下载 Istio 1.20.x
curl -L https://istio.io/downloadIstio | ISTIO_VERSION=1.20.1 sh -

# 添加到 PATH
cd istio-1.20.1
export PATH=$PWD/bin:$PATH

# 验证安装
istioctl version --remote=false
```

### 2.2 安装 Istio 控制平面

```bash
# 创建 Istio 配置文件
cat > istio-install-config.yaml << EOF
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  name: control-plane
spec:
  profile: production
  components:
    pilot:
      k8s:
        hpaSpec:
          minReplicas: 3
        resources:
          requests:
            cpu: 1000m
            memory: 2048Mi
    ingressGateways:
    - name: istio-ingressgateway
      enabled: true
      k8s:
        hpaSpec:
          minReplicas: 3
        service:
          type: LoadBalancer
        resources:
          requests:
            cpu: 500m
            memory: 512Mi
  meshConfig:
    defaultConfig:
      proxyStatsMatcher:
        inclusionRegexps:
        - ".*outlier_detection.*"
        - ".*circuit_breakers.*"
        - ".*upstream_rq_retry.*"
        - ".*upstream_rq_pending.*"
        - ".*upstream_rq_time.*"
        - ".*_cx_.*"
    accessLogFile: /dev/stdout
  values:
    global:
      proxy:
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 500m
            memory: 512Mi
    telemetry:
      v2:
        prometheus:
          wasmEnabled: false
EOF

# 安装 Istio
istioctl install -f istio-install-config.yaml -y

# 等待 Istio 组件就绪
kubectl -n istio-system rollout status deployment/istiod
kubectl -n istio-system rollout status deployment/istio-ingressgateway
```

### 2.3 配置说明

**注意：Sealos 采用 Gateway-Only 模式，无需启用 sidecar 注入**

```bash
# ❌ 不需要执行：为用户命名空间启用自动注入
# for ns in $(kubectl get namespaces -o name | grep "namespace/ns-" | cut -d/ -f2); do
#   kubectl label namespace $ns istio-injection=enabled --overwrite
# done

# ✅ Sealos 只需要 Istio Gateway 功能，不需要 sidecar
echo "Using Gateway-Only mode - no sidecar injection needed"
```

**原因：**
- Sealos 主要是南北向流量（用户→应用）
- Gateway 层已提供所需的路由、TLS、CORS 等功能  
- 避免不必要的资源开销和复杂性

## 第三步：部署 cert-manager（如果未安装）

```bash
# 检查 cert-manager 是否已安装
if ! kubectl get namespace cert-manager >/dev/null 2>&1; then
  echo "Installing cert-manager..."
  kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.3/cert-manager.yaml
  
  # 等待 cert-manager 就绪
  kubectl -n cert-manager rollout status deployment/cert-manager
  kubectl -n cert-manager rollout status deployment/cert-manager-webhook
  kubectl -n cert-manager rollout status deployment/cert-manager-cainjector
else
  echo "cert-manager already installed"
fi
```

## 第四步：创建通配符证书

```bash
# 创建 ClusterIssuer
cat > cluster-issuer.yaml << EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@sealos.io  # 替换为实际邮箱
    privateKeySecretRef:
      name: letsencrypt-prod
    solvers:
    - dns01:
        cloudflare:
          email: admin@sealos.io  # 替换为实际邮箱
          apiTokenSecretRef:
            name: cloudflare-api-token
            key: api-token
EOF

# 如果使用 HTTP01 验证（替代方案）
cat > cluster-issuer-http.yaml << EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@sealos.io
    privateKeySecretRef:
      name: letsencrypt-prod
    solvers:
    - http01:
        ingress:
          class: istio
EOF

# 应用 ClusterIssuer
kubectl apply -f cluster-issuer-http.yaml

# 创建通配符证书
cat > wildcard-certificate.yaml << EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: wildcard-cert
  namespace: istio-system
spec:
  secretName: wildcard-cert
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
  - "*.cloud.sealos.io"  # 替换为实际域名
  - "cloud.sealos.io"
EOF

kubectl apply -f wildcard-certificate.yaml

# 等待证书颁发
echo "Waiting for certificate to be ready..."
kubectl -n istio-system wait --for=condition=Ready certificate/wildcard-cert --timeout=300s
```

## 第五步：创建默认 Gateway

```bash
# 创建默认 Gateway
cat > default-gateway.yaml << EOF
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
    tls:
      httpsRedirect: true  # 自动重定向到 HTTPS
  - port:
      number: 443
      name: https
      protocol: HTTPS
    hosts:
    - "*.cloud.sealos.io"  # 替换为实际域名
    tls:
      mode: SIMPLE
      credentialName: wildcard-cert
EOF

kubectl apply -f default-gateway.yaml
```

## 第六步：更新 Admission Webhooks

```bash
# 启用 Istio webhooks
kubectl set env deployment/admission-webhook -n sealos-system \
  ENABLE_ISTIO_WEBHOOKS=true \
  VIRTUALSERVICE_MUTATING_ANNOTATIONS="sealos.io/app-name=istio-migrated"

# 重启 webhook
kubectl rollout restart deployment/admission-webhook -n sealos-system
kubectl rollout status deployment/admission-webhook -n sealos-system
```

## 第七步：更新控制器到 Istio 模式

```bash
# 更新所有控制器到双模式
CONTROLLERS=(
  "terminal-controller"
  "adminer-controller"
  "resources-controller"
)

for controller in "${CONTROLLERS[@]}"; do
  echo "Updating $controller to dual mode..."
  kubectl set env deployment/$controller -n sealos-system \
    USE_ISTIO_NETWORKING=true \
    USE_INGRESS_NETWORKING=true \
    ISTIO_ENABLED=true \
    ISTIO_GATEWAY=sealos-gateway.istio-system
  
  kubectl rollout restart deployment/$controller -n sealos-system
  kubectl rollout status deployment/$controller -n sealos-system
done
```

## 第八步：部署监控

```bash
# 克隆项目（如果还没有）
git clone https://github.com/labring/sealos.git
cd sealos

# 设置监控
./scripts/istio-migration/phase6-monitoring-setup.sh --component all \
  --webhook-url "${WEBHOOK_URL:-}" \
  --alert-email "${ALERT_EMAIL:-ops@sealos.io}"
```

## 第九步：执行迁移

### 9.1 禁用新 Ingress 创建

```bash
# 停止创建新的 Ingress
./scripts/istio-migration/phase6-full-cutover.sh --step disable-ingress

# 验证
kubectl get validatingadmissionwebhookconfiguration block-ingress-creation
```

### 9.2 批量迁移存量 Ingress

```bash
# 先做干运行测试
./scripts/istio-migration/phase6-full-cutover.sh --step migrate-existing --dry-run

# 检查输出，确认无误后执行实际迁移
./scripts/istio-migration/phase6-full-cutover.sh --step migrate-existing

# 检查迁移状态
kubectl get virtualservices --all-namespaces | grep "ns-" | wc -l
kubectl get ingress --all-namespaces | grep "migrated-to-istio=true" | wc -l
```

### 9.3 验证功能

```bash
# 运行验证测试
./scripts/istio-migration/phase6-full-cutover.sh --step validate

# 运行完整测试套件
./tests/istio-migration/scripts/run-test-suite.sh all

# 手动验证几个关键服务
# 1. 获取一个示例 VirtualService
kubectl get virtualservice -n ns-admin -o yaml | head -20

# 2. 测试流量
SAMPLE_HOST=$(kubectl get virtualservice --all-namespaces -o json | jq -r '.items[0].spec.hosts[0]' | grep -v null | head -1)
if [ -n "$SAMPLE_HOST" ]; then
  curl -I https://$SAMPLE_HOST
fi
```

## 第十步：性能验证

```bash
# 检查 P95 延迟
kubectl port-forward -n istio-system svc/prometheus 9090:9090 &
PF_PID=$!
sleep 5

# 查询延迟指标
curl -s "http://localhost:9090/api/v1/query?query=histogram_quantile(0.95,sum(rate(istio_request_duration_milliseconds_bucket[5m]))by(le))" | jq -r '.data.result[0].value[1]'

kill $PF_PID 2>/dev/null || true

# 检查错误率
kubectl logs -n istio-system deployment/istiod --tail=100 | grep -i error || echo "No errors found"
```

## 第十一步：清理旧资源

```bash
# 最终确认
echo "Ready to clean up old Ingress resources?"
echo "This will delete all migrated Ingress resources."
read -p "Type 'yes' to continue: " response

if [ "$response" = "yes" ]; then
  # 清理旧资源
  ./scripts/istio-migration/phase6-full-cutover.sh --step cleanup
  
  # 验证清理结果
  echo "Remaining Ingress resources:"
  kubectl get ingress --all-namespaces
fi
```

## 第十二步：切换到 Istio-only 模式

```bash
# 更新控制器到 Istio-only 模式
for controller in "${CONTROLLERS[@]}"; do
  echo "Updating $controller to Istio-only mode..."
  kubectl set env deployment/$controller -n sealos-system \
    USE_ISTIO_NETWORKING=true \
    USE_INGRESS_NETWORKING=false
  
  kubectl rollout restart deployment/$controller -n sealos-system
  kubectl rollout status deployment/$controller -n sealos-system
done
```

## 第十三步：最终验证

```bash
# 检查所有组件状态
echo "=== Istio Components Status ==="
kubectl get pods -n istio-system

echo -e "\n=== VirtualServices Count ==="
kubectl get virtualservices --all-namespaces | grep "ns-" | wc -l

echo -e "\n=== Remaining Ingress Count ==="
kubectl get ingress --all-namespaces | grep "ns-" | wc -l

echo -e "\n=== Sample Traffic Test ==="
kubectl get virtualservice --all-namespaces -o json | \
  jq -r '.items[0:3] | .[].spec.hosts[0]' | \
  while read host; do
    if [ "$host" != "null" ] && [ -n "$host" ]; then
      echo "Testing $host..."
      curl -s -o /dev/null -w "%{http_code}" https://$host || echo "Failed"
    fi
  done
```

## 第十四步：监控和观察

```bash
# 查看 Grafana 仪表板
kubectl port-forward -n sealos-monitoring svc/grafana 3000:3000 &
echo "Grafana available at: http://localhost:3000"

# 查看 Prometheus
kubectl port-forward -n istio-system svc/prometheus 9090:9090 &
echo "Prometheus available at: http://localhost:9090"

# 检查告警
kubectl get prometheusrule -n sealos-monitoring
kubectl logs -n sealos-monitoring deployment/alertmanager | tail -20
```

## 故障排查

如果遇到问题：

### 1. 检查 Istio 状态
```bash
istioctl proxy-status
istioctl analyze --all-namespaces
```

### 2. 查看具体服务的配置
```bash
# 替换 <namespace> 和 <deployment>
istioctl proxy-config all deploy/<deployment> -n <namespace>
```

### 3. 检查日志
```bash
# Istiod 日志
kubectl logs -n istio-system deployment/istiod --tail=100

# IngressGateway 日志  
kubectl logs -n istio-system deployment/istio-ingressgateway --tail=100

# 特定 Pod 的 sidecar 日志
kubectl logs <pod-name> -n <namespace> -c istio-proxy --tail=100
```

### 4. 紧急回滚
```bash
# 如果需要紧急回滚
./scripts/istio-migration/emergency-rollback.sh --mode full
```

## 完成标志

迁移成功完成的标志：
- ✅ 所有用户命名空间的 Ingress 已迁移到 VirtualService
- ✅ 监控显示 P95 延迟增加 < 15%
- ✅ 错误率 < 0.1%
- ✅ 所有功能测试通过
- ✅ 24小时运行稳定

## 后续维护

1. **日常监控**
   - 查看 Grafana 仪表板
   - 检查 AlertManager 告警
   - 定期查看性能报告

2. **配置优化**
   - 根据实际负载调整 HPA 配置
   - 优化 Envoy 代理配置
   - 调整超时和重试策略

3. **功能增强**
   - 启用熔断器
   - 配置流量镜像
   - 实施金丝雀发布

---

**注意事项**：
- 整个迁移过程预计需要 2-4 小时
- 建议在低峰期执行
- 保持与团队的实时沟通
- 记录所有异常和处理方法