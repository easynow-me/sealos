# LoadBalancer 服务强制限制实施方案

## 概述

本文档详细记录了在 Sealos 各个阶段强制限制 `services.loadbalancers: "0"` 的实施方案，确保所有用户无法创建 LoadBalancer 类型的服务，推动使用 NodePort + Istio Gateway 的现代化网络架构。

## 实施目标

- **统一网络架构**：所有服务使用 NodePort + Istio Gateway 模式
- **强制执行**：在所有阶段和场景下都无法绕过 LoadBalancer 限制
- **向前兼容**：不影响现有应用的功能，只改变网络实现方式
- **云原生**：推动现代微服务网络架构的采用

## 代码更改清单

### 1. 默认配额限制 (`controllers/pkg/resources/resources.go`)

**修改位置**：`DefaultResourceQuotaHard()` 函数

**更改前**：
```go
func DefaultResourceQuotaHard() corev1.ResourceList {
    return corev1.ResourceList{
        // ... 其他资源限制
        // 没有 LoadBalancer 限制
    }
}
```

**更改后**：
```go
func DefaultResourceQuotaHard() corev1.ResourceList {
    return corev1.ResourceList{
        // ... 其他资源限制
        "services.loadbalancers": resource.MustParse("0"), // 强制禁止 LoadBalancer 服务
    }
}
```

**影响范围**：所有新创建的用户默认配额

### 2. 订阅计划保护 (`controllers/pkg/resources/resources.go`)

**修改位置**：`ParseResourceLimitWithSubscription()` 函数

**更改前**：
```go
// 订阅计划可以完全控制资源配额
subPlansLimit[plans[i].Name] = rl
```

**更改后**：
```go
// 强制添加 LoadBalancer 限制，不允许订阅计划覆盖
rl["services.loadbalancers"] = resource.MustParse("0")
subPlansLimit[plans[i].Name] = rl
```

**影响范围**：所有订阅用户的配额，确保订阅计划无法绕过 LoadBalancer 限制

### 3. 动态配额调整保护 (`controllers/resources/controllers/quota_controller.go`)

**修改位置**：`AdjustQuota()` 函数

**新增代码**：
```go
// 强制确保 LoadBalancer 服务限制始终为 0，不允许动态调整
if loadBalancerQuota, exists := quota.Spec.Hard["services.loadbalancers"]; !exists || loadBalancerQuota.String() != "0" {
    quota.Spec.Hard["services.loadbalancers"] = resource.MustParse("0")
    updateRequired = true
}
```

**影响范围**：所有配额自动扩展场景，确保动态调整不会移除 LoadBalancer 限制

### 4. 债务管理保护 (`controllers/account/controllers/namespace_controller.go`)

**修改位置**：`GetLimit0ResourceQuota()` 函数

**更改前**：
```go
quota.Spec.Hard = corev1.ResourceList{
    corev1.ResourceLimitsCPU:        resource.MustParse("0"),
    corev1.ResourceLimitsMemory:     resource.MustParse("0"),
    // ... 其他限制
}
```

**更改后**：
```go
quota.Spec.Hard = corev1.ResourceList{
    corev1.ResourceLimitsCPU:        resource.MustParse("0"),
    corev1.ResourceLimitsMemory:     resource.MustParse("0"),
    // ... 其他限制
    "services.loadbalancers":        resource.MustParse("0"), // 强制禁止 LoadBalancer 服务
}
```

**影响范围**：用户欠费时的限制性配额

### 5. Account 控制器保护 (`controllers/account/controllers/account_controller.go`)

**修改位置**：`getDefaultResourceQuota()` 函数

**新增导入**：
```go
"k8s.io/apimachinery/pkg/api/resource"
```

**更改前**：
```go
func getDefaultResourceQuota(ns, name string, hard corev1.ResourceList) *corev1.ResourceQuota {
    return &corev1.ResourceQuota{
        Spec: corev1.ResourceQuotaSpec{
            Hard: hard,
        },
    }
}
```

**更改后**：
```go
func getDefaultResourceQuota(ns, name string, hard corev1.ResourceList) *corev1.ResourceQuota {
    // 强制确保 LoadBalancer 服务限制为 0，不允许订阅计划覆盖
    if hard == nil {
        hard = make(corev1.ResourceList)
    }
    hard["services.loadbalancers"] = resource.MustParse("0")
    
    return &corev1.ResourceQuota{
        Spec: corev1.ResourceQuotaSpec{
            Hard: hard,
        },
    }
}
```

**影响范围**：订阅模式下的配额创建和更新

## 保护机制层级

### 第一层：默认配额保护
- **位置**：`DefaultResourceQuotaHard()`
- **作用**：确保所有新用户默认包含 LoadBalancer 限制
- **覆盖范围**：标准用户注册流程

### 第二层：订阅计划保护
- **位置**：`ParseResourceLimitWithSubscription()`
- **作用**：确保订阅计划无法绕过 LoadBalancer 限制
- **覆盖范围**：所有付费用户和订阅用户

### 第三层：动态调整保护
- **位置**：`AdjustQuota()`
- **作用**：确保配额自动扩展时不会移除 LoadBalancer 限制
- **覆盖范围**：所有配额超限自动扩展场景

### 第四层：债务管理保护
- **位置**：`GetLimit0ResourceQuota()`
- **作用**：确保用户欠费时的限制性配额也包含 LoadBalancer 限制
- **覆盖范围**：所有欠费用户的资源暂停

### 第五层：控制器同步保护
- **位置**：`getDefaultResourceQuota()`
- **作用**：确保 Account 控制器同步时强制添加 LoadBalancer 限制
- **覆盖范围**：所有控制器驱动的配额更新

## 验证方案

### 1. 单元测试验证
```bash
# 验证默认配额包含 LoadBalancer 限制
go test -v ./controllers/pkg/resources/ -run TestDefaultResourceQuotaHard

# 验证订阅计划保护
go test -v ./controllers/pkg/resources/ -run TestParseResourceLimitWithSubscription

# 验证动态调整保护
go test -v ./controllers/resources/controllers/ -run TestAdjustQuota
```

### 2. 集成测试验证
```bash
# 创建测试用户，验证配额限制
kubectl apply -f - <<EOF
apiVersion: user.sealos.io/v1
kind: User
metadata:
  name: test-loadbalancer-restriction
spec:
  csrExpirationSeconds: 1000000000
EOF

# 检查生成的 ResourceQuota
kubectl get resourcequota -n ns-test-loadbalancer-restriction -o yaml | grep "services.loadbalancers"
```

### 3. 功能测试验证
```bash
# 尝试创建 LoadBalancer 服务（应该失败）
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: test-loadbalancer
  namespace: ns-test-user
spec:
  type: LoadBalancer
  ports:
  - port: 80
    targetPort: 8080
  selector:
    app: test
EOF
```

**预期结果**：创建失败，错误信息包含配额限制

## 影响分析

### 正面影响

1. **架构统一**：所有服务使用相同的网络模式
2. **可观测性提升**：通过 Istio 获得更详细的监控和追踪
3. **安全性增强**：更细粒度的访问控制和流量管理
4. **运维简化**：减少网络配置的复杂性
5. **云原生对齐**：符合现代微服务架构最佳实践

### 可能的挑战

1. **现有 LoadBalancer 服务**：需要迁移到 NodePort + Istio 模式
2. **用户习惯**：需要适应新的网络模式
3. **性能影响**：Istio 代理会增加一定的延迟（通常 <10ms）
4. **资源消耗**：每个 Pod 需要额外的 Istio sidecar

### 迁移建议

1. **MinIO 对象存储**：优先使用自动化迁移脚本
2. **用户教育**：提供详细的迁移指南和最佳实践
3. **监控预警**：建立迁移过程的监控和告警机制
4. **回滚计划**：准备应急回滚方案以应对问题

## 部署步骤

### 1. 预备阶段
```bash
# 1. 备份现有配额配置
kubectl get resourcequota --all-namespaces -o yaml > resourcequota-backup.yaml

# 2. 识别现有 LoadBalancer 服务
kubectl get services --all-namespaces --field-selector spec.type=LoadBalancer

# 3. 准备迁移脚本
./scripts/istio-migration/migrate-minio-to-nodeport.sh --dry-run
```

### 2. 代码部署
```bash
# 1. 构建更新的控制器镜像
cd controllers/
make docker-build-all

# 2. 部署更新的控制器
kubectl apply -f controllers/deploy/manifests/

# 3. 验证控制器更新
kubectl get pods -n sealos-system | grep controller
```

### 3. 验证阶段
```bash
# 1. 创建测试用户验证新配额
./scripts/create-test-user.sh

# 2. 验证现有用户配额更新
kubectl get resourcequota --all-namespaces | grep "services.loadbalancers"

# 3. 执行功能测试
./scripts/test-loadbalancer-restriction.sh
```

### 4. 清理阶段
```bash
# 1. 迁移现有 LoadBalancer 服务
./scripts/istio-migration/migrate-minio-to-nodeport.sh

# 2. 验证服务功能正常
./scripts/verify-services.sh

# 3. 清理测试资源
kubectl delete namespace ns-test-loadbalancer-restriction
```

## 监控和告警

### 关键指标

1. **ResourceQuota 合规性**：
   ```promql
   kube_resourcequota{resource="services.loadbalancers", namespace!~"kube-.*|istio-.*|sealos-.*"} > 0
   ```

2. **LoadBalancer 服务检测**：
   ```promql
   kube_service_info{type="LoadBalancer", namespace!~"kube-.*|istio-.*|sealos-.*"}
   ```

3. **配额创建失败**：
   ```promql
   increase(controller_runtime_reconcile_errors_total{controller="account"}[5m]) > 0
   ```

### 告警规则

```yaml
groups:
- name: loadbalancer-restriction
  rules:
  - alert: LoadBalancerQuotaViolation
    expr: kube_resourcequota{resource="services.loadbalancers", namespace!~"kube-.*|istio-.*|sealos-.*"} > 0
    for: 1m
    labels:
      severity: critical
    annotations:
      summary: "LoadBalancer quota violation detected"
      description: "Namespace {{ $labels.namespace }} has LoadBalancer quota > 0"

  - alert: UnauthorizedLoadBalancerService
    expr: kube_service_info{type="LoadBalancer", namespace!~"kube-.*|istio-.*|sealos-.*"}
    for: 1m
    labels:
      severity: warning
    annotations:
      summary: "Unauthorized LoadBalancer service detected"
      description: "LoadBalancer service {{ $labels.service }} in namespace {{ $labels.namespace }}"
```

## 总结

通过在 Sealos 的 5 个关键层面实施强制 LoadBalancer 限制，我们确保了：

1. **全面覆盖**：从用户创建到配额管理的所有环节都包含限制
2. **无法绕过**：多层保护机制确保限制无法被绕过
3. **向前兼容**：现有功能不受影响，只改变网络实现方式
4. **架构现代化**：推动向 Istio + NodePort 的云原生架构迁移

这一实施为 Sealos 迁移到 Istio 网络架构奠定了坚实的基础，确保了网络模式的统一性和现代化。