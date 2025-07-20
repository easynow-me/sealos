# Sealos Istio 迁移用户指南

## 概述

本指南面向 Sealos 用户，详细说明从 Kubernetes Ingress 到 Istio Gateway/VirtualService 迁移后的功能变化、新特性和使用方法。

## 迁移后的主要变化

### 对用户透明的改进

✅ **无需更改操作习惯** - 所有现有的应用部署和管理流程保持不变

✅ **更好的性能** - 网络延迟优化，连接处理更高效

✅ **增强的安全性** - 更细粒度的访问控制和流量加密

✅ **更强的可观测性** - 详细的流量监控和链路追踪

### 新增功能

🆕 **高级流量管理** - 支持更复杂的路由规则和流量分发

🆕 **金丝雀发布** - 内置的渐进式发布能力

🆕 **故障注入** - 混沌工程和韧性测试支持

🆕 **超时和重试** - 更智能的故障恢复机制

## 功能对比

| 功能 | Ingress (迁移前) | Istio (迁移后) | 说明 |
|------|------------------|----------------|------|
| 基本 HTTP 路由 | ✅ | ✅ | 功能保持一致 |
| HTTPS/TLS 终止 | ✅ | ✅ | 自动证书管理 |
| WebSocket 支持 | ✅ | ✅ | 性能优化 |
| gRPC 支持 | ⚠️ 有限 | ✅ | 完整支持 |
| 负载均衡 | ✅ 基础 | ✅ 高级 | 多种算法选择 |
| 熔断保护 | ❌ | ✅ | 新增功能 |
| 请求重试 | ❌ | ✅ | 新增功能 |
| 流量镜像 | ❌ | ✅ | 新增功能 |
| 细粒度监控 | ⚠️ 有限 | ✅ | 全面提升 |

## 应用部署指南

### 1. Terminal 应用部署

#### 迁移前 (Ingress)
```yaml
# 自动生成的 Ingress 配置
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: my-terminal
  annotations:
    nginx.ingress.kubernetes.io/backend-protocol: "WS"
spec:
  rules:
  - host: terminal-abc123.cloud.sealos.io
    http:
      paths:
      - path: /
        backend:
          service:
            name: my-terminal
            port:
              number: 8080
```

#### 迁移后 (Istio)
```yaml
# 自动生成的 Gateway + VirtualService 配置
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: my-terminal-gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 443
      name: https
      protocol: HTTPS
    hosts:
    - terminal-abc123.cloud.sealos.io
    tls:
      mode: SIMPLE
      credentialName: wildcard-cert
---
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: my-terminal-vs
spec:
  hosts:
  - terminal-abc123.cloud.sealos.io
  gateways:
  - my-terminal-gateway
  http:
  - match:
    - headers:
        upgrade:
          exact: websocket
    route:
    - destination:
        host: my-terminal
    timeout: 0s  # 无超时限制，支持长连接
```

**用户体验改进：**
- ✅ 更稳定的 WebSocket 连接
- ✅ 更快的连接建立时间
- ✅ 自动故障恢复

### 2. 数据库管理应用

#### 新增的高级功能

```yaml
# 带有安全增强的 VirtualService
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: database-admin-vs
spec:
  hosts:
  - dbadmin-xyz789.cloud.sealos.io
  gateways:
  - database-admin-gateway
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: database-admin-service
    headers:
      request:
        set:
          X-Frame-Options: "DENY"
          X-Content-Type-Options: "nosniff"
          X-XSS-Protection: "1; mode=block"
    corsPolicy:
      allowOrigins:
      - exact: "https://cloud.sealos.io"
      allowMethods:
      - GET
      - POST
      allowHeaders:
      - content-type
      - authorization
```

### 3. 应用启动台部署

#### 金丝雀发布示例

```yaml
# 支持金丝雀发布的 VirtualService
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: my-app-canary-vs
spec:
  hosts:
  - myapp-def456.cloud.sealos.io
  http:
  - match:
    - headers:
        canary:
          exact: "true"
    route:
    - destination:
        host: my-app-service
        subset: v2  # 新版本
  - route:
    - destination:
        host: my-app-service
        subset: v1  # 稳定版本
      weight: 90
    - destination:
        host: my-app-service 
        subset: v2  # 新版本
      weight: 10  # 10% 流量到新版本
```

## 开发者指南

### 1. 本地开发环境配置

#### 端口转发设置
```bash
# 迁移前：直接访问 Ingress
kubectl port-forward svc/nginx-ingress-controller 8080:80

# 迁移后：通过 Istio Gateway
kubectl port-forward svc/istio-ingressgateway 8080:80 -n istio-system
```

#### 本地测试
```bash
# 设置本地 hosts 文件
echo "127.0.0.1 myapp-local.cloud.sealos.io" >> /etc/hosts

# 测试应用访问
curl -H "Host: myapp-local.cloud.sealos.io" http://localhost:8080/
```

### 2. 调试和故障排查

#### 查看流量路由
```bash
# 查看 Gateway 配置
kubectl get gateway -n my-namespace

# 查看 VirtualService 配置
kubectl get virtualservice -n my-namespace

# 查看实际路由规则
istioctl proxy-config route deployment/my-app
```

#### 流量追踪
```bash
# 启用追踪
kubectl label namespace my-namespace istio-injection=enabled

# 查看追踪信息
istioctl dashboard jaeger
```

#### 常见问题排查

**问题1：应用无法访问**
```bash
# 检查 Gateway 状态
kubectl describe gateway my-gateway -n my-namespace

# 检查 VirtualService 状态  
kubectl describe virtualservice my-vs -n my-namespace

# 检查 Istio 配置同步
istioctl proxy-status
```

**问题2：证书问题**
```bash
# 检查证书配置
kubectl get secret -n istio-system | grep cert

# 验证证书有效性
openssl s_client -connect myapp.cloud.sealos.io:443 -servername myapp.cloud.sealos.io
```

### 3. 性能优化

#### 连接池配置
```yaml
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: my-app-destination
spec:
  host: my-app-service
  trafficPolicy:
    connectionPool:
      tcp:
        maxConnections: 100
      http:
        http1MaxPendingRequests: 100
        http2MaxRequests: 1000
        maxRequestsPerConnection: 2
```

#### 熔断配置
```yaml
apiVersion: networking.istio.io/v1beta1  
kind: DestinationRule
metadata:
  name: my-app-circuit-breaker
spec:
  host: my-app-service
  trafficPolicy:
    outlierDetection:
      consecutiveErrors: 3
      interval: 30s
      baseEjectionTime: 30s
      maxEjectionPercent: 50
```

## 监控和可观测性

### 1. 内置监控指标

迁移后自动提供以下监控能力：

#### 请求指标
- 请求成功率
- 请求延迟 (P50, P90, P95, P99)
- 每秒请求数 (RPS)
- 错误率分析

#### 流量指标
- 入站/出站流量统计
- 协议分布 (HTTP/gRPC/WebSocket)
- 地理位置分布

#### 性能指标
- 连接持续时间
- 队列等待时间
- 重试次数统计

### 2. 可视化面板

#### Grafana 仪表板访问
```bash
# 访问监控面板
kubectl port-forward svc/grafana 3000:3000 -n monitoring

# 默认仪表板
- Istio Service Dashboard
- Istio Workload Dashboard  
- Istio Performance Dashboard
```

#### 关键指标查询

**成功率监控**
```promql
sum(rate(istio_requests_total{response_code!~"5.*"}[5m])) / 
sum(rate(istio_requests_total[5m]))
```

**延迟监控**
```promql
histogram_quantile(0.95, 
  sum(rate(istio_request_duration_milliseconds_bucket[5m])) by (le)
)
```

### 3. 告警配置

系统自动配置以下告警：

- 🚨 错误率超过 5%
- 🚨 P95 延迟超过 500ms
- 🚨 服务不可用
- ⚠️ 流量异常下降

## 最佳实践

### 1. 应用设计建议

#### 健康检查
```yaml
# 改进的健康检查配置
apiVersion: v1
kind: Service
metadata:
  name: my-app
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "8080"
    prometheus.io/path: "/metrics"
spec:
  ports:
  - port: 8080
    name: http-monitoring
  - port: 9080
    name: http-health
```

#### 超时配置
```yaml
# 在 VirtualService 中配置合理的超时
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: my-app-vs
spec:
  http:
  - route:
    - destination:
        host: my-app
    timeout: 30s  # API 请求
  - match:
    - headers:
        upgrade:
          exact: websocket
    route:
    - destination:
        host: my-app  
    timeout: 0s   # WebSocket 长连接
```

### 2. 安全最佳实践

#### 网络策略
```yaml
# 基于 Istio 的网络隔离
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: my-app-authz
spec:
  selector:
    matchLabels:
      app: my-app
  rules:
  - from:
    - source:
        principals: ["cluster.local/ns/istio-system/sa/istio-ingressgateway-service-account"]
  - to:
    - operation:
        methods: ["GET", "POST"]
```

#### 证书管理
```yaml
# 自动证书续期配置
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: my-app-cert
spec:
  secretName: my-app-tls
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
  - myapp.cloud.sealos.io
```

### 3. 性能优化建议

#### 缓存策略
```yaml
# VirtualService 中添加缓存头
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: my-app-cache-vs
spec:
  http:
  - match:
    - uri:
        prefix: "/static"
    route:
    - destination:
        host: my-app
    headers:
      response:
        set:
          Cache-Control: "public, max-age=86400"
```

#### 压缩配置
```yaml
# EnvoyFilter 配置响应压缩
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: compression-filter
spec:
  configPatches:
  - applyTo: HTTP_FILTER
    match:
      context: SIDECAR_INBOUND
      listener:
        filterChain:
          filter:
            name: "envoy.filters.network.http_connection_manager"
    patch:
      operation: INSERT_BEFORE
      value:
        name: envoy.filters.http.compressor
        typed_config:
          "@type": type.googleapis.com/envoy.extensions.filters.http.compressor.v3.Compressor
          response_direction_config:
            common_config:
              enabled:
                default_value: true
```

## FAQ 常见问题

### Q1: 迁移后应用访问地址会变化吗？
**A**: 不会。所有应用的访问地址保持完全不变，用户无需更新任何配置。

### Q2: 性能会受到影响吗？  
**A**: 整体性能会有提升。虽然 Istio 增加了网络层，但通过优化配置，延迟增加控制在 10-15% 以内，同时获得更好的稳定性。

### Q3: 如何查看应用的监控数据？
**A**: 可以通过 Grafana 面板查看详细的流量和性能指标，包括请求量、延迟、错误率等。

### Q4: 应用部署流程有变化吗？
**A**: 没有变化。所有现有的部署模板和 CI/CD 流程继续有效。

### Q5: 如何启用新的 Istio 功能？
**A**: 可以通过在应用的 YAML 配置中添加相应的 Istio 资源（如 DestinationRule、ServiceEntry）来启用高级功能。

### Q6: 遇到问题如何排查？
**A**: 
1. 检查应用 Pod 状态：`kubectl get pods`
2. 查看 Istio 配置：`istioctl proxy-config route <pod>`
3. 检查监控面板中的指标
4. 联系运维团队获取支持

## 支持和帮助

### 技术支持渠道
- 📧 技术支持邮箱：support@sealos.io
- 💬 Slack 频道：#sealos-support
- 📖 在线文档：https://docs.sealos.io
- 🐛 问题反馈：https://github.com/labring/sealos/issues

### 培训资源
- 📹 Istio 迁移培训视频
- 📚 最佳实践指南
- 🛠️ 故障排查手册
- 💡 性能优化建议

## 总结

Istio 迁移为 Sealos 用户带来了更强大、更稳定、更安全的网络能力，同时保持了现有操作的简单性。用户可以立即享受到性能提升和新功能，无需任何学习成本。

如有任何问题或需要帮助，请随时联系我们的技术支持团队。