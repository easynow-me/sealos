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
	"os"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/labring/sealos/controllers/pkg/istio"
	terminalv1 "github.com/labring/sealos/controllers/terminal/api/v1"
)

// SetupIstioSupport 设置 Terminal 控制器的 Istio 支持
func (r *TerminalReconciler) SetupIstioSupport(ctx context.Context) error {
	logger := log.FromContext(ctx)
	
	// 检查是否启用 Istio
	useIstio := os.Getenv("USE_ISTIO")
	if useIstio != "true" {
		logger.Info("Istio support is disabled")
		r.useIstio = false
		return nil
	}
	
	// 检查 Istio 是否已安装
	isEnabled, err := istio.IsIstioEnabled(r.Client)
	if err != nil {
		logger.Error(err, "failed to check Istio installation")
		return err
	}
	
	if !isEnabled {
		logger.Info("Istio is not installed, falling back to Ingress mode")
		r.useIstio = false
		return nil
	}
	
	// 构建 Istio 网络配置
	config := r.buildIstioNetworkConfig()
	
	// 🎯 使用通用 Istio 网络助手（替代自定义协调器）
	r.istioHelper = istio.NewUniversalIstioNetworkingHelperWithScheme(r.Client, r.Scheme, config, "terminal")
	
	// 保留旧协调器用于向后兼容和验证
	r.istioReconciler = NewIstioNetworkingReconciler(r.Client, config)
	
	// 验证 Istio 安装
	if err := r.istioReconciler.ValidateIstioInstallation(ctx); err != nil {
		logger.Error(err, "Istio validation failed, falling back to Ingress mode")
		r.useIstio = false
		r.istioReconciler = nil
		r.istioHelper = nil
		return nil
	}
	
	r.useIstio = true
	logger.Info("Istio support enabled for Terminal controller")
	
	return nil
}

// buildIstioNetworkConfig 构建 Istio 网络配置（使用智能Gateway优化）
func (r *TerminalReconciler) buildIstioNetworkConfig() *istio.NetworkConfig {
	config := istio.DefaultNetworkConfig()
	
	// 基础域名配置
	if baseDomain := os.Getenv("ISTIO_BASE_DOMAIN"); baseDomain != "" {
		config.BaseDomain = baseDomain
	} else if r.CtrConfig != nil && r.CtrConfig.Global.CloudDomain != "" {
		config.BaseDomain = r.CtrConfig.Global.CloudDomain
	}
	
	// Gateway配置
	if defaultGateway := os.Getenv("ISTIO_DEFAULT_GATEWAY"); defaultGateway != "" {
		config.DefaultGateway = defaultGateway
	} else {
		config.DefaultGateway = "istio-system/sealos-gateway"
	}
	
	// TLS证书配置
	if tlsSecret := os.Getenv("ISTIO_TLS_SECRET"); tlsSecret != "" {
		config.DefaultTLSSecret = tlsSecret
	}
	
	// 🎯 新增：公共域名配置（支持智能Gateway选择）
	r.configurePublicDomains(config)
	
	// Terminal 专用的域名模板
	config.DomainTemplates["terminal"] = "terminal-{{.Hash}}.{{.TenantID}}.{{.BaseDomain}}"
	
	// 检查是否启用 TLS
	if enableTLS := os.Getenv("ISTIO_ENABLE_TLS"); enableTLS == "false" {
		config.TLSEnabled = false
	} else {
		config.TLSEnabled = true // 默认启用TLS
	}
	
	// 检查是否使用共享 Gateway
	if sharedGateway := os.Getenv("ISTIO_SHARED_GATEWAY"); sharedGateway == "false" {
		config.SharedGatewayEnabled = false
	} else {
		config.SharedGatewayEnabled = true // 默认启用智能共享Gateway
	}
	
	return config
}

// configurePublicDomains 配置公共域名（智能Gateway核心配置）
func (r *TerminalReconciler) configurePublicDomains(config *istio.NetworkConfig) {
	// 1. 基础域名和子域名
	if config.BaseDomain != "" {
		config.PublicDomains = append(config.PublicDomains, config.BaseDomain)
		config.PublicDomainPatterns = append(config.PublicDomainPatterns, "*."+config.BaseDomain)
	}
	
	// 2. 从环境变量读取额外的公共域名
	if publicDomains := os.Getenv("ISTIO_PUBLIC_DOMAINS"); publicDomains != "" {
		domains := strings.Split(publicDomains, ",")
		for _, domain := range domains {
			domain = strings.TrimSpace(domain)
			if domain != "" {
				config.PublicDomains = append(config.PublicDomains, domain)
			}
		}
	}
	
	// 3. 从环境变量读取公共域名模式（支持通配符）
	if domainPatterns := os.Getenv("ISTIO_PUBLIC_DOMAIN_PATTERNS"); domainPatterns != "" {
		patterns := strings.Split(domainPatterns, ",")
		for _, pattern := range patterns {
			pattern = strings.TrimSpace(pattern)
			if pattern != "" {
				config.PublicDomainPatterns = append(config.PublicDomainPatterns, pattern)
			}
		}
	}
	
	// 4. 默认公共域名模式（如果没有配置）
	if len(config.PublicDomains) == 0 && len(config.PublicDomainPatterns) == 0 {
		// 使用Terminal控制器的云域名作为默认公共域名
		if r.CtrConfig != nil && r.CtrConfig.Global.CloudDomain != "" {
			config.PublicDomains = append(config.PublicDomains, r.CtrConfig.Global.CloudDomain)
			config.PublicDomainPatterns = append(config.PublicDomainPatterns, "*."+r.CtrConfig.Global.CloudDomain)
		}
	}
}

// IsIstioEnabled 检查是否启用了 Istio 模式
func (r *TerminalReconciler) IsIstioEnabled() bool {
	return r.useIstio
}

// GetNetworkingStatus 获取 Terminal 的网络状态
func (r *TerminalReconciler) GetNetworkingStatus(ctx context.Context, terminalName, namespace string) (*istio.NetworkingStatus, error) {
	if !r.useIstio || r.istioReconciler == nil {
		return nil, fmt.Errorf("Istio mode is not enabled")
	}
	
	// 获取 Terminal 资源
	terminal := &terminalv1.Terminal{}
	if err := r.Get(ctx, client.ObjectKey{Name: terminalName, Namespace: namespace}, terminal); err != nil {
		return nil, err
	}
	
	return r.istioReconciler.GetNetworkingStatus(ctx, terminal)
}

// EnableIstioMode 动态启用 Istio 模式
func (r *TerminalReconciler) EnableIstioMode(ctx context.Context) error {
	if r.useIstio {
		return nil // 已经启用
	}
	
	return r.SetupIstioSupport(ctx)
}

// DisableIstioMode 禁用 Istio 模式，回退到 Ingress
func (r *TerminalReconciler) DisableIstioMode() {
	r.useIstio = false
	r.istioReconciler = nil
}