# CLAUDE.md

此文件为 Claude Code (claude.ai/code) 在处理此代码库时提供指导。

## 项目概述

Sealos 是一个基于 Kubernetes 的云操作系统，提供完整的 PaaS 解决方案。这是一个大型 monorepo，包含：
- **后端**：基于 Go 的 Kubernetes 控制器、操作器和微服务
- **前端**：TypeScript/React/Next.js Web 应用程序
- **基础设施**：Kubernetes 原生架构，带有自定义资源

## 常用开发命令

### 前端开发

```bash
# 安装依赖（需要 pnpm v8.9.0）
cd frontend
pnpm install

# 在开发模式下运行特定的前端应用
pnpm dev-desktop      # 桌面应用（主UI）
pnpm dev-app         # 应用启动器
pnpm dev-db          # 数据库提供者
pnpm dev-cost        # 费用中心
pnpm dev-terminal    # 终端
pnpm dev-devbox      # 开发箱

# 构建前端包
pnpm build-packages

# 检查前端代码
cd frontend/desktop  # 或其他应用目录
pnpm lint

# 运行前端测试
pnpm test:e2e        # 端到端测试
pnpm test:ci         # CI 测试
```

### 后端开发

```bash
# 控制器开发（示例：account 控制器）
cd controllers/account
make manifests       # 生成 CRD 和 webhook
make generate        # 生成 DeepCopy 方法
make test           # 运行测试并生成覆盖率报告
make build          # 构建控制器二进制文件
make run            # 本地运行控制器

# Lifecycle/Core 开发
cd lifecycle
make build          # 为主机平台构建
make lint           # 运行 golangci-lint
make format         # 格式化 Go 代码
make test           # 运行测试
make coverage       # 运行测试并生成覆盖率报告
```

### Docker/容器命令

```bash
# 构建前端镜像
cd frontend
make image-build-desktop DOCKER_USERNAME=<用户名> IMAGE_TAG=<标签>

# 构建后端镜像
cd controllers/<控制器名称>
make docker-build
make docker-push
```

## 架构概览

### 目录结构

- **`/lifecycle`**：Sealos 核心二进制文件（sealos、sealctl、lvscare）
  - 集群管理的主要入口点
  - 使用 Kubernetes client-go、containerd 和 Helm

- **`/controllers`**：Kubernetes 控制器和操作器
  - 每个控制器都是独立的 Go 模块
  - 为不同功能实现自定义资源（CRD）
  - 主要控制器：account、app、user、resources、terminal、objectstorage

- **`/service`**：提供 REST API 的微服务
  - account、database、pay、launchpad、license 服务
  - 使用 Go 构建，连接到控制器

- **`/frontend`**：Web UI 应用程序（pnpm 工作区）
  - `desktop`：主 UI 应用程序
  - `providers/*`：各个功能 UI（applaunchpad、dbprovider 等）
  - `packages/*`：共享组件和工具
  - 使用 Next.js、React、Chakra UI、TypeScript 构建

### 核心技术

- **后端**：Go 1.24+、Kubernetes API、controller-runtime
- **前端**：Node.js v20.4.0、pnpm v8.9.0、Next.js、React、TypeScript
- **数据库**：MongoDB（用于服务）、PostgreSQL（通过 Prisma 在前端使用）
- **容器**：Docker、containerd、buildah
- **Kubernetes**：v1.27+ 兼容性、CRD、webhook

### 开发模式

1. **控制器**：遵循 Kubernetes 控制器模式，使用协调循环
2. **前端应用**：每个应用都是独立的 Next.js 应用程序，共享包
3. **API 通信**：前端 → 服务 API → 控制器 → Kubernetes
4. **多租户**：通过命名空间和 RBAC 进行用户隔离
5. **数据库访问**：前端使用 Prisma ORM，服务中使用直接的 MongoDB 驱动

### 测试方法

- **前端**：使用 Playwright 进行 E2E 测试，组件测试
- **后端**：使用 Go 测试包进行单元测试，覆盖率报告
- **集成**：GitHub Actions 工作流针对多个 Kubernetes 版本进行测试

### 重要文件检查

- `frontend/.env.template.*`：前端应用的环境变量模板
- `controllers/*/config/crd/bases/*.yaml`：CRD 定义
- `frontend/desktop/prisma/global/schema.prisma`：数据库模式
- `lifecycle/pkg/types/v1beta1/config.go`：核心配置类型

### 迁移和开发工具

- **`/tools/istio-migration/`**：Ingress 到 Istio 迁移工具集，包括：
  - `converter/`：Go 语言编写的 Ingress 到 Gateway/VirtualService 转换工具
  - `scripts/validate-migration.sh`：迁移验证脚本，支持完整性和流量测试
  - `scripts/rollback.sh`：快速回滚脚本，支持从 Istio 回滚到 Ingress
  - `docs/`：详细的操作文档和技术方案设计
  - `examples/`：测试用例和使用示例
  
```bash
# 构建和使用迁移工具
cd tools/istio-migration
make build              # 构建所有工具
./bin/converter -help   # 查看转换工具帮助
./scripts/validate-migration.sh -h  # 查看验证脚本帮助
```

## 项目任务计划

- **`/todo.md`**：项目的详细任务计划和技术改造方案，包括：
  - Ingress 到 Istio Gateway/VirtualService 迁移计划
  - 技术可行性分析
  - 分阶段执行计划
  - 风险管理和成功标准