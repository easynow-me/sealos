/*
Copyright 2025 labring.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package istio

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Protocol 支持的协议类型
type Protocol string

const (
	ProtocolHTTP      Protocol = "http"
	ProtocolHTTPS     Protocol = "https"
	ProtocolGRPC      Protocol = "grpc"
	ProtocolWebSocket Protocol = "websocket"
	ProtocolTCP       Protocol = "tcp"
)

// NetworkingManager 统一管理所有网络资源
type NetworkingManager interface {
	// 创建应用的完整网络配置
	CreateAppNetworking(ctx context.Context, spec *AppNetworkingSpec) error

	// 更新网络配置
	UpdateAppNetworking(ctx context.Context, spec *AppNetworkingSpec) error

	// 删除网络配置
	DeleteAppNetworking(ctx context.Context, name, namespace string) error

	// 暂停网络访问
	SuspendNetworking(ctx context.Context, namespace string) error

	// 恢复网络访问
	ResumeNetworking(ctx context.Context, namespace string) error

	// 检查网络状态
	GetNetworkingStatus(ctx context.Context, name, namespace string) (*NetworkingStatus, error)
}

// AppNetworkingSpec 应用网络配置规范
type AppNetworkingSpec struct {
	// 基础信息
	Name      string
	Namespace string
	TenantID  string
	AppName   string

	// 网络配置
	Protocol    Protocol
	Hosts       []string
	ServiceName string
	ServicePort int32

	// TLS 配置
	TLSConfig *TLSConfig

	// 高级配置
	Timeout    *time.Duration
	Retries    *RetryPolicy
	CorsPolicy *CorsPolicy
	Headers    map[string]string

	// 安全配置
	SecretHeader string // Terminal 专用

	// 标签和注解
	Labels      map[string]string
	Annotations map[string]string
}

// TLSConfig TLS 配置
type TLSConfig struct {
	SecretName string
	Hosts      []string
}

// RetryPolicy 重试策略
type RetryPolicy struct {
	Attempts      int32
	PerTryTimeout *time.Duration
}

// CorsPolicy CORS 策略
type CorsPolicy struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	AllowCredentials bool
	MaxAge           *time.Duration
}

// NetworkingStatus 网络状态
type NetworkingStatus struct {
	// 资源状态
	GatewayReady        bool
	VirtualServiceReady bool

	// 配置状态
	Hosts      []string
	TLSEnabled bool

	// 错误信息
	LastError string

	// 时间戳
	LastUpdated time.Time
}

// GatewayConfig Gateway 配置
type GatewayConfig struct {
	Name      string
	Namespace string
	Hosts     []string
	TLSConfig *TLSConfig
	Labels    map[string]string
	UseShared bool // 是否使用共享 Gateway
}

// VirtualServiceConfig VirtualService 配置
type VirtualServiceConfig struct {
	Name        string
	Namespace   string
	Hosts       []string
	Gateways    []string
	Protocol    Protocol
	ServiceName string
	ServicePort int32
	Timeout     *time.Duration
	Retries     *RetryPolicy
	CorsPolicy  *CorsPolicy
	Headers     map[string]string
	Labels      map[string]string
}

// GatewayController Gateway 控制器接口
type GatewayController interface {
	// 创建 Gateway
	Create(ctx context.Context, config *GatewayConfig) error

	// 更新 Gateway
	Update(ctx context.Context, config *GatewayConfig) error

	// 删除 Gateway
	Delete(ctx context.Context, name, namespace string) error

	// 获取 Gateway
	Get(ctx context.Context, name, namespace string) (*Gateway, error)

	// 检查 Gateway 是否存在
	Exists(ctx context.Context, name, namespace string) (bool, error)
}

// VirtualServiceController VirtualService 控制器接口
type VirtualServiceController interface {
	// 创建 VirtualService
	Create(ctx context.Context, config *VirtualServiceConfig) error

	// 更新 VirtualService
	Update(ctx context.Context, config *VirtualServiceConfig) error

	// 删除 VirtualService
	Delete(ctx context.Context, name, namespace string) error

	// 获取 VirtualService
	Get(ctx context.Context, name, namespace string) (*VirtualService, error)

	// 暂停 VirtualService（设置为不可达）
	Suspend(ctx context.Context, name, namespace string) error

	// 恢复 VirtualService
	Resume(ctx context.Context, name, namespace string) error
}

// Gateway Istio Gateway 资源（简化版本）
type Gateway struct {
	Name      string
	Namespace string
	Hosts     []string
	TLS       bool
	Ready     bool
}

// VirtualService Istio VirtualService 资源（简化版本）
type VirtualService struct {
	Name        string
	Namespace   string
	Hosts       []string
	Gateways    []string
	ServiceName string
	ServicePort int32
	Protocol    Protocol
	Suspended   bool
	Ready       bool
}

// DomainAllocator 域名分配器接口
type DomainAllocator interface {
	// 生成应用域名
	GenerateAppDomain(tenantID, appName string) string

	// 验证自定义域名
	ValidateCustomDomain(domain string) error

	// 检查域名是否可用
	IsDomainAvailable(domain string) (bool, error)
}

// CertificateManager 证书管理器接口
type CertificateManager interface {
	// 创建或更新证书
	CreateOrUpdate(ctx context.Context, domain string, namespace string) error

	// 检查证书过期时间
	CheckExpiration(ctx context.Context, secretName string, namespace string) (time.Time, error)

	// 轮换证书
	Rotate(ctx context.Context, secretName string, namespace string) error

	// 删除证书
	Delete(ctx context.Context, secretName string, namespace string) error
}

// NetworkConfig 网络配置
type NetworkConfig struct {
	// 基础配置
	BaseDomain       string
	DefaultGateway   string
	DefaultTLSSecret string
	TLSEnabled       bool

	// 域名配置
	DomainTemplates map[string]string
	ReservedDomains []string

	// 证书配置
	CertManager string
	AutoTLS     bool

	// Gateway 配置
	GatewaySelector      map[string]string
	SharedGatewayEnabled bool
}

// NamespacedName 带命名空间的名称
type NamespacedName = types.NamespacedName

// Client Kubernetes 客户端接口（用于依赖注入）
type Client = client.Client
