# Adminer CORS 策略配置

## 概述
为 Adminer 应用添加了特殊的 CORS (跨源资源共享) 策略配置，允许从特定的源访问 Adminer 应用。

## 实现细节

### 1. 检测 Adminer 应用
系统会自动检测应用名称是否包含 "adminer"（不区分大小写）。

### 2. CORS 策略配置
当检测到 Adminer 应用时，会应用以下特殊的 CORS 策略：

```yaml
corsPolicy:
  allowOrigins:
    - exact: "https://adminer.cloud.sealos.io"  # 根据实际公共域名动态生成
  allowMethods:
    - GET
    - POST
    - PUT
    - DELETE
    - OPTIONS
    - PATCH
  allowHeaders:
    - content-type
    - authorization
    - upgrade
    - connection
  allowCredentials: true  # Adminer 可能需要凭据
  maxAge: "24h"
```

### 3. 动态域名处理
- 系统会根据配置的公共域名自动生成允许的源
- 支持通配符域名（如 `*.cloud.sealos.io`）
- 对于通配符域名，会生成 `https://adminer.{baseDomain}` 格式的允许源

### 4. 默认 CORS 策略
非 Adminer 应用将继续使用默认的 CORS 策略，允许所有源访问：

```yaml
corsPolicy:
  allowOrigins:
    - regex: ".*"
  allowMethods: [GET, POST, PUT, DELETE, OPTIONS, PATCH]
  allowHeaders: [content-type, authorization, upgrade, connection]
  allowCredentials: false
  maxAge: "24h"
```

## 使用示例

当创建名为 "adminer" 或包含 "adminer" 的应用时（如 "my-adminer-app"），系统会自动应用特殊的 CORS 策略。

假设公共域名配置为 `*.cloud.sealos.io`，生成的 VirtualService 将包含：

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: adminer-network
spec:
  http:
    - corsPolicy:
        allowOrigins:
          - exact: "https://adminer.cloud.sealos.io"
        allowMethods: [GET, POST, PUT, DELETE, OPTIONS, PATCH]
        allowHeaders: [content-type, authorization, upgrade, connection]
        allowCredentials: true
        maxAge: "24h"
```

## 注意事项

1. **域名配置**：确保在全局配置中正确设置了 `ISTIO_PUBLIC_DOMAINS`
2. **应用命名**：只有包含 "adminer" 的应用名称才会触发特殊 CORS 策略
3. **安全考虑**：特殊的 CORS 策略限制了访问源，提高了 Adminer 应用的安全性