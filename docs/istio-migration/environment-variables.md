# Istio 环境变量配置指南

本文档详细说明了在 Sealos 各个前端应用中启用 Istio 模式所需的环境变量配置。

## 目录

- [概述](#概述)
- [AppLaunchpad 配置](#applaunchpad-配置)
- [Devbox 配置](#devbox-配置)
- [配置方式](#配置方式)
- [验证配置](#验证配置)
- [故障排查](#故障排查)

## 概述

Sealos 前端应用支持两种网络模式：
- **Ingress 模式**（默认）：使用传统的 Kubernetes Ingress 资源
- **Istio 模式**：使用 Istio VirtualService 和 Gateway 资源

切换到 Istio 模式可以获得更好的流量管理、安全性和可观测性。

## AppLaunchpad 配置

### 方法一：运行时配置（推荐）

通过配置文件动态启用 Istio，无需重新构建应用。

#### 开发环境配置
创建 `frontend/providers/applaunchpad/data/config.yaml.local`：

```yaml
# 基础配置
cloud:
  domain: 'cloud.sealos.io'
  port: ''
  userDomains:
    - name: 'cloud.sealos.io'
      secretName: 'wildcard-cert'
  desktopDomain: 'cloud.sealos.io'

# Istio 配置
istio:
  enabled: true                    # 启用 Istio 模式
  publicDomains:                  # 公共域名列表（使用共享 Gateway）
    - 'cloud.sealos.io'
    - '*.cloud.sealos.io'
    - 'sealos.io'
    - '*.sealos.io'
  sharedGateway: 'sealos-gateway' # 共享 Gateway 名称
  enableTracing: false            # 启用分布式追踪

# 其他配置保持默认...
```

#### 生产环境配置
在容器中修改 `/app/data/config.yaml`：

```yaml
istio:
  enabled: true
  publicDomains:
    - 'your-domain.com'
    - '*.your-domain.com'
  sharedGateway: 'istio-system/your-gateway'
  enableTracing: true
```

### 方法二：构建时环境变量

在构建镜像时设置环境变量：

```bash
# .env.local 或 Dockerfile
NEXT_PUBLIC_USE_ISTIO=true
NEXT_PUBLIC_ENABLE_ISTIO=true
NEXT_PUBLIC_ISTIO_ENABLED=true
NEXT_PUBLIC_ENABLE_TRACING=false
```

### Kubernetes 部署配置

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: applaunchpad-config
  namespace: sealos
data:
  config.yaml: |
    istio:
      enabled: true
      publicDomains:
        - 'cloud.sealos.io'
        - '*.cloud.sealos.io'
      sharedGateway: 'sealos-gateway'
      enableTracing: false
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: applaunchpad
  namespace: sealos
spec:
  template:
    spec:
      containers:
      - name: applaunchpad
        volumeMounts:
        - name: config
          mountPath: /app/data
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: applaunchpad-config
```

## Devbox 配置

### 环境变量配置

Devbox 现在支持通过环境变量启用 Istio 模式。

```bash
# 启用 Istio 模式
USE_ISTIO=true
# 或
ISTIO_ENABLED=true

# Istio 相关配置
ISTIO_PUBLIC_DOMAINS=cloud.sealos.io,*.cloud.sealos.io
ISTIO_SHARED_GATEWAY=istio-system/sealos-gateway
ISTIO_ENABLE_TRACING=false
```

### 开发环境配置
创建 `.env.local` 文件：

```bash
# 启用 Istio
ISTIO_ENABLED=true

# 公共域名列表（逗号分隔）
ISTIO_PUBLIC_DOMAINS=cloud.sealos.io,*.cloud.sealos.io

# 共享 Gateway
ISTIO_SHARED_GATEWAY=istio-system/sealos-gateway

# 其他配置
SEALOS_DOMAIN=cloud.sealos.io
INGRESS_SECRET=wildcard-cert
```

### Kubernetes 部署配置

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: devbox
  namespace: sealos
spec:
  template:
    spec:
      containers:
      - name: devbox
        env:
        - name: USE_ISTIO
          value: "true"
        - name: ISTIO_PUBLIC_DOMAINS
          value: "cloud.sealos.io,*.cloud.sealos.io"
        - name: ISTIO_SHARED_GATEWAY
          value: "istio-system/sealos-gateway"
```

## 配置方式

### 1. Docker Compose

```yaml
version: '3.8'
services:
  applaunchpad:
    image: sealos/applaunchpad:latest
    volumes:
      - ./config.yaml:/app/data/config.yaml
    environment:
      - NODE_ENV=production

  devbox:
    image: sealos/devbox:latest
    environment:
      - USE_ISTIO=true
      - ISTIO_PUBLIC_DOMAINS=cloud.sealos.io
      - ISTIO_SHARED_GATEWAY=sealos-gateway
```

### 2. Helm Values

```yaml
# applaunchpad/values.yaml
applaunchpad:
  config:
    istio:
      enabled: true
      publicDomains:
        - "cloud.sealos.io"
        - "*.cloud.sealos.io"
      sharedGateway: "sealos-gateway"
      enableTracing: false

# devbox/values.yaml
devbox:
  env:
    USE_ISTIO: "true"
    ISTIO_PUBLIC_DOMAINS: "cloud.sealos.io,*.cloud.sealos.io"
    ISTIO_SHARED_GATEWAY: "sealos-gateway"
```

### 3. 环境特定配置

```bash
# 开发环境
export ISTIO_ENABLED=false
export ISTIO_PUBLIC_DOMAINS=dev.sealos.io

# 测试环境
export ISTIO_ENABLED=true
export ISTIO_PUBLIC_DOMAINS=test.sealos.io
export ISTIO_ENABLE_TRACING=true

# 生产环境
export ISTIO_ENABLED=true
export ISTIO_PUBLIC_DOMAINS=cloud.sealos.io,*.cloud.sealos.io
export ISTIO_SHARED_GATEWAY=istio-system/sealos-gateway
```

## 验证配置

### 1. 检查配置是否生效

```bash
# 检查 AppLaunchpad 配置
kubectl exec -it deployment/applaunchpad -n sealos -- cat /app/data/config.yaml | grep -A5 "istio:"

# 检查环境变量
kubectl exec -it deployment/devbox -n sealos -- env | grep ISTIO
```

### 2. 验证资源创建

```bash
# 创建测试应用
kubectl apply -f test-app.yaml

# 检查创建的资源类型
kubectl get virtualservice,gateway -n ns-user-xxx

# 确认没有创建 Ingress
kubectl get ingress -n ns-user-xxx
```

### 3. 测试访问

```bash
# 测试 HTTP 访问
curl -H "Host: myapp.cloud.sealos.io" http://gateway-ip/

# 测试 HTTPS 访问
curl -k https://myapp.cloud.sealos.io/
```

## 故障排查

### 问题：仍然创建 Ingress 而非 VirtualService

1. **检查配置文件**
   ```bash
   # AppLaunchpad
   kubectl logs deployment/applaunchpad -n sealos | grep -i "istio"
   
   # 查看实际加载的配置
   kubectl exec deployment/applaunchpad -n sealos -- cat /app/data/config.yaml
   ```

2. **验证运行时配置**
   ```bash
   # 通过 API 检查配置
   curl http://applaunchpad-service/api/platform/getInitData
   ```

3. **重启应用加载新配置**
   ```bash
   kubectl rollout restart deployment/applaunchpad -n sealos
   ```

### 问题：Gateway 未找到

1. **检查共享 Gateway 是否存在**
   ```bash
   kubectl get gateway -n istio-system
   ```

2. **验证 Gateway 名称配置**
   ```bash
   # 确保配置的 Gateway 名称正确
   # 格式：namespace/gateway-name 或仅 gateway-name（同命名空间）
   ```

### 问题：证书错误

1. **检查证书 Secret**
   ```bash
   kubectl get secret wildcard-cert -n istio-system
   ```

2. **验证 Gateway TLS 配置**
   ```bash
   kubectl get gateway sealos-gateway -n istio-system -o yaml
   ```

## 最佳实践

1. **使用运行时配置**
   - 优先使用配置文件而非环境变量
   - 便于在不同环境间切换配置

2. **域名管理**
   - 将所有公共域名添加到 `publicDomains` 列表
   - 自定义域名会自动创建独立 Gateway

3. **监控和调试**
   - 启用 `enableTracing` 进行分布式追踪
   - 使用 Istio 的可观测性工具

4. **逐步迁移**
   - 先在测试环境启用 Istio
   - 验证功能正常后再推广到生产环境

## 相关文档

- [AppLaunchpad Istio 配置指南](../../frontend/providers/applaunchpad/ISTIO_CONFIGURATION.md)
- [Gateway 优化计划](./gateway-optimization-plan.md)
- [Istio 迁移脚本](../../scripts/istio-migration/README.md)
- [运行时配置示例](../../frontend/providers/applaunchpad/data/config.yaml.istio-example)