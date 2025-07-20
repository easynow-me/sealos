# Phase 6: Production Readiness Checklist

## 概述

本文档提供了从 Ingress 完全切换到 Istio 之前的生产就绪检查清单。确保所有项目都已完成并验证，以保证平稳过渡。

## 前置条件检查

### 1. 基础设施就绪 ✓

- [ ] Istio 控制平面高可用部署（至少 3 个副本）
- [ ] Istio IngressGateway 高可用部署（至少 3 个副本）
- [ ] 足够的集群资源（CPU、内存、存储）
- [ ] 网络策略配置正确
- [ ] LoadBalancer 限制已在所有层面强制执行

### 2. 监控和可观测性 ✓

- [ ] Prometheus 正常运行并收集 Istio 指标
- [ ] Grafana 仪表板已配置并可访问
- [ ] AlertManager 配置了适当的告警规则
- [ ] 分布式追踪（Jaeger/Zipkin）已部署
- [ ] 日志聚合系统正常工作

### 3. 安全配置 ✓

- [ ] mTLS 在网格内启用
- [ ] 证书管理（cert-manager）正常运行
- [ ] RBAC 策略已配置
- [ ] 网络策略已实施
- [ ] 入站流量的安全头部配置

## 功能验证清单

### 1. 核心功能测试 ✓

```bash
# 运行核心功能测试
./tests/istio-migration/scripts/run-test-suite.sh all
```

- [ ] 多租户隔离验证通过
- [ ] HTTP/HTTPS 流量正常
- [ ] WebSocket 连接稳定
- [ ] gRPC 服务正常工作
- [ ] CORS 配置生效

### 2. 性能基准 ✓

- [ ] P95 延迟增加 < 15%
- [ ] 吞吐量下降 < 10%
- [ ] 错误率 < 0.1%
- [ ] 资源使用增加 < 20%

### 3. 兼容性验证 ✓

- [ ] 所有现有应用正常运行
- [ ] API 端点响应正常
- [ ] 数据库连接稳定
- [ ] 外部服务集成正常

## 迁移前检查

### 1. 备份和恢复 ✓

```bash
# 创建完整备份
kubectl get ingress --all-namespaces -o yaml > backup/all-ingress-$(date +%Y%m%d).yaml
kubectl get deployments --all-namespaces -o yaml > backup/all-deployments-$(date +%Y%m%d).yaml
```

- [ ] 所有 Ingress 资源已备份
- [ ] 关键配置已备份
- [ ] 回滚脚本已测试
- [ ] 恢复流程已验证

### 2. 双模式运行验证 ✓

- [ ] Ingress 和 Istio 同时运行正常
- [ ] 流量分配比例可调
- [ ] 无冲突或资源竞争
- [ ] 监控显示两种模式都健康

### 3. 团队准备 ✓

- [ ] 运维团队已完成 Istio 培训
- [ ] 开发团队了解新的部署方式
- [ ] 紧急响应流程已更新
- [ ] 值班人员安排就绪

## 切换执行清单

### 1. 停止 Ingress 创建

```bash
# 执行步骤 1：禁用新 Ingress 创建
./scripts/istio-migration/phase6-full-cutover.sh --step disable-ingress
```

- [ ] Admission webhook 已更新
- [ ] 控制器已切换到 Istio 模式
- [ ] 新部署使用 VirtualService

### 2. 迁移存量资源

```bash
# 执行步骤 2：迁移现有 Ingress
./scripts/istio-migration/phase6-full-cutover.sh --step migrate-existing
```

- [ ] 转换工具运行成功
- [ ] 所有 VirtualService 已创建
- [ ] Gateway 配置正确
- [ ] 流量路由正常

### 3. 功能验证

```bash
# 执行步骤 3：验证功能
./scripts/istio-migration/phase6-full-cutover.sh --step validate
```

- [ ] 自动化测试全部通过
- [ ] 手动抽查关键服务
- [ ] 用户反馈正常
- [ ] 监控指标健康

### 4. 清理旧资源

```bash
# 执行步骤 4：清理
./scripts/istio-migration/phase6-full-cutover.sh --step cleanup
```

- [ ] 旧 Ingress 已标记
- [ ] 确认无流量后删除
- [ ] 配置清理完成
- [ ] 资源释放验证

## 监控设置

### 1. 24/7 监控部署

```bash
# 设置监控
./scripts/istio-migration/phase6-monitoring-setup.sh --component all \
  --webhook-url https://your-webhook-url \
  --alert-email ops@example.com
```

- [ ] ServiceMonitor 已创建
- [ ] PrometheusRule 已配置
- [ ] 告警通知已测试
- [ ] 仪表板可访问

### 2. 关键指标监控

监控以下关键指标：
- 请求速率（RPS）
- 错误率（5xx 响应）
- P50/P95/P99 延迟
- 连接数
- CPU/内存使用率

### 3. 告警阈值

| 指标 | 警告阈值 | 严重阈值 |
|------|----------|----------|
| P95 延迟 | > 1000ms | > 5000ms |
| 错误率 | > 1% | > 5% |
| CPU 使用率 | > 70% | > 90% |
| 内存使用率 | > 70% | > 90% |

## 应急预案

### 1. 快速回滚

如果出现严重问题，执行快速回滚：

```bash
# 紧急回滚到 Ingress
./scripts/istio-migration/emergency-rollback.sh --mode full
```

### 2. 部分回滚

对特定服务回滚：

```bash
# 回滚特定命名空间
./scripts/istio-migration/emergency-rollback.sh --mode partial --namespace ns-user-xxx
```

### 3. 故障排查

常见问题排查步骤：

```bash
# 检查 Istio 组件状态
istioctl proxy-status

# 检查配置同步
istioctl analyze --all-namespaces

# 查看 Envoy 配置
istioctl proxy-config all deploy/your-app -n your-namespace
```

## 成功标准

### 1. 技术指标 ✓
- 零服务中断
- 性能影响 < 15%
- 错误率 < 0.1%
- 所有测试通过

### 2. 业务指标 ✓
- 用户体验无影响
- API 可用性 > 99.9%
- 无客户投诉
- 业务功能正常

### 3. 运维指标 ✓
- 告警数量正常
- 资源使用稳定
- 日志无异常
- 团队响应及时

## 后续优化

### 1. 短期（1-2周）
- 性能调优
- 告警规则优化
- 文档完善
- 培训补充

### 2. 中期（1-3月）
- 高级功能启用（熔断、重试）
- 安全策略加强
- 自动化增强
- 成本优化

### 3. 长期（3-6月）
- 服务网格扩展
- 多集群支持
- 高级流量管理
- AI/ML 驱动的优化

## 签核确认

在执行全面切换前，请确保以下人员已审核并签核：

- [ ] 技术负责人：_______________ 日期：_______________
- [ ] 运维负责人：_______________ 日期：_______________
- [ ] 安全负责人：_______________ 日期：_______________
- [ ] 业务负责人：_______________ 日期：_______________

## 附录

### A. 相关文档
- [Istio 官方文档](https://istio.io/latest/docs/)
- [迁移操作手册](./5.1-gradual-rollout-strategy.md)
- [监控配置指南](./5.2-monitoring-dashboard.md)
- [应急响应流程](./emergency-response.md)

### B. 工具和脚本
- 完整切换脚本：`scripts/istio-migration/phase6-full-cutover.sh`
- 监控设置脚本：`scripts/istio-migration/phase6-monitoring-setup.sh`
- 紧急回滚脚本：`scripts/istio-migration/emergency-rollback.sh`
- 性能测试工具：`tests/istio-migration/performance/`

### C. 联系信息
- 值班电话：xxx-xxxx-xxxx
- 紧急邮箱：emergency@example.com
- Slack 频道：#istio-migration
- 技术支持：support@sealos.io