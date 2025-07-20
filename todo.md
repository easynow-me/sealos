# Sealos Ingress 到 Istio Gateway/VirtualService 迁移计划

## 迁移可行性分析

### 技术可行性：✅ 可行
- Istio Gateway/VirtualService 支持所有当前 Ingress 的功能
- 支持 HTTP、GRPC、WebSocket 等协议
- 提供更强大的流量管理和安全特性
- 支持多租户和命名空间隔离

### 主要挑战
1. 代码重构工作量大（5个控制器 + 前端应用）
2. Istio 运维复杂度增加
3. 团队需要 Istio 技术储备
4. 性能开销（约 10-15%）

## 执行计划

### 第一阶段：准备工作（2周）

#### 1.1 环境准备
- [ ] 部署 Istio 到测试集群
- [ ] 配置 Istio 多租户支持
- [ ] 验证 Istio 与现有组件兼容性
- [ ] 性能基准测试

#### 1.2 技术方案设计
- [ ] 设计 Gateway/VirtualService 资源命名规范
- [ ] 设计多租户隔离方案
- [ ] 设计 SSL 证书管理方案
- [ ] 设计域名管理策略

#### 1.3 工具开发 ✅ 已完成
- [x] 开发 Ingress 到 Gateway/VirtualService 转换工具
- [x] 开发迁移验证脚本
- [x] 准备回滚方案

**完成的工具：**
- `tools/istio-migration/converter/` - Go 语言编写的转换工具，支持自动转换 Ingress 到 Istio 资源
- `tools/istio-migration/scripts/validate-migration.sh` - 迁移验证脚本，支持完整性和流量测试
- `tools/istio-migration/scripts/rollback.sh` - 快速回滚脚本，支持自动恢复到 Ingress
- 完整的文档和使用指南
- Makefile 构建系统

### 第二阶段：控制器改造（4周）

#### 2.1 Terminal 控制器改造
- [ ] 重构 `/controllers/terminal/controllers/ingress.go`
- [ ] 实现 Gateway/VirtualService 创建逻辑
- [ ] 支持 WebSocket 协议配置
- [ ] 添加单元测试
- [ ] E2E 测试验证

#### 2.2 DB Adminer 控制器改造
- [ ] 重构 `/controllers/db/adminer/controllers/ingress.go`
- [ ] 实现 Gateway/VirtualService 创建逻辑
- [ ] 支持 HTTP/HTTPS 配置
- [ ] 添加单元测试
- [ ] E2E 测试验证

#### 2.3 Resources 控制器改造
- [ ] 修改网络资源暂停/恢复逻辑
- [ ] 支持 Gateway/VirtualService 的启用/禁用
- [ ] 添加单元测试
- [ ] E2E 测试验证

#### 2.4 Webhook 改造
- [ ] 重构 Ingress Mutator 为 VirtualService Mutator
- [ ] 重构 Ingress Validator 为 VirtualService Validator
- [ ] 保持 ICP 域名验证功能
- [ ] 添加单元测试

### 第三阶段：前端应用改造（3周）

#### 3.1 App Launchpad 改造
- [ ] 修改 `json2Ingress()` 为 `json2Gateway()`
- [ ] 更新 YAML 生成逻辑
- [ ] 修改域名配置界面
- [ ] 前端集成测试

#### 3.2 DevBox 改造
- [ ] 更新端口暴露逻辑
- [ ] 修改 YAML 生成逻辑
- [ ] 前端集成测试

#### 3.3 KubePanel 改造
- [ ] 添加 Gateway/VirtualService 视图
- [ ] 更新资源模板
- [ ] 修改创建/编辑界面
- [ ] 前端集成测试

#### 3.4 Desktop/License 服务改造
- [ ] 更新部署模板
- [ ] 修改域名配置逻辑

### 第四阶段：集成测试（2周）

#### 4.1 功能测试
- [ ] 多租户隔离测试
- [ ] 协议支持测试（HTTP/GRPC/WebSocket）
- [ ] SSL 证书测试
- [ ] 域名管理测试
- [ ] CORS 配置测试

#### 4.2 性能测试
- [ ] 延迟对比测试
- [ ] 吞吐量测试
- [ ] 资源消耗对比
- [ ] 大规模场景测试

#### 4.3 兼容性测试
- [ ] 与现有功能兼容性测试
- [ ] 升级路径测试
- [ ] 回滚测试

### 第五阶段：灰度发布（3周）

#### 5.1 灰度策略
- [ ] 10% 流量切换到 Istio
- [ ] 监控关键指标
- [ ] 收集用户反馈
- [ ] 逐步扩大灰度范围

#### 5.2 文档更新
- [ ] 更新用户文档
- [ ] 更新运维文档
- [ ] 更新开发文档
- [ ] 迁移指南

#### 5.3 培训
- [ ] 开发团队 Istio 培训
- [ ] 运维团队培训
- [ ] 准备 FAQ

### 第六阶段：全面切换（1周）

#### 6.1 最终切换
- [ ] 停止创建新的 Ingress 资源
- [ ] 批量迁移存量 Ingress
- [ ] 验证所有功能正常
- [ ] 清理旧资源

#### 6.2 监控观察
- [ ] 7x24 小时监控
- [ ] 性能指标跟踪
- [ ] 问题快速响应

## 关键技术点

### 1. Gateway 配置示例
```yaml
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: app-gateway
  namespace: ns-user
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*.cloud.sealos.io"
  - port:
      number: 443
      name: https
      protocol: HTTPS
    hosts:
    - "*.cloud.sealos.io"
    tls:
      mode: SIMPLE
      credentialName: wildcard-cert
```

### 2. VirtualService 配置示例
```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: app-vs
  namespace: ns-user
spec:
  hosts:
  - myapp.cloud.sealos.io
  gateways:
  - app-gateway
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: myapp-svc
        port:
          number: 80
    timeout: 300s
    corsPolicy:
      allowOrigins:
      - exact: "https://cloud.sealos.io"
      allowMethods:
      - GET
      - POST
      - PUT
      - DELETE
      allowHeaders:
      - content-type
      - authorization
```

### 3. 协议特定配置

#### WebSocket 支持
```yaml
http:
- match:
  - headers:
      upgrade:
        exact: websocket
  route:
  - destination:
      host: terminal-svc
  timeout: 0s  # 无超时
```

#### GRPC 支持
```yaml
http:
- match:
  - headers:
      content-type:
        prefix: application/grpc
  route:
  - destination:
      host: grpc-svc
```

## 风险管理

### 技术风险
1. **性能影响**：准备性能优化方案
2. **复杂度增加**：加强团队培训
3. **兼容性问题**：充分测试，准备回滚

### 业务风险
1. **服务中断**：灰度发布，快速回滚
2. **用户体验**：保持 API 兼容性
3. **运维成本**：自动化运维工具

## 成功标准

1. 所有功能正常迁移，无功能缺失
2. 性能损耗控制在 15% 以内
3. 零服务中断事故
4. 用户无感知迁移

## 总耗时预估

- 总计：13-15 周
- 人力：3-4 名工程师全职投入

## 后续优化

1. 利用 Istio 高级特性：
   - 金丝雀发布
   - 熔断限流
   - 分布式追踪
   - 细粒度授权

2. 性能优化：
   - Envoy 配置调优
   - 连接池优化
   - 缓存策略

3. 运维提升：
   - 可观测性增强
   - 自动化故障恢复
   - 智能路由策略