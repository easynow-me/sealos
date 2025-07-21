# Applaunchpad Istio功能测试指南

## 测试前准备

1. **确保Istio已安装**
```bash
kubectl get ns istio-system
kubectl get pods -n istio-system
```

2. **配置Istio模式**
在`data/config.yaml`中启用Istio：
```yaml
istio:
  enabled: true
  publicDomains:
    - 'cloud.sealos.io'
    - '*.cloud.sealos.io'
  sharedGateway: 'sealos-gateway'
```

3. **重启应用**
```bash
kubectl rollout restart deployment/launchpad-frontend -n sealos
```

## 测试场景

### 1. 创建应用测试

**测试步骤**：
1. 创建新应用，开启网络暴露
2. 使用默认域名（公共域名）
3. 检查生成的资源

**验证点**：
```bash
# 检查VirtualService
kubectl get virtualservice -n <namespace> -l app.kubernetes.io/name=<appname>

# 检查是否使用共享网关
kubectl get virtualservice <appname> -n <namespace> -o yaml | grep -A 5 gateways

# 期望看到：
# gateways:
# - istio-system/sealos-gateway
```

### 2. 自定义域名测试

**测试步骤**：
1. 创建应用并设置自定义域名
2. 验证CNAME记录
3. 检查生成的资源

**验证点**：
```bash
# 检查是否创建了专用Gateway
kubectl get gateway -n <namespace> -l app.kubernetes.io/name=<appname>

# 检查Gateway配置
kubectl get gateway <appname>-gateway -n <namespace> -o yaml
```

### 3. 域名冲突检查测试

**测试步骤**：
1. 创建应用A，使用域名 test.example.com
2. 创建应用B，尝试使用相同域名
3. 应该收到域名冲突错误

**验证点**：
- 错误信息应包含："Domain already exists in VirtualService: app-a"

### 4. 应用删除测试

**测试步骤**：
1. 创建包含网络配置的应用
2. 删除应用
3. 检查资源清理情况

**验证点**：
```bash
# 删除前记录资源
kubectl get virtualservice,gateway -n <namespace> -l app.kubernetes.io/name=<appname>

# 删除应用后验证资源已清理
kubectl get virtualservice,gateway -n <namespace> -l app.kubernetes.io/name=<appname>
# 期望：No resources found
```

### 5. 更新应用测试

**测试步骤**：
1. 创建应用with网络配置
2. 修改域名或端口
3. 保存更新

**验证点**：
```bash
# 检查VirtualService是否更新
kubectl get virtualservice <appname> -n <namespace> -o yaml | grep -A 5 hosts
```

### 6. 混合场景测试

**测试步骤**：
1. 创建多个网络端口
2. 部分使用公共域名，部分使用自定义域名
3. 验证智能网关选择逻辑

**验证点**：
- 如果所有域名都是公共域名 → 使用共享网关
- 如果有任何自定义域名 → 创建专用网关

## 性能测试

### 流量测试
```bash
# 通过Istio网关访问应用
curl -H "Host: myapp.cloud.sealos.io" http://<istio-gateway-ip>

# 检查响应时间和状态
```

### 监控验证
1. 检查Prometheus指标
2. 验证日志收集正常
3. 确认追踪功能（如果启用）

## 回滚测试

如果需要从Istio模式回滚到Ingress：

1. **修改配置**
```yaml
istio:
  enabled: false
```

2. **重新创建应用**
- 系统应自动创建Ingress资源而非VirtualService/Gateway

## 故障排查

### 常见问题

1. **域名无法访问**
```bash
# 检查Gateway监听器
kubectl get gateway -A -o yaml | grep -A 10 servers

# 检查VirtualService路由
kubectl get virtualservice <name> -n <namespace> -o yaml
```

2. **资源未创建**
```bash
# 检查应用日志
kubectl logs -n sealos deployment/launchpad-frontend

# 检查权限
kubectl auth can-i create virtualservices.networking.istio.io -n <namespace>
```

3. **删除失败**
```bash
# 手动清理遗留资源
kubectl delete virtualservice,gateway -n <namespace> -l app.kubernetes.io/name=<appname>
```

## 测试检查清单

- [ ] Istio模式下创建应用生成VirtualService/Gateway
- [ ] 公共域名使用共享网关
- [ ] 自定义域名创建专用网关
- [ ] 域名冲突检查正常工作
- [ ] 应用删除时清理所有Istio资源
- [ ] 应用更新正确更新Istio资源
- [ ] 从Ingress迁移到Istio正常工作
- [ ] 监控和日志功能正常