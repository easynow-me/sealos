# 🔧 修复 Ingress 和 VirtualService 数量不匹配问题

## 问题描述

在从 Ingress 迁移到 Istio 的过程中，可能会出现 VirtualService 和 Ingress 数量不匹配的情况。这通常是因为：

1. **部分 Ingress 未被迁移**：迁移脚本可能遗漏了某些 Ingress
2. **迁移失败但未重试**：某些 Ingress 迁移失败但没有被重新处理
3. **手动创建的资源**：存在手动创建的 Ingress 或 VirtualService
4. **资源清理不完整**：已迁移的 Ingress 没有被正确清理

## 诊断步骤

### 1. 运行诊断脚本

```bash
# 诊断当前迁移状态
./scripts/istio-migration/diagnose-migration.sh
```

这个脚本会：
- 统计 Ingress 和 VirtualService 的总数
- 检查每个用户命名空间的迁移状态
- 找出未迁移的 Ingress
- 检查孤立的 VirtualService
- 生成迁移脚本

### 2. 查看诊断结果

诊断脚本会输出类似以下信息：

```
[INFO] Total Ingresses: 245
[INFO] Total VirtualServices: 198
[INFO] Total Gateways: 46

Namespace: ns-user123
  Ingresses: 5
  VirtualServices: 3
  Gateways: 1
  ✓ app1 -> app1-vs (migrated)
  ✓ app2 -> app2-vs (migrated)
  ✗ app3 (not migrated)
  ✗ app4 (not migrated)
  ⚠ app5 marked as migrated but VirtualService missing!
```

## 修复步骤

### 1. 备份当前状态

```bash
# 备份所有 Ingress 资源
kubectl get ingress --all-namespaces -o yaml > /tmp/all-ingress-backup-$(date +%Y%m%d).yaml

# 备份所有 VirtualService 资源
kubectl get virtualservice --all-namespaces -o yaml > /tmp/all-virtualservice-backup-$(date +%Y%m%d).yaml
```

### 2. 运行增强迁移脚本

```bash
# 查看将要迁移的资源（干运行）
./scripts/istio-migration/migrate-unmigrated-ingresses.sh --dry-run

# 执行实际迁移
./scripts/istio-migration/migrate-unmigrated-ingresses.sh

# 强制迁移（跳过确认）
./scripts/istio-migration/migrate-unmigrated-ingresses.sh --force
```

### 3. 验证迁移结果

```bash
# 再次运行诊断脚本
./scripts/istio-migration/diagnose-migration.sh

# 检查特定命名空间
kubectl get ingress,virtualservice,gateway -n ns-user123
```

## 手动修复

如果自动迁移失败，可以手动修复：

### 1. 手动迁移单个 Ingress

```bash
# 使用转换工具
./tools/istio-migration/converter/sealos-ingress-converter \
    -namespace ns-user123 \
    -ingress app-name \
    -output /tmp/istio-resources

# 查看生成的资源
ls -la /tmp/istio-resources/ns-user123/

# 应用资源
kubectl apply -f /tmp/istio-resources/ns-user123/

# 标记为已迁移
kubectl annotate ingress app-name -n ns-user123 \
    "sealos.io/migrated-to-istio=true" \
    "sealos.io/migration-time=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --overwrite
```

### 2. 清理已迁移的 Ingress

```bash
# 列出所有已迁移的 Ingress
kubectl get ingress --all-namespaces \
    -l "sealos.io/migrated-to-istio=true" \
    -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name

# 删除特定的已迁移 Ingress
kubectl delete ingress app-name -n ns-user123

# 批量删除已迁移的 Ingress（谨慎操作）
kubectl delete ingress --all-namespaces -l "sealos.io/migrated-to-istio=true"
```

### 3. 修复孤立的 VirtualService

```bash
# 找出没有对应服务的 VirtualService
for ns in $(kubectl get ns -o name | grep ns- | cut -d/ -f2); do
  for vs in $(kubectl get virtualservice -n $ns -o name | cut -d/ -f2); do
    service=$(kubectl get virtualservice $vs -n $ns -o jsonpath='{.spec.http[0].route[0].destination.host}' | cut -d. -f1)
    if ! kubectl get service $service -n $ns >/dev/null 2>&1; then
      echo "Orphaned VirtualService: $ns/$vs (missing service: $service)"
    fi
  done
done
```

## 常见问题

### Q1: 为什么有些 Ingress 没有被迁移？

**可能原因：**
- Ingress 使用了不支持的注解或配置
- 命名空间不在迁移范围内（非 `ns-*` 命名空间）
- 迁移时资源正在被修改
- 转换工具版本过旧

**解决方法：**
1. 检查 Ingress 配置是否有特殊注解
2. 确保使用最新版本的转换工具
3. 手动迁移特殊配置的 Ingress

### Q2: VirtualService 创建成功但流量不通

**检查步骤：**
```bash
# 检查 Gateway 是否正确
kubectl get gateway -n <namespace>

# 检查 VirtualService 的 hosts 和 gateways 配置
kubectl get virtualservice <name> -n <namespace> -o yaml

# 检查服务是否存在
kubectl get service <service-name> -n <namespace>

# 检查 Istio sidecar 注入
kubectl get namespace <namespace> -o jsonpath='{.metadata.labels.istio-injection}'
```

### Q3: 如何回滚到 Ingress？

参考[回滚指南](../scripts/rollback.sh)：
```bash
./scripts/istio-migration/rollback.sh --namespace <namespace>
```

## 最佳实践

1. **分批迁移**：不要一次性迁移所有资源，分批处理更容易发现问题
2. **监控验证**：每次迁移后验证流量是否正常
3. **保留备份**：在删除 Ingress 前确保 VirtualService 工作正常
4. **使用标签**：为迁移的资源添加标签，便于管理和回滚

## 监控迁移进度

```bash
# 实时监控迁移进度
watch -n 5 'echo "=== Migration Status ==="; \
  echo "Total Ingresses: $(kubectl get ingress --all-namespaces --no-headers | wc -l)"; \
  echo "Migrated Ingresses: $(kubectl get ingress --all-namespaces -l sealos.io/migrated-to-istio=true --no-headers | wc -l)"; \
  echo "Total VirtualServices: $(kubectl get virtualservice --all-namespaces --no-headers | wc -l)"; \
  echo "Total Gateways: $(kubectl get gateway --all-namespaces --no-headers | wc -l)"'
```

## 联系支持

如果遇到无法解决的问题，请提供以下信息：
1. 诊断脚本的输出
2. 特定资源的 YAML 配置
3. 相关的错误日志

提交问题到：[Sealos GitHub Issues](https://github.com/labring/sealos/issues)