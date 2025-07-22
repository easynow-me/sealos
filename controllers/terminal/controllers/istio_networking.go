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

package controllers

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	terminalv1 "github.com/labring/sealos/controllers/terminal/api/v1"
	"github.com/labring/sealos/controllers/pkg/istio"
)

// IstioNetworkingReconciler Istio 网络配置协调器
type IstioNetworkingReconciler struct {
	client.Client
	networkingManager istio.NetworkingManager
	config           *istio.NetworkConfig
}

// NewIstioNetworkingReconciler 创建新的 Istio 网络协调器（使用优化管理器）
func NewIstioNetworkingReconciler(client client.Client, config *istio.NetworkConfig) *IstioNetworkingReconciler {
	return &IstioNetworkingReconciler{
		Client:            client,
		networkingManager: istio.NewOptimizedNetworkingManager(client, config), // 🎯 使用优化管理器
		config:            config,
	}
}

// SyncIstioNetworking 同步 Terminal 的 Istio 网络配置
func (r *IstioNetworkingReconciler) SyncIstioNetworking(ctx context.Context, terminal *terminalv1.Terminal, hostname string) error {
	// 构建网络配置规范
	spec := r.buildNetworkingSpec(terminal, hostname)
	
	// 验证配置
	if err := istio.ValidateNetworkingSpec(spec); err != nil {
		return fmt.Errorf("invalid networking spec: %w", err)
	}
	
	// 检查是否已存在
	status, err := r.networkingManager.GetNetworkingStatus(ctx, terminal.Name, terminal.Namespace)
	if err != nil {
		// 如果不存在，创建新的网络配置
		return r.networkingManager.CreateAppNetworking(ctx, spec)
	}
	
	// 如果存在但配置可能已更改，更新配置
	if r.needsUpdate(terminal, status) {
		return r.networkingManager.UpdateAppNetworking(ctx, spec)
	}
	
	return nil
}

// DeleteIstioNetworking 删除 Terminal 的 Istio 网络配置
func (r *IstioNetworkingReconciler) DeleteIstioNetworking(ctx context.Context, terminal *terminalv1.Terminal) error {
	return r.networkingManager.DeleteAppNetworking(ctx, terminal.Name, terminal.Namespace)
}

// buildNetworkingSpec 构建 Terminal 的网络配置规范
func (r *IstioNetworkingReconciler) buildNetworkingSpec(terminal *terminalv1.Terminal, hostname string) *istio.AppNetworkingSpec {
	// 构建域名
	domain := hostname + "." + r.config.BaseDomain
	
	// 构建 CORS 源 - 使用精确匹配的 terminal 子域名
	corsOrigins := r.buildCorsOrigins()
	
	spec := &istio.AppNetworkingSpec{
		Name:         terminal.Name,
		Namespace:    terminal.Namespace,
		TenantID:     r.extractTenantID(terminal.Namespace),
		AppName:      "terminal",
		Protocol:     istio.ProtocolWebSocket,
		Hosts:        []string{domain},
		ServiceName:  terminal.Status.ServiceName,
		ServicePort:  8080,
		SecretHeader: terminal.Status.SecretHeader,
		
		// WebSocket 专用配置
		Timeout: &[]time.Duration{86400 * time.Second}[0], // 24小时超时
		
		// CORS 配置
		CorsPolicy: &istio.CorsPolicy{
			AllowOrigins:     corsOrigins,
			AllowMethods:     []string{"PUT", "GET", "POST", "PATCH", "OPTIONS"},
			AllowHeaders:     []string{"content-type", "authorization"},
			AllowCredentials: false,
		},
		
		// 响应头部配置（安全头部）
		ResponseHeaders: r.buildSecurityResponseHeaders(),
		
		// 标签
		Labels: map[string]string{
			"app.kubernetes.io/name":       terminal.Name,
			"app.kubernetes.io/component":  "terminal",
			"app.kubernetes.io/managed-by": "terminal-controller",
			"sealos.io/app-name":           "terminal",
		},
		
		// 注解
		Annotations: map[string]string{
			"converted-from": "ingress",
		},
	}
	
	// 如果启用了 TLS，添加 TLS 配置
	if r.config.TLSEnabled {
		spec.TLSConfig = &istio.TLSConfig{
			SecretName: r.config.DefaultTLSSecret,
			Hosts:      []string{domain},
		}
	}
	
	return spec
}

// needsUpdate 检查是否需要更新网络配置
func (r *IstioNetworkingReconciler) needsUpdate(terminal *terminalv1.Terminal, status *istio.NetworkingStatus) bool {
	// 简单检查：如果 VirtualService 或 Gateway 未就绪，需要更新
	if !status.VirtualServiceReady || !status.GatewayReady {
		return true
	}
	
	// 检查 TLS 配置是否匹配
	if r.config.TLSEnabled != status.TLSEnabled {
		return true
	}
	
	// 可以添加更多检查逻辑
	return false
}

// extractTenantID 从命名空间提取租户 ID
func (r *IstioNetworkingReconciler) extractTenantID(namespace string) string {
	// 假设命名空间格式为 "ns-{tenant-id}"
	if len(namespace) > 3 && namespace[:3] == "ns-" {
		return namespace[3:]
	}
	return namespace
}

// GetNetworkingStatus 获取网络状态
func (r *IstioNetworkingReconciler) GetNetworkingStatus(ctx context.Context, terminal *terminalv1.Terminal) (*istio.NetworkingStatus, error) {
	return r.networkingManager.GetNetworkingStatus(ctx, terminal.Name, terminal.Namespace)
}

// ValidateIstioInstallation 验证 Istio 是否已安装
func (r *IstioNetworkingReconciler) ValidateIstioInstallation(ctx context.Context) error {
	isEnabled, err := istio.IsIstioEnabled(r.Client)
	if err != nil {
		return fmt.Errorf("failed to check Istio installation: %w", err)
	}
	
	if !isEnabled {
		return fmt.Errorf("Istio is not installed or enabled in the cluster")
	}
	
	return nil
}

// buildCorsOrigins 构建CORS源 - 使用精确匹配的terminal子域名
func (r *IstioNetworkingReconciler) buildCorsOrigins() []string {
	corsOrigins := []string{}
	
	if r.config.TLSEnabled {
		// 添加精确的 terminal 子域名
		corsOrigins = append(corsOrigins, fmt.Sprintf("https://terminal.%s", r.config.BaseDomain))
		
		// 如果有配置的公共域名，也添加它们的 terminal 子域名
		if len(r.config.PublicDomains) > 0 {
			for _, publicDomain := range r.config.PublicDomains {
				// 处理通配符域名 (如 *.cloud.sealos.io)
				if len(publicDomain) > 2 && publicDomain[0:2] == "*." {
					baseDomain := publicDomain[2:]
					corsOrigins = append(corsOrigins, fmt.Sprintf("https://terminal.%s", baseDomain))
				} else {
					// 精确域名
					corsOrigins = append(corsOrigins, fmt.Sprintf("https://terminal.%s", publicDomain))
				}
			}
		}
	} else {
		// HTTP 模式
		corsOrigins = append(corsOrigins, fmt.Sprintf("http://terminal.%s", r.config.BaseDomain))
		
		if len(r.config.PublicDomains) > 0 {
			for _, publicDomain := range r.config.PublicDomains {
				if len(publicDomain) > 2 && publicDomain[0:2] == "*." {
					baseDomain := publicDomain[2:]
					corsOrigins = append(corsOrigins, fmt.Sprintf("http://terminal.%s", baseDomain))
				} else {
					corsOrigins = append(corsOrigins, fmt.Sprintf("http://terminal.%s", publicDomain))
				}
			}
		}
	}
	
	// 去重
	uniqueOrigins := make([]string, 0, len(corsOrigins))
	seen := make(map[string]bool)
	for _, origin := range corsOrigins {
		if !seen[origin] {
			uniqueOrigins = append(uniqueOrigins, origin)
			seen[origin] = true
		}
	}
	
	return uniqueOrigins
}

// buildSecurityResponseHeaders 构建安全响应头部
func (r *IstioNetworkingReconciler) buildSecurityResponseHeaders() map[string]string {
	headers := make(map[string]string)
	
	// 设置 X-Frame-Options，防止点击劫持
	headers["X-Frame-Options"] = "SAMEORIGIN"
	
	// 设置 X-Content-Type-Options，防止 MIME 类型嗅探
	headers["X-Content-Type-Options"] = "nosniff"
	
	// 设置 X-XSS-Protection
	headers["X-XSS-Protection"] = "1; mode=block"
	
	// 设置 Referrer-Policy
	headers["Referrer-Policy"] = "strict-origin-when-cross-origin"
	
	// 对于 WebSocket 应用的基本 CSP
	headers["Content-Security-Policy"] = "default-src 'self'; connect-src 'self' wss:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline' 'unsafe-eval';"
	
	return headers
}