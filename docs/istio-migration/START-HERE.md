# 🚀 Sealos Istio 迁移 - 从这里开始

欢迎使用 Sealos Istio 迁移工具！本指南将帮助您选择合适的迁移路径。

## 📋 第一步：评估您的环境

运行环境评估脚本，了解您的集群状态和推荐的迁移方案：

```bash
./scripts/istio-migration/environment-assessment.sh
```

这个脚本会：
- ✅ 检查 Kubernetes 版本兼容性
- ✅ 统计服务和 Ingress 数量
- ✅ 检查资源可用性
- ✅ 评估集群准备度（0-100分）
- ✅ 推荐合适的迁移方案

## 🎯 第二步：选择迁移方案

根据评估结果，选择适合您的方案：

### 🏃 方案 A：一键迁移（开发/测试环境）

**适用场景**：开发或测试环境，可接受短暂中断

```bash
# ⚠️ 警告：仅用于非生产环境！
./scripts/istio-migration/one-click-migration.sh --domain your-domain.com
```

**耗时**：约 1 小时

---

### ⚡ 方案 B：快速迁移（小规模生产）

**适用场景**：服务数量 < 100，有 4 小时维护窗口

**执行步骤**：
1. 查看快速指南：[quick-migration-checklist.md](./quick-migration-checklist.md)
2. 执行迁移脚本：
   ```bash
   ./scripts/istio-migration/phase6-full-cutover.sh --step all
   ```

**耗时**：2-4 小时

---

### 🎯 方案 C：标准迁移（中型生产）

**适用场景**：服务数量 100-500，需要零停机

**执行步骤**：
1. 阅读完整指南：[complete-migration-guide.md](./complete-migration-guide.md)
2. 按步骤执行每个阶段
3. 在每个阶段后验证

**耗时**：1-2 天

---

### 🛡️ 方案 D：保守迁移（大型生产）

**适用场景**：关键业务系统，服务数量 > 500

**执行步骤**：
1. 查看决策树：[migration-decision-tree.md](./migration-decision-tree.md)
2. 制定详细计划
3. 分阶段执行

**耗时**：3-5 天

## 📚 核心文档

### 必读文档
- 🎯 [迁移决策树](./migration-decision-tree.md) - 帮助选择合适方案
- 📋 [生产就绪检查清单](./phase6-production-readiness.md) - 上线前必查
- 🚨 [LoadBalancer 限制说明](./loadbalancer-restriction-implementation.md) - 重要架构变更

### 操作指南
- 📖 [完整迁移指南](./complete-migration-guide.md) - 详细步骤说明
- ⚡ [快速迁移清单](./quick-migration-checklist.md) - 快速执行参考
- 🔄 [当前状态报告](./current-status.md) - 项目进度总览

### 参考文档
- 👥 [用户使用指南](./user-guide.md) - 面向最终用户
- 🔧 [运维操作手册](./operations-guide.md) - 日常运维指南
- 📊 [监控配置指南](./5.2-monitoring-dashboard.md) - 监控设置

## 🛠️ 核心脚本

### 迁移执行
- `phase6-full-cutover.sh` - 主迁移脚本（支持分步执行）
- `one-click-migration.sh` - 一键迁移脚本（仅限测试环境）
- `environment-assessment.sh` - 环境评估脚本

### 监控和验证
- `phase6-monitoring-setup.sh` - 监控设置脚本
- `run-test-suite.sh` - 测试套件运行器

### 应急工具
- `emergency-rollback.sh` - 紧急回滚脚本
- `gradual-rollback.sh` - 渐进回滚脚本

## ⚡ 快速开始示例

### 示例 1：测试环境快速体验

```bash
# 1. 评估环境
./scripts/istio-migration/environment-assessment.sh

# 2. 如果评分 > 80，执行一键迁移
./scripts/istio-migration/one-click-migration.sh --confirm

# 3. 验证结果
kubectl get virtualservices --all-namespaces
```

### 示例 2：生产环境标准迁移

```bash
# 1. 评估和准备
./scripts/istio-migration/environment-assessment.sh

# 2. 设置监控
./scripts/istio-migration/phase6-monitoring-setup.sh --component all

# 3. 分步执行
./scripts/istio-migration/phase6-full-cutover.sh --step disable-ingress
./scripts/istio-migration/phase6-full-cutover.sh --step migrate-existing
./scripts/istio-migration/phase6-full-cutover.sh --step validate
./scripts/istio-migration/phase6-full-cutover.sh --step cleanup
```

## 🆘 需要帮助？

### 故障排查
```bash
# 查看 Istio 状态
istioctl proxy-status

# 分析配置问题
istioctl analyze --all-namespaces

# 查看日志
kubectl logs -n istio-system deployment/istiod --tail=100
```

### 紧急回滚
```bash
# 完全回滚
./scripts/istio-migration/emergency-rollback.sh --mode full

# 部分回滚
./scripts/istio-migration/emergency-rollback.sh --mode partial
```

### 获取支持
- 📧 邮箱：istio-support@sealos.io
- 💬 Slack：#istio-migration
- 📞 紧急：查看您的运维手册

## ✅ 迁移成功标志

您的迁移成功完成的标志：
- 所有 VirtualService 资源创建成功
- 监控显示流量正常（错误率 < 0.1%）
- 性能影响在可接受范围内（< 15%）
- 所有功能测试通过
- 24 小时稳定运行

---

**提示**：
- 🕐 选择合适的维护窗口
- 📊 持续监控关键指标
- 📝 记录所有操作步骤
- 👥 保持团队沟通

祝您迁移顺利！🎉