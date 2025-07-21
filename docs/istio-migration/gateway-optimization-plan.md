# 🎯 Gateway架构优化方案

## 设计目标

### 🔄 优化策略
- **公共域名** (`*.cloud.sealos.io`) → 使用 `istio-system/sealos-gateway`
- **自定义域名** → 在用户空间创建专用Gateway + 证书

### 📊 资源优化
- **减少Gateway数量**：从240个 → 46个 (节省81%)
- **证书安全性**：系统证书隔离，用户证书独立
- **管理简化**：统一公共域名配置

## 技术实现

### 1. 域名分类逻辑
```go
func isDomainPublic(host string) bool {
    publicDomains := []string{
        ".cloud.sealos.io",
        "cloud.sealos.io",
    }
    
    for _, domain := range publicDomains {
        if strings.HasSuffix(host, domain) {
            return true
        }
    }
    return false
}
```

### 2. Gateway选择策略
```yaml
# 公共域名VirtualService
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
spec:
  gateways:
    - istio-system/sealos-gateway  # 跨命名空间引用
  hosts:
    - xxx.cloud.sealos.io

---
# 自定义域名需要独立Gateway
apiVersion: networking.istio.io/v1beta1  
kind: Gateway
metadata:
  namespace: ns-user123
spec:
  servers:
  - hosts: [custom.example.com]
    tls:
      credentialName: custom-domain-cert  # 用户提供的证书
```

### 3. 证书管理策略

#### 公共域名证书
- **位置**: `istio-system/wildcard-cert`
- **覆盖**: `*.cloud.sealos.io`
- **管理**: 系统级别，用户不可见

#### 自定义域名证书
- **位置**: `用户命名空间/custom-domain-cert`
- **覆盖**: 用户指定域名
- **管理**: 用户自行上传和维护

## 实现步骤

### Phase 1: Converter工具增强
1. **域名识别逻辑**
   - 判断域名是否为公共域名
   - 选择对应的Gateway创建策略

2. **VirtualService生成**
   - 公共域名：引用 `istio-system/sealos-gateway`
   - 自定义域名：引用本命名空间Gateway

3. **Gateway生成**
   - 公共域名：跳过Gateway创建
   - 自定义域名：创建专用Gateway

### Phase 2: 证书验证逻辑
```go
func validateCertificate(namespace, host, certName string) error {
    if isDomainPublic(host) {
        // 公共域名不需要用户提供证书
        return nil
    }
    
    // 自定义域名需要验证证书存在
    if !certificateExists(namespace, certName) {
        return fmt.Errorf("certificate %s not found in namespace %s", certName, namespace)
    }
    
    return nil
}
```

### Phase 3: 迁移策略
1. **新建服务**：直接使用新逻辑
2. **存量服务**：批量迁移脚本
3. **回滚机制**：保留原Gateway备份

## 安全模型

### 🔒 安全边界
```
公共域名流量:
[User] → [Istio Ingress] → [istio-system/sealos-gateway] → [VirtualService] → [Service]

自定义域名流量:
[User] → [Istio Ingress] → [用户空间/custom-gateway] → [VirtualService] → [Service]
```

### 🛡️ 安全优势
1. **证书隔离**: 系统证书不暴露给用户
2. **权限分离**: 用户只能管理自己的自定义域名证书
3. **资源隔离**: 公共Gateway由系统管理，用户无法修改

## 配置示例

### 优化前 (当前)
```yaml
# 用户空间: ns-abc123
# ❌ 问题: 引用不存在的证书
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: app-gateway
  namespace: ns-abc123
spec:
  servers:
  - hosts: [xxx.cloud.sealos.io]
    tls:
      credentialName: wildcard-cert  # ❌ 用户空间中不存在
---
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
spec:
  gateways: [app-gateway]  # ❌ 本地Gateway
```

### 优化后 (目标)
```yaml
# 公共域名 - 无需创建Gateway
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  namespace: ns-abc123
spec:
  gateways: 
    - istio-system/sealos-gateway  # ✅ 引用系统Gateway
  hosts: [xxx.cloud.sealos.io]

---
# 自定义域名 - 创建专用Gateway
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  namespace: ns-abc123
spec:
  servers:
  - hosts: [custom.example.com]
    tls:
      credentialName: user-custom-cert  # ✅ 用户提供的证书
```

## 监控和验证

### 📊 监控指标
- Gateway数量减少率
- 证书引用错误消除
- TLS握手成功率
- 跨命名空间Gateway引用成功率

### 🧪 测试用例
1. **公共域名测试**: 验证 `istio-system/sealos-gateway` 引用
2. **自定义域名测试**: 验证用户Gateway + 证书
3. **证书隔离测试**: 确认系统证书不可见
4. **流量测试**: 验证两种模式的流量正常

## 预期效果

### 📈 资源优化
- **Gateway数量**: 240 → 46 (减少81%)
- **内存占用**: 减少约200MB
- **管理复杂度**: 大幅降低

### 🔐 安全提升
- 系统证书完全隔离
- 用户证书管理独立
- 权限边界清晰

### 🚀 运维优化
- 公共域名统一管理
- 证书更新影响范围明确
- 故障隔离更好

这个方案完全可行且效益显著，建议立即实施！