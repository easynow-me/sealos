# 🎯 修复 Adminer 删除时 VirtualService 资源泄漏问题

## 问题描述

当删除 Adminer 资源时，相关的 VirtualService 和 Gateway 没有被自动删除，导致资源泄漏。这是因为 Istio 资源没有正确设置 OwnerReference。

## 根本原因

1. **AppNetworkingSpec 缺少 OwnerObject**：网络配置规范中没有传递 Owner 信息
2. **VirtualServiceController 和 GatewayController 接口不支持 OwnerReference**：原接口只有 Create/Update 方法，不支持设置 OwnerReference
3. **optimizedNetworkingManager 没有 runtime.Scheme**：无法创建 OwnerReference

## 解决方案

### 1. 添加 OwnerObject 到 AppNetworkingSpec

```go
// types.go
type AppNetworkingSpec struct {
    // ... 其他字段
    
    // 对象引用（用于设置OwnerReference）
    OwnerObject metav1.Object
}
```

### 2. 添加支持 OwnerReference 的接口方法

```go
// VirtualServiceController 接口
type VirtualServiceController interface {
    // ... 其他方法
    
    // 创建或更新 VirtualService（支持设置 OwnerReference）
    CreateOrUpdateWithOwner(ctx context.Context, config *VirtualServiceConfig, owner metav1.Object, scheme *runtime.Scheme) error
}

// GatewayController 接口
type GatewayController interface {
    // ... 其他方法
    
    // 创建或更新 Gateway（支持设置 OwnerReference）
    CreateOrUpdateWithOwner(ctx context.Context, config *GatewayConfig, owner metav1.Object, scheme *runtime.Scheme) error
}
```

### 3. 更新管理器以支持 Scheme

```go
// 新增带 Scheme 的构造函数
func NewOptimizedNetworkingManagerWithScheme(client client.Client, scheme *runtime.Scheme, config *NetworkConfig) NetworkingManager {
    // ...
}

func NewUniversalIstioNetworkingHelperWithScheme(client client.Client, scheme *runtime.Scheme, config *NetworkConfig, appType string) *UniversalIstioNetworkingHelper {
    // ...
}
```

### 4. 更新控制器使用新的构造函数

```go
// adminer/controllers/setup.go
r.istioHelper = istio.NewUniversalIstioNetworkingHelperWithScheme(r.Client, r.Scheme, config, "adminer")

// terminal/controllers/setup.go
r.istioHelper = istio.NewUniversalIstioNetworkingHelperWithScheme(r.Client, r.Scheme, config, "terminal")

// resources/controllers/network_controller.go
r.networkingManager = istio.NewOptimizedNetworkingManagerWithScheme(r.Client, r.Scheme, config)
```

## 修改的文件清单

1. `/controllers/pkg/istio/types.go`
   - 添加 OwnerObject 到 AppNetworkingSpec
   - 添加 CreateOrUpdateWithOwner 到接口定义

2. `/controllers/pkg/istio/universal_helper.go`
   - 传递 OwnerObject 到 AppNetworkingSpec
   - 添加带 Scheme 的构造函数

3. `/controllers/pkg/istio/optimized_manager.go`
   - 添加 scheme 字段
   - 使用 CreateOrUpdateWithOwner 方法

4. `/controllers/pkg/istio/virtualservice.go`
   - 重命名方法为 CreateOrUpdateWithOwner

5. `/controllers/pkg/istio/gateway.go`
   - 实现 CreateOrUpdateWithOwner 方法

6. 控制器更新：
   - `/controllers/db/adminer/controllers/setup.go`
   - `/controllers/terminal/controllers/setup.go`
   - `/controllers/resources/controllers/network_controller.go`

## 验证方法

### 1. 创建 Adminer 资源

```bash
kubectl apply -f - <<EOF
apiVersion: adminer.db.sealos.io/v1
kind: Adminer
metadata:
  name: test-adminer
  namespace: test-ns
spec:
  keepalived: "1h"
  connections:
    - driver: mysql
      host: mysql-service
      port: 3306
EOF
```

### 2. 检查创建的资源

```bash
# 检查 VirtualService
kubectl get virtualservice -n test-ns test-adminer-vs -o yaml | grep ownerReferences -A 5

# 应该看到：
# ownerReferences:
# - apiVersion: adminer.db.sealos.io/v1
#   blockOwnerDeletion: true
#   controller: true
#   kind: Adminer
#   name: test-adminer
```

### 3. 删除 Adminer 资源

```bash
kubectl delete adminer -n test-ns test-adminer
```

### 4. 验证级联删除

```bash
# VirtualService 应该被自动删除
kubectl get virtualservice -n test-ns test-adminer-vs
# 应该返回: Error from server (NotFound)

# Gateway（如果创建了）也应该被删除
kubectl get gateway -n test-ns test-adminer-gateway
# 应该返回: Error from server (NotFound)
```

## 优势

1. **自动资源清理**：删除主资源时，所有相关的 Istio 资源会自动删除
2. **防止资源泄漏**：不会留下孤立的 VirtualService 或 Gateway 资源
3. **符合 Kubernetes 最佳实践**：使用标准的 OwnerReference 机制
4. **向后兼容**：对于没有提供 OwnerObject 的旧代码，仍然可以正常工作

## 注意事项

1. **Scheme 要求**：控制器必须提供 runtime.Scheme 才能设置 OwnerReference
2. **命名空间限制**：OwnerReference 只能在同一命名空间内工作
3. **级联删除策略**：默认使用 background 删除策略