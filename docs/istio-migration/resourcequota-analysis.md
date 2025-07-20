# Sealos ResourceQuota 创建机制与配置来源分析

## 概述

本文档详细分析 Sealos 系统中 ResourceQuota 的创建时机、配置来源、管理逻辑和动态调整机制。

## 1. ResourceQuota 创建时机

### 1.1 用户/租户初始化流程

```mermaid
graph TB
    A[用户注册/创建] --> B[User CR 创建]
    B --> C[UserReconciler 处理]
    C --> D[创建用户命名空间<br/>ns-{username}]
    D --> E[AccountReconciler 触发]
    E --> F[创建 ResourceQuota]
    F --> G[应用默认配额限制]
    G --> H[启动配额监控]
```

### 1.2 具体创建触发点

**主要触发器：**

1. **User CR 创建时**
   ```go
   // controllers/user/controllers/user_controller.go
   func (r *UserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
       // 创建用户命名空间：ns-{username}
       namespace := &corev1.Namespace{
           ObjectMeta: metav1.ObjectMeta{
               Name: fmt.Sprintf("ns-%s", user.Name),
           },
       }
   }
   ```

2. **Account 初始化时**
   ```go
   // controllers/account/controllers/account_controller.go
   func (r *AccountReconciler) syncResourceQuotaAndLimitRange(account *accountv1.Account) error {
       // 为每个用户命名空间创建 ResourceQuota
       quota := resources.GetDefaultResourceQuota(account.Status.NamespaceList[i], quotaName)
       return r.Create(ctx, quota)
   }
   ```

3. **订阅计划变更时**
   ```go
   func (r *AccountReconciler) syncResourceQuotaAndLimitRangeBySubscription() error {
       // 根据订阅计划更新配额
       // 当用户升级/降级订阅时触发
   }
   ```

## 2. 配额限制内容来源

### 2.1 默认配额配置

**硬编码默认值** (`controllers/pkg/resources/resources.go`)：

```go
const (
    DefaultQuotaLimitsCPU           = "16"      // 16 CPU 核心
    DefaultQuotaLimitsMemory        = "64Gi"    // 64 GiB 内存
    DefaultQuotaLimitsStorage       = "100Gi"   // 100 GiB 存储
    DefaultQuotaLimitsGPU           = "8"       // 8 个 GPU
    DefaultQuotaLimitsNodePorts     = "10"      // 10 个 NodePort
    DefaultQuotaObjectStorageSize   = "100Gi"   // 100 GiB 对象存储
    DefaultQuotaObjectStorageBucket = "5"       // 5 个存储桶
)
```

**完整默认配额资源列表：**

```go
func DefaultResourceQuotaHard() corev1.ResourceList {
    return corev1.ResourceList{
        // CPU 限制
        corev1.ResourceLimitsCPU: resource.MustParse(GetEnvWithDefault("QUOTA_LIMITS_CPU", DefaultQuotaLimitsCPU)),
        corev1.ResourceRequestsCPU: resource.MustParse(GetEnvWithDefault("QUOTA_LIMITS_CPU", DefaultQuotaLimitsCPU)),
        
        // 内存限制
        corev1.ResourceLimitsMemory: resource.MustParse(GetEnvWithDefault("QUOTA_LIMITS_MEMORY", DefaultQuotaLimitsMemory)),
        corev1.ResourceRequestsMemory: resource.MustParse(GetEnvWithDefault("QUOTA_LIMITS_MEMORY", DefaultQuotaLimitsMemory)),
        
        // 存储限制
        corev1.ResourceRequestsStorage: resource.MustParse(GetEnvWithDefault("QUOTA_LIMITS_STORAGE", DefaultQuotaLimitsStorage)),
        corev1.ResourceEphemeralStorage: resource.MustParse(GetEnvWithDefault("QUOTA_LIMITS_STORAGE", DefaultQuotaLimitsStorage)),
        
        // GPU 限制
        "nvidia.com/gpu": resource.MustParse(GetEnvWithDefault("QUOTA_LIMITS_GPU", DefaultQuotaLimitsGPU)),
        
        // 服务限制
        "services.nodeports": resource.MustParse(GetEnvWithDefault("QUOTA_LIMITS_NODE_PORTS", DefaultQuotaLimitsNodePorts)),
        "services.loadbalancers": resource.MustParse("0"),  // 🚨 禁止 LoadBalancer
        
        // 对象数量限制
        "count/persistentvolumeclaims": resource.MustParse("100"),
        "count/configmaps": resource.MustParse("100"),
        "count/secrets": resource.MustParse("100"),
        "count/services": resource.MustParse("100"),
        "count/pods": resource.MustParse("100"),
    }
}
```

### 2.2 环境变量配置覆盖

**可配置的环境变量：**

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `QUOTA_LIMITS_CPU` | "16" | CPU 核心数限制 |
| `QUOTA_LIMITS_MEMORY` | "64Gi" | 内存限制 |
| `QUOTA_LIMITS_STORAGE` | "100Gi" | 存储限制 |
| `QUOTA_LIMITS_GPU` | "8" | GPU 数量限制 |
| `QUOTA_LIMITS_NODE_PORTS` | "10" | NodePort 服务数量 |
| `QUOTA_OBJECT_STORAGE_SIZE` | "100Gi" | 对象存储大小 |
| `QUOTA_OBJECT_STORAGE_BUCKET` | "5" | 存储桶数量 |

**配置优先级：**
1. 环境变量设置（最高优先级）
2. 订阅计划配置
3. 硬编码默认值（最低优先级）

### 2.3 订阅计划配置

**订阅驱动的配额** (`SUBSCRIPTION_ENABLED=true` 时)：

```go
// 订阅计划中的资源配置示例
type SubscriptionPlan struct {
    MaxResources map[string]string `json:"max_resources"`
    // 例如：
    // {
    //   "cpu": "32",
    //   "memory": "128Gi", 
    //   "storage": "500Gi",
    //   "gpu": "16"
    // }
}
```

## 3. 负责配额管理的控制器

### 3.1 AccountReconciler（主要）

**文件位置**: `controllers/account/controllers/account_controller.go`

**职责：**
- 用户账户初始化时创建初始 ResourceQuota
- 根据订阅计划同步配额
- 处理账户余额变化对配额的影响

**关键方法：**
```go
func (r *AccountReconciler) syncResourceQuotaAndLimitRange(account *accountv1.Account) error
func (r *AccountReconciler) syncResourceQuotaAndLimitRangeBySubscription() error
```

### 3.2 NamespaceQuotaReconciler（动态调整）

**文件位置**: `controllers/resources/controllers/quota_controller.go`

**职责：**
- **动态配额扩展**：当资源使用超过配额时自动增加
- **智能调整算法**：根据使用模式调整配额大小
- **防抖动机制**：避免频繁的配额调整

**自动扩展逻辑：**
```go
func (r *NamespaceQuotaReconciler) expandQuota(quota *corev1.ResourceQuota, resourceName corev1.ResourceName, requestQuantity resource.Quantity) {
    // 扩展策略：
    // 1. 常规使用：增加 50%
    // 2. 大量使用：增加 100%-200%
    // 3. 设置上限：CPU(200核), Memory(1024Gi), Storage(800Gi)
}
```

**扩展限制和冷却时间：**
```go
const (
    DefaultLimitQuotaExpansionCycle = 24 * time.Hour  // 24小时冷却期
    MaxCPUQuota                     = "200"           // CPU 上限
    MaxMemoryQuota                  = "1024Gi"        // 内存上限  
    MaxStorageQuota                 = "800Gi"         // 存储上限
)
```

### 3.3 NamespaceReconciler（债务管理）

**文件位置**: `controllers/account/controllers/namespace_controller.go`

**职责：**
- 用户欠费时创建限制性配额
- 暂停用户资源使用
- 恢复用户服务

**债务限制配额：**
```go
func GetLimit0ResourceQuota(namespace string) *corev1.ResourceQuota {
    return &corev1.ResourceQuota{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "debt-limit0",
            Namespace: namespace,
        },
        Spec: corev1.ResourceQuotaSpec{
            Hard: corev1.ResourceList{
                corev1.ResourceLimitsCPU:        resource.MustParse("0"),      // 暂停 CPU
                corev1.ResourceLimitsMemory:     resource.MustParse("0"),      // 暂停内存
                corev1.ResourceRequestsStorage:  resource.MustParse("0"),      // 暂停存储
                corev1.ResourceEphemeralStorage: resource.MustParse("0"),      // 暂停临时存储
            },
        },
    }
}
```

## 4. 配额策略应用流程

### 4.1 正常用户配额创建

```yaml
# 标准 ResourceQuota 示例
apiVersion: v1
kind: ResourceQuota
metadata:
  name: quota-ns-testuser
  namespace: ns-testuser
spec:
  hard:
    limits.cpu: "16"
    limits.memory: "64Gi"
    requests.cpu: "16"
    requests.memory: "64Gi"
    requests.storage: "100Gi"
    ephemeral-storage: "100Gi"
    nvidia.com/gpu: "8"
    services.nodeports: "10"
    services.loadbalancers: "0"          # 🚨 关键：禁止 LoadBalancer
    count/persistentvolumeclaims: "100"
    count/configmaps: "100"
    count/secrets: "100"
    count/services: "100"
    count/pods: "100"
```

### 4.2 订阅用户配额创建

```go
// 根据订阅计划调整配额
func (r *AccountReconciler) getQuotaFromSubscription(plan SubscriptionPlan) corev1.ResourceList {
    quotaHard := DefaultResourceQuotaHard()
    
    // 覆盖默认值
    if cpu, ok := plan.MaxResources["cpu"]; ok {
        quotaHard[corev1.ResourceLimitsCPU] = resource.MustParse(cpu)
    }
    if memory, ok := plan.MaxResources["memory"]; ok {
        quotaHard[corev1.ResourceLimitsMemory] = resource.MustParse(memory)
    }
    
    return quotaHard
}
```

## 5. 动态配额调整机制

### 5.1 自动扩展触发条件

**事件监听：**
```go
// 监听配额超限事件
func (r *NamespaceQuotaReconciler) handleQuotaExceededEvent(event *corev1.Event) {
    if event.Reason == "ExceededQuota" {
        // 触发配额扩展逻辑
        r.expandQuotaIfNeeded(event)
    }
}
```

**扩展算法：**
```go
func calculateNewQuota(current, requested resource.Quantity) resource.Quantity {
    // 扩展策略：
    // 1. 如果请求量是当前的 2 倍以上，扩展到请求量的 1.5 倍
    // 2. 否则，扩展当前配额的 1.5 倍
    
    if requested.Cmp(current) > 0 {
        ratio := float64(requested.Value()) / float64(current.Value())
        if ratio >= 2.0 {
            return resource.NewQuantity(int64(float64(requested.Value())*1.5), resource.BinarySI)
        }
    }
    
    return resource.NewQuantity(int64(float64(current.Value())*1.5), resource.BinarySI)
}
```

### 5.2 扩展限制和保护机制

**速率限制：**
```go
const (
    QuotaExpansionCooldown = 24 * time.Hour  // 24小时冷却期
    MaxExpansionAttempts   = 3               // 最大扩展次数
)

func (r *NamespaceQuotaReconciler) canExpandQuota(quota *corev1.ResourceQuota) bool {
    lastExpansion := getLastExpansionTime(quota)
    return time.Since(lastExpansion) > QuotaExpansionCooldown
}
```

**上限保护：**
```go
var QuotaUpperLimits = map[corev1.ResourceName]resource.Quantity{
    corev1.ResourceLimitsCPU:     resource.MustParse("200"),     // 200 CPU 核心
    corev1.ResourceLimitsMemory:  resource.MustParse("1024Gi"),  // 1024 GiB 内存
    corev1.ResourceRequestsStorage: resource.MustParse("800Gi"), // 800 GiB 存储
}
```

## 6. LoadBalancer 限制的具体实现

### ✅ LoadBalancer 限制现已强制实施！

经过代码修改，现在在 Sealos 的各个阶段都强制限制 LoadBalancer 服务：

**更新后的 ResourceQuota 配置** (`controllers/pkg/resources/resources.go`):

```go
func DefaultResourceQuotaHard() corev1.ResourceList {
    return corev1.ResourceList{
        ResourceRequestGpu:                    resource.MustParse(env.GetEnvWithDefault(QuotaLimitsGPU, DefaultQuotaLimitsGPU)),
        ResourceLimitGpu:                      resource.MustParse(env.GetEnvWithDefault(QuotaLimitsGPU, DefaultQuotaLimitsGPU)),
        corev1.ResourceLimitsCPU:              resource.MustParse(env.GetEnvWithDefault(QuotaLimitsCPU, DefaultQuotaLimitsCPU)),
        corev1.ResourceLimitsMemory:           resource.MustParse(env.GetEnvWithDefault(QuotaLimitsMemory, DefaultQuotaLimitsMemory)),
        corev1.ResourceRequestsStorage:        resource.MustParse(env.GetEnvWithDefault(QuotaLimitsStorage, DefaultQuotaLimitsStorage)),
        corev1.ResourceLimitsEphemeralStorage: resource.MustParse(env.GetEnvWithDefault(QuotaLimitsStorage, DefaultQuotaLimitsStorage)),
        corev1.ResourceServicesNodePorts:      resource.MustParse(env.GetEnvWithDefault(QuotaLimitsNodePorts, DefaultQuotaLimitsNodePorts)),
        "services.loadbalancers":              resource.MustParse("0"), // 🚨 强制禁止 LoadBalancer 服务
        ResourceObjectStorageSize:             resource.MustParse(env.GetEnvWithDefault(QuotaObjectStorageSize, DefaultQuotaObjectStorageSize)),
        ResourceObjectStorageBucket:           resource.MustParse(env.GetEnvWithDefault(QuotaObjectStorageBucket, DefaultQuotaObjectStorageBucket)),
    }
}
```

### 6.2 多层保护机制

**1. 默认配额限制**：
在 `DefaultResourceQuotaHard()` 函数中硬编码 `services.loadbalancers: "0"`

**2. 订阅计划保护**：
```go
// 在 ParseResourceLimitWithSubscription 中
// 强制添加 LoadBalancer 限制，不允许订阅计划覆盖
rl["services.loadbalancers"] = resource.MustParse("0")
```

**3. 动态配额调整保护**：
```go
// 在 AdjustQuota 函数中
// 强制确保 LoadBalancer 服务限制始终为 0，不允许动态调整
if loadBalancerQuota, exists := quota.Spec.Hard["services.loadbalancers"]; !exists || loadBalancerQuota.String() != "0" {
    quota.Spec.Hard["services.loadbalancers"] = resource.MustParse("0")
    updateRequired = true
}
```

**4. 债务管理保护**：
```go
// 在 GetLimit0ResourceQuota 函数中
quota.Spec.Hard = corev1.ResourceList{
    // ... 其他限制
    "services.loadbalancers": resource.MustParse("0"), // 强制禁止 LoadBalancer 服务
}
```

**5. Account 控制器保护**：
```go
// 在 getDefaultResourceQuota 函数中
// 强制确保 LoadBalancer 服务限制为 0，不允许订阅计划覆盖
hard["services.loadbalancers"] = resource.MustParse("0")
```

### 6.3 系统级例外

**对于系统命名空间**（如 `objectstorage-system`），ResourceQuota 通常不会应用，因为：
```go
// 系统命名空间通常被排除在用户配额管理之外
func isSystemNamespace(namespace string) bool {
    systemNamespaces := []string{
        "kube-system",
        "istio-system", 
        "objectstorage-system",
        "sealos-system",
        "monitoring",
    }
    // 检查是否为系统命名空间
}
```

## 7. 总结

### 7.1 ResourceQuota 创建时机

1. **用户注册时**：UserReconciler 创建命名空间触发
2. **账户初始化时**：AccountReconciler 创建初始配额
3. **订阅变更时**：根据新订阅计划调整配额
4. **债务管理时**：创建限制性配额暂停服务

### 7.2 配置来源优先级

1. **环境变量**（最高优先级）
2. **订阅计划配置**
3. **硬编码默认值**（最低优先级）

### 7.3 LoadBalancer 限制影响 - 已全面实施

**✅ LoadBalancer 限制现已成为 Sealos 的核心架构约束**

- **所有阶段强制执行**：在用户创建、订阅管理、动态调整、债务管理等所有阶段都强制限制
- **多层保护机制**：通过 5 个不同层面确保 LoadBalancer 限制不会被绕过
- **架构统一**：推动所有服务使用 NodePort + Istio Gateway 的现代化架构

**实施效果：**
1. **新用户注册**：自动创建包含 LoadBalancer 限制的 ResourceQuota
2. **订阅用户**：即使订阅计划没有明确限制，也会强制添加 LoadBalancer 限制
3. **动态扩展**：配额自动扩展时会保持 LoadBalancer 限制不变
4. **债务管理**：用户欠费时的限制性配额也包含 LoadBalancer 限制
5. **现有用户**：通过 Account 控制器同步时会自动添加 LoadBalancer 限制

**架构优势：**
- **统一性**：所有服务使用相同的网络模式
- **可观测性**：通过 Istio 获得更好的监控和追踪
- **安全性**：更细粒度的访问控制和流量管理
- **云原生**：符合现代微服务架构最佳实践