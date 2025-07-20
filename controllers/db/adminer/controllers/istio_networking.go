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

	adminerv1 "github.com/labring/sealos/controllers/db/adminer/api/v1"
	"github.com/labring/sealos/controllers/pkg/istio"
)

// AdminerIstioNetworkingReconciler DB Adminer Istio 网络配置协调器
type AdminerIstioNetworkingReconciler struct {
	client.Client
	networkingManager istio.NetworkingManager
	config           *istio.NetworkConfig
	tlsEnabled       bool
	adminerDomain    string
}

// NewAdminerIstioNetworkingReconciler 创建新的 DB Adminer Istio 网络协调器
func NewAdminerIstioNetworkingReconciler(client client.Client, config *istio.NetworkConfig, tlsEnabled bool, adminerDomain string) *AdminerIstioNetworkingReconciler {
	return &AdminerIstioNetworkingReconciler{
		Client:            client,
		networkingManager: istio.NewNetworkingManager(client, config),
		config:            config,
		tlsEnabled:        tlsEnabled,
		adminerDomain:     adminerDomain,
	}
}

// SyncIstioNetworking 同步 DB Adminer 的 Istio 网络配置
func (r *AdminerIstioNetworkingReconciler) SyncIstioNetworking(ctx context.Context, adminer *adminerv1.Adminer, hostname string) error {
	// 构建网络配置规范
	spec := r.buildNetworkingSpec(adminer, hostname)
	
	// 验证配置
	if err := istio.ValidateNetworkingSpec(spec); err != nil {
		return fmt.Errorf("invalid networking spec: %w", err)
	}
	
	// 检查是否已存在
	status, err := r.networkingManager.GetNetworkingStatus(ctx, adminer.Name, adminer.Namespace)
	if err != nil {
		// 如果不存在，创建新的网络配置
		return r.networkingManager.CreateAppNetworking(ctx, spec)
	}
	
	// 如果存在但配置可能已更改，更新配置
	if r.needsUpdate(adminer, status) {
		return r.networkingManager.UpdateAppNetworking(ctx, spec)
	}
	
	return nil
}

// DeleteIstioNetworking 删除 DB Adminer 的 Istio 网络配置
func (r *AdminerIstioNetworkingReconciler) DeleteIstioNetworking(ctx context.Context, adminer *adminerv1.Adminer) error {
	return r.networkingManager.DeleteAppNetworking(ctx, adminer.Name, adminer.Namespace)
}

// buildNetworkingSpec 构建 DB Adminer 的网络配置规范
func (r *AdminerIstioNetworkingReconciler) buildNetworkingSpec(adminer *adminerv1.Adminer, hostname string) *istio.AppNetworkingSpec {
	// 构建域名
	domain := hostname + "." + r.adminerDomain
	
	// 构建 CORS 源
	corsOrigins := []string{}
	if r.tlsEnabled {
		corsOrigins = []string{
			fmt.Sprintf("https://%s", r.adminerDomain),
			fmt.Sprintf("https://*.%s", r.adminerDomain),
		}
	} else {
		corsOrigins = []string{
			fmt.Sprintf("http://%s", r.adminerDomain),
			fmt.Sprintf("http://*.%s", r.adminerDomain),
		}
	}
	
	spec := &istio.AppNetworkingSpec{
		Name:        adminer.Name,
		Namespace:   adminer.Namespace,
		TenantID:    r.extractTenantID(adminer.Namespace),
		AppName:     "adminer",
		Protocol:    istio.ProtocolHTTP,
		Hosts:       []string{domain},
		ServiceName: adminer.Name,
		ServicePort: 8080,
		
		// 数据库管理器专用配置
		Timeout: &[]time.Duration{86400 * time.Second}[0], // 24小时超时，支持长时间数据库操作
		
		// CORS 配置
		CorsPolicy: &istio.CorsPolicy{
			AllowOrigins:     corsOrigins,
			AllowMethods:     []string{"PUT", "GET", "POST", "PATCH", "OPTIONS"},
			AllowHeaders:     []string{"content-type", "authorization"},
			AllowCredentials: false,
		},
		
		// 自定义头部配置，用于安全策略
		Headers: r.buildSecurityHeaders(),
		
		// 标签
		Labels: map[string]string{
			"app.kubernetes.io/name":       adminer.Name,
			"app.kubernetes.io/component":  "database",
			"app.kubernetes.io/managed-by": "adminer-controller",
			"sealos.io/app-name":           "adminer",
		},
		
		// 注解
		Annotations: map[string]string{
			"converted-from": "ingress",
		},
	}
	
	// 如果启用了 TLS，添加 TLS 配置
	if r.tlsEnabled {
		spec.TLSConfig = &istio.TLSConfig{
			SecretName: r.getSecretName(adminer),
			Hosts:      []string{domain},
		}
	}
	
	return spec
}

// buildSecurityHeaders 构建安全头部
func (r *AdminerIstioNetworkingReconciler) buildSecurityHeaders() map[string]string {
	headers := make(map[string]string)
	
	// 清除 X-Frame-Options，允许 iframe 嵌入
	headers["X-Frame-Options"] = ""
	
	// 设置 Content Security Policy
	cspValue := fmt.Sprintf("default-src * blob: data: *.%s %s; img-src * data: blob: resource: *.%s %s; connect-src * wss: blob: resource:; style-src 'self' 'unsafe-inline' blob: *.%s %s resource:; script-src 'self' 'unsafe-inline' 'unsafe-eval' blob: *.%s %s resource: *.baidu.com *.bdstatic.com; frame-src 'self' %s *.%s mailto: tel: weixin: mtt: *.baidu.com; frame-ancestors 'self' https://%s https://*.%s",
		r.adminerDomain, r.adminerDomain,
		r.adminerDomain, r.adminerDomain,
		r.adminerDomain, r.adminerDomain,
		r.adminerDomain, r.adminerDomain,
		r.adminerDomain, r.adminerDomain,
		r.adminerDomain, r.adminerDomain)
	
	headers["Content-Security-Policy"] = cspValue
	
	// 设置 XSS 保护
	headers["X-Xss-Protection"] = "1; mode=block"
	
	return headers
}

// needsUpdate 检查是否需要更新网络配置
func (r *AdminerIstioNetworkingReconciler) needsUpdate(adminer *adminerv1.Adminer, status *istio.NetworkingStatus) bool {
	// 简单检查：如果 VirtualService 或 Gateway 未就绪，需要更新
	if !status.VirtualServiceReady || !status.GatewayReady {
		return true
	}
	
	// 检查 TLS 配置是否匹配
	if r.tlsEnabled != status.TLSEnabled {
		return true
	}
	
	// 可以添加更多检查逻辑
	return false
}

// extractTenantID 从命名空间提取租户 ID
func (r *AdminerIstioNetworkingReconciler) extractTenantID(namespace string) string {
	// 假设命名空间格式为 "ns-{tenant-id}"
	if len(namespace) > 3 && namespace[:3] == "ns-" {
		return namespace[3:]
	}
	return namespace
}

// getSecretName 获取证书 Secret 名称
func (r *AdminerIstioNetworkingReconciler) getSecretName(adminer *adminerv1.Adminer) string {
	// 如果 Adminer 有自定义证书配置，使用它
	// 否则使用默认证书
	return r.config.DefaultTLSSecret
}

// GetNetworkingStatus 获取网络状态
func (r *AdminerIstioNetworkingReconciler) GetNetworkingStatus(ctx context.Context, adminer *adminerv1.Adminer) (*istio.NetworkingStatus, error) {
	return r.networkingManager.GetNetworkingStatus(ctx, adminer.Name, adminer.Namespace)
}

// ValidateIstioInstallation 验证 Istio 是否已安装
func (r *AdminerIstioNetworkingReconciler) ValidateIstioInstallation(ctx context.Context) error {
	isEnabled, err := istio.IsIstioEnabled(r.Client)
	if err != nil {
		return fmt.Errorf("failed to check Istio installation: %w", err)
	}
	
	if !isEnabled {
		return fmt.Errorf("Istio is not installed or enabled in the cluster")
	}
	
	return nil
}