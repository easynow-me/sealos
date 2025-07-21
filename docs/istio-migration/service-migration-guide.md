# 🎯 Service Istio Gateway Migration Guide

本文档指导如何将 Sealos 服务从传统 Ingress 迁移到优化的 Istio Gateway 配置。

## 📋 概述

按照 [Gateway 优化方案](./gateway-optimization-plan.md)，以下服务已被优化以使用智能 Gateway 选择：

### 已迁移的服务

1. **Account Service** (`account-api.{{ .cloudDomain }}`)
   - 从 `service/account/deploy/manifests/ingress.yaml.tmpl` 
   - 迁移到 `service/account/deploy/manifests/gateway.yaml.tmpl`

2. **License Service** (`{{ .cloudDomain }}`)
   - 从 `service/license/deploy/manifests/ingress.yaml.tmpl`
   - 迁移到 `service/license/deploy/manifests/gateway.yaml.tmpl`

## 🎯 优化收益

### 智能 Gateway 选择
- **公共域名**: 自动使用 `istio-system/sealos-gateway` 共享 Gateway
- **自定义域名**: 创建专用 Gateway（如需要）
- **资源减少**: 实现 81% 的 Gateway 资源减少

### 功能保持
- ✅ CORS 配置完全迁移
- ✅ 安全头部保持一致
- ✅ TLS 配置自动处理
- ✅ 超时和路由规则优化

## 🚀 部署指南

### 1. 前置条件

确保 Istio 已安装并配置：

```bash
# 检查 Istio 状态
kubectl get pods -n istio-system

# 确认共享 Gateway 存在
kubectl get gateway -n istio-system sealos-gateway
```

### 2. Account Service 迁移

```bash
# 应用新的 Istio 配置
kubectl apply -f service/account/deploy/manifests/gateway.yaml.tmpl

# 验证 VirtualService 创建
kubectl get virtualservice -n account-system account-service-vs

# 检查路由配置
kubectl describe virtualservice -n account-system account-service-vs
```

### 3. License Service 迁移

```bash
# 应用新的 Istio 配置
kubectl apply -f service/license/deploy/manifests/gateway.yaml.tmpl

# 验证 VirtualService 创建
kubectl get virtualservice -n sealos desktop-frontend-vs

# 检查路由配置
kubectl describe virtualservice -n sealos desktop-frontend-vs
```

### 4. 清理旧 Ingress 资源

⚠️ **注意**: 只有在确认 Istio 配置工作正常后才执行清理

```bash
# 删除旧的 Ingress 资源
kubectl delete ingress -n account-system account-service
kubectl delete ingress -n sealos sealos-desktop
```

## 🔍 验证和测试

### 1. 连通性测试

```bash
# 测试 Account Service
curl -I https://account-api.{{ .cloudDomain }}/api/v1/health

# 测试 License Service  
curl -I https://{{ .cloudDomain }}/
```

### 2. CORS 测试

```bash
# 测试 Account Service CORS
curl -H "Origin: https://{{ .cloudDomain }}" \
     -H "Access-Control-Request-Method: POST" \
     -H "Access-Control-Request-Headers: content-type" \
     -X OPTIONS \
     https://account-api.{{ .cloudDomain }}/api/v1/account
```

### 3. 安全头部验证

```bash
# 检查安全头部
curl -I https://account-api.{{ .cloudDomain }}/ | grep -E "(Content-Security-Policy|X-Xss-Protection)"
```

## 📊 监控和观察

### 1. 流量监控

```bash
# 查看 VirtualService 状态
kubectl get virtualservice -A -l sealos.io/gateway-type=optimized

# 检查 Gateway 使用情况
kubectl get gateway -A
```

### 2. Istio 指标

使用 Kiali 或 Prometheus 监控：
- 请求延迟
- 成功率
- 流量分布
- Gateway 负载

## 🔧 故障排除

### 1. VirtualService 不工作

```bash
# 检查 VirtualService 配置
kubectl describe virtualservice -n account-system account-service-vs

# 检查 Gateway 状态
kubectl describe gateway -n istio-system sealos-gateway

# 查看 Envoy 配置
istioctl proxy-config route <pod-name> -n istio-system
```

### 2. CORS 问题

```bash
# 检查 VirtualService CORS 配置
kubectl get virtualservice account-service-vs -n account-system -o yaml | grep -A 10 corsPolicy
```

### 3. 证书问题

```bash
# 检查 TLS 证书
kubectl get secret -n istio-system {{ .certSecretName }}

# 验证证书有效性
kubectl describe secret -n istio-system {{ .certSecretName }}
```

## 🔄 回滚计划

如果需要回滚到 Ingress：

```bash
# 重新应用 Ingress 配置
kubectl apply -f service/account/deploy/manifests/ingress.yaml.tmpl
kubectl apply -f service/license/deploy/manifests/ingress.yaml.tmpl

# 删除 Istio 配置
kubectl delete virtualservice -n account-system account-service-vs
kubectl delete virtualservice -n sealos desktop-frontend-vs
```

## 📈 性能优化建议

### 1. 缓存配置

考虑在 VirtualService 中添加缓存头：

```yaml
headers:
  response:
    set:
      Cache-Control: "public, max-age=3600"  # 1小时缓存
```

### 2. 压缩配置

在 Istio Gateway 级别启用压缩：

```yaml
spec:
  servers:
    - port:
        number: 443
        name: https
        protocol: HTTPS
      tls:
        mode: SIMPLE
        credentialName: {{ .certSecretName }}
      hosts:
        - "*.{{ .cloudDomain }}"
```

## 📚 相关文档

- [Gateway 优化方案](./gateway-optimization-plan.md)
- [Istio 迁移工具](../tools/istio-migration/)
- [验证脚本使用指南](./validation-guide.md)