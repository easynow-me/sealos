# Istio VirtualService Webhook Support

本文档说明 Sealos admission webhook 对 Istio VirtualService 资源的支持。

## 概述

为了支持 Istio 作为 Kubernetes Ingress 的替代方案，我们为 VirtualService 资源实现了与 Ingress 相同的验证和变更逻辑。

## 功能特性

### VirtualService 验证器 (VirtualServiceValidator)

提供与 Ingress 验证器相同的安全检查：

1. **CNAME 验证**: 验证 VirtualService 中的 hosts 是否通过 DNS CNAME 指向允许的域名
2. **所有权检查**: 确保每个 hostname 只能被一个命名空间使用，防止跨租户的域名冲突
3. **ICP 备案验证**: 对于中国大陆的合规要求，验证域名的 ICP 备案信息

### VirtualService 变更器 (VirtualServiceMutator)

自动为用户命名空间中的 VirtualService 添加必要的注解。

## 配置方式

### 1. 命令行参数

```bash
# 启用 Istio VirtualService webhooks
--enable-istio-webhooks=true

# 配置 VirtualService 变更注解
--virtualservice-mutating-annotations="annotation1=value1,annotation2=value2"

# 配置允许的域名（与 Ingress 共享）
--domains="example.com,test.com"
```

### 2. 环境变量

```bash
# 启用 Istio webhooks
export ENABLE_ISTIO_WEBHOOKS=true

# 配置 VirtualService 注解
export VIRTUALSERVICE_MUTATING_ANNOTATIONS="annotation1=value1,annotation2=value2"

# ICP 配置（与 Ingress 共享）
export ICP_ENABLED=true
export ICP_ENDPOINT="https://icp-api.example.com"
export ICP_KEY="your-icp-api-key"
```

## 使用场景

### 1. Ingress 到 VirtualService 迁移

在从 Kubernetes Ingress 迁移到 Istio VirtualService 时，可以同时启用两种 webhook：

```yaml
# deployment.yaml
env:
- name: ENABLE_ISTIO_WEBHOOKS
  value: "true"
- name: VIRTUALSERVICE_MUTATING_ANNOTATIONS
  value: "istio.io/security-policy=strict,networking.istio.io/exportTo=."
```

### 2. 纯 Istio 环境

在纯 Istio 环境中，可以只启用 VirtualService webhook：

```bash
./admission-webhook \
  --enable-istio-webhooks=true \
  --domains="cloud.sealos.io" \
  --virtualservice-mutating-annotations="istio.io/protocol=HTTP"
```

## 验证逻辑

### VirtualService Host 提取

webhook 会自动提取 VirtualService 中的有效 hosts：

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: my-app
  namespace: ns-user123
spec:
  hosts:
  - "app.user123.cloud.sealos.io"  # 有效 host，会被验证
  - "*.internal.local"             # 内部 host，跳过验证
  - "my-service.default.svc.cluster.local"  # 集群内部，跳过验证
```

### 多租户隔离

- 只有以 `ns-` 开头的用户命名空间会被验证
- 同一个 hostname 只能在一个命名空间中使用
- 系统命名空间的 VirtualService 不受限制

### CNAME 检查

```bash
# 例如：验证 app.user123.cloud.sealos.io
dig CNAME app.user123.cloud.sealos.io
# 必须返回以配置域名结尾的 CNAME，如：
# app.user123.cloud.sealos.io. 300 IN CNAME ingress.cloud.sealos.io.
```

## 与现有 Ingress Webhook 的关系

- VirtualService 和 Ingress webhook 可以同时运行
- 它们共享相同的域名配置和 ICP 设置
- 验证逻辑保持一致，确保安全策略的统一性

## Webhook 配置文件

### ValidatingAdmissionWebhook

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionWebhook
metadata:
  name: virtualservice-validator
webhooks:
- name: vvirtualservice.sealos.io
  clientConfig:
    service:
      name: admission-webhook-service
      namespace: sealos-system
      path: /validate-networking-istio-io-v1beta1-virtualservice
  rules:
  - operations: ["CREATE", "UPDATE"]
    apiGroups: ["networking.istio.io"]
    apiVersions: ["v1beta1"]
    resources: ["virtualservices"]
```

### MutatingAdmissionWebhook

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingAdmissionWebhook
metadata:
  name: virtualservice-mutator
webhooks:
- name: mvirtualservice.sealos.io
  clientConfig:
    service:
      name: admission-webhook-service
      namespace: sealos-system
      path: /mutate-networking-istio-io-v1beta1-virtualservice
  rules:
  - operations: ["CREATE", "UPDATE"]
    apiGroups: ["networking.istio.io"]
    apiVersions: ["v1beta1"]
    resources: ["virtualservices"]
```

## 故障排除

### 检查 Webhook 状态

```bash
# 检查 webhook 是否注册
kubectl get validatingadmissionwebhooks | grep virtualservice
kubectl get mutatingadmissionwebhooks | grep virtualservice

# 查看 webhook 日志
kubectl logs -n sealos-system deployment/admission-webhook -f
```

### 常见错误

1. **40300: CNAME 检查失败**
   - 确保域名的 CNAME 记录指向允许的域名
   - 检查 DNS 解析是否正常

2. **40301: 所有权检查失败**
   - 同一个 hostname 已被其他命名空间使用
   - 检查集群中是否有重复的 VirtualService hosts

3. **40302: ICP 备案检查失败**
   - 域名未进行 ICP 备案（仅适用于中国大陆）
   - ICP API 服务不可用

## 性能考量

- VirtualService host 索引：webhook 使用缓存索引加速 hostname 查找
- DNS 查询：CNAME 检查会进行 DNS 查询，可能影响性能
- ICP 查询：使用缓存减少 API 调用次数

## 最佳实践

1. **渐进式迁移**: 先启用 VirtualService webhook，再逐步迁移应用
2. **监控告警**: 设置 webhook 错误率监控
3. **测试验证**: 在测试环境验证 webhook 配置
4. **备份策略**: 保留 Ingress webhook 作为回滚选项