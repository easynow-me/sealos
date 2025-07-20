# Istio Migration Integration Tests

这个目录包含了 Sealos Ingress 到 Istio Gateway/VirtualService 迁移的集成测试套件。

## 测试结构

```
tests/istio-migration/
├── README.md                 # 本文件
├── config/                   # 测试配置文件
│   ├── test-config.yaml     # 测试配置
│   └── environments.yaml    # 环境配置
├── functional/               # 功能测试
│   ├── multi-tenant/        # 多租户隔离测试
│   ├── protocols/           # 协议支持测试
│   ├── certificates/        # SSL证书测试
│   ├── domains/             # 域名管理测试
│   └── cors/                # CORS配置测试
├── performance/              # 性能测试
│   ├── latency/             # 延迟测试
│   ├── throughput/          # 吞吐量测试
│   ├── resources/           # 资源消耗测试
│   └── scale/               # 大规模测试
├── compatibility/            # 兼容性测试
│   ├── upgrade/             # 升级路径测试
│   ├── rollback/            # 回滚测试
│   └── integration/         # 现有功能集成测试
├── scripts/                  # 测试脚本
│   ├── setup-test-env.sh    # 测试环境搭建
│   ├── run-all-tests.sh     # 运行所有测试
│   ├── cleanup.sh           # 清理环境
│   └── helpers/             # 辅助脚本
└── reports/                  # 测试报告
    ├── functional/          # 功能测试报告
    ├── performance/         # 性能测试报告
    └── compatibility/       # 兼容性测试报告
```

## 测试分类

### 功能测试 (Functional Tests)

#### 1. 多租户隔离测试
- 验证不同命名空间之间的网络隔离
- 测试Gateway和VirtualService的命名空间作用域
- 验证跨租户访问控制

#### 2. 协议支持测试  
- **HTTP测试**: 基本HTTP请求/响应、CORS、重定向
- **GRPC测试**: GRPC服务调用、流式传输、错误处理
- **WebSocket测试**: 连接建立、消息传输、长连接维持

#### 3. SSL证书测试
- 证书自动获取和续期
- 自定义证书配置
- 通配符证书支持

#### 4. 域名管理测试
- 自动域名分配
- 自定义域名配置
- 域名冲突处理

#### 5. CORS配置测试
- 跨域请求处理
- 预检请求验证
- 安全头部配置

### 性能测试 (Performance Tests)

#### 1. 延迟测试
- 请求响应时间对比（Ingress vs Istio）
- P50、P95、P99延迟分析
- 连接建立时间测试

#### 2. 吞吐量测试
- 最大并发连接数
- 每秒请求数(RPS)对比
- 带宽利用率测试

#### 3. 资源消耗测试
- CPU使用率对比
- 内存使用率对比
- 网络开销分析

#### 4. 大规模测试
- 1000+ 应用场景测试
- 高并发访问压力测试
- 资源扩缩容测试

### 兼容性测试 (Compatibility Tests)

#### 1. 升级路径测试
- 平滑迁移验证
- 数据一致性检查
- 服务可用性保证

#### 2. 回滚测试
- 快速回滚机制验证
- 回滚后功能完整性
- 数据恢复验证

#### 3. 现有功能集成测试
- Terminal控制器功能验证
- DB Adminer功能验证
- 应用部署流程验证

## 运行测试

### 环境准备

```bash
# 1. 准备测试环境
./scripts/setup-test-env.sh

# 2. 检查环境状态
kubectl get pods -n istio-system
kubectl get pods -n sealos-system
```

### 运行全部测试

```bash
# 运行所有测试
./scripts/run-all-tests.sh

# 生成测试报告
./scripts/generate-report.sh
```

### 运行特定测试

```bash
# 仅运行功能测试
./scripts/run-all-tests.sh --functional

# 仅运行性能测试
./scripts/run-all-tests.sh --performance

# 运行特定协议测试
./scripts/run-all-tests.sh --protocol websocket
```

### 清理环境

```bash
# 清理测试资源
./scripts/cleanup.sh
```

## 测试配置

测试配置文件位于 `config/` 目录：

- `test-config.yaml`: 主要测试配置
- `environments.yaml`: 测试环境配置

可以通过修改这些配置文件来调整测试参数。

## 测试报告

测试完成后，报告将生成在 `reports/` 目录：

- **功能测试报告**: 测试通过率、失败用例详情
- **性能测试报告**: 性能指标对比、图表分析
- **兼容性测试报告**: 兼容性问题和解决方案

## 贡献指南

添加新测试用例时，请遵循以下规范：

1. 在对应分类目录下创建测试文件
2. 使用统一的测试框架和工具
3. 编写清晰的测试文档
4. 确保测试可重复执行
5. 添加必要的清理逻辑

## 问题排查

遇到测试问题时，请检查：

1. 测试环境是否正确搭建
2. 必要的依赖是否已安装
3. 网络连接是否正常
4. 权限配置是否正确

详细的问题排查指南请参考 [TROUBLESHOOTING.md](TROUBLESHOOTING.md)。