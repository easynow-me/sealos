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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	adminerv1 "github.com/labring/sealos/controllers/db/adminer/api/v1"
	"github.com/labring/sealos/controllers/pkg/istio"
)

// SetupIstioSupport 设置 DB Adminer 控制器的 Istio 支持
func (r *AdminerReconciler) SetupIstioSupport(ctx context.Context) error {
	logger := log.FromContext(ctx)
	
	// 检查是否启用 Istio
	useIstio := os.Getenv("USE_ISTIO")
	if useIstio != "true" {
		logger.Info("Istio support is disabled for Adminer")
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
		logger.Info("Istio is not installed, falling back to Ingress mode for Adminer")
		r.useIstio = false
		return nil
	}
	
	// 构建 Istio 网络配置
	config := r.buildIstioNetworkConfig()
	
	// 创建 Istio 网络协调器
	r.istioReconciler = NewAdminerIstioNetworkingReconciler(r.Client, config, r.tlsEnabled, r.adminerDomain)
	
	// 验证 Istio 安装
	if err := r.istioReconciler.ValidateIstioInstallation(ctx); err != nil {
		logger.Error(err, "Istio validation failed, falling back to Ingress mode for Adminer")
		r.useIstio = false
		r.istioReconciler = nil
		return nil
	}
	
	r.useIstio = true
	logger.Info("Istio support enabled for Adminer controller")
	
	return nil
}

// buildIstioNetworkConfig 构建 Istio 网络配置
func (r *AdminerReconciler) buildIstioNetworkConfig() *istio.NetworkConfig {
	config := istio.DefaultNetworkConfig()
	
	// 使用 Adminer 控制器的配置
	if r.adminerDomain != "" {
		config.BaseDomain = r.adminerDomain
	}
	
	// 从环境变量读取配置
	if baseDomain := os.Getenv("ISTIO_BASE_DOMAIN"); baseDomain != "" {
		config.BaseDomain = baseDomain
	} else if r.adminerDomain != "" {
		config.BaseDomain = r.adminerDomain
	}
	
	if defaultGateway := os.Getenv("ISTIO_DEFAULT_GATEWAY"); defaultGateway != "" {
		config.DefaultGateway = defaultGateway
	}
	
	if tlsSecret := os.Getenv("ISTIO_TLS_SECRET"); tlsSecret != "" {
		config.DefaultTLSSecret = tlsSecret
	} else if r.secretName != "" {
		config.DefaultTLSSecret = r.secretName
	}
	
	// DB Adminer 专用的域名模板
	config.DomainTemplates["database"] = "db-{{.Hash}}.{{.TenantID}}.{{.BaseDomain}}"
	config.DomainTemplates["adminer"] = "adminer-{{.Hash}}.{{.TenantID}}.{{.BaseDomain}}"
	
	// 设置 TLS 状态
	config.TLSEnabled = r.tlsEnabled
	
	// 检查是否使用共享 Gateway
	if sharedGateway := os.Getenv("ISTIO_SHARED_GATEWAY"); sharedGateway == "false" {
		config.SharedGatewayEnabled = false
	}
	
	return config
}

// IsIstioEnabled 检查是否启用了 Istio 模式
func (r *AdminerReconciler) IsIstioEnabled() bool {
	return r.useIstio
}

// GetNetworkingStatus 获取 Adminer 的网络状态
func (r *AdminerReconciler) GetNetworkingStatus(ctx context.Context, adminerName, namespace string) (*istio.NetworkingStatus, error) {
	if !r.useIstio || r.istioReconciler == nil {
		return nil, fmt.Errorf("Istio mode is not enabled")
	}
	
	// 获取 Adminer 资源
	adminer := &adminerv1.Adminer{}
	if err := r.Get(ctx, client.ObjectKey{Name: adminerName, Namespace: namespace}, adminer); err != nil {
		return nil, err
	}
	
	return r.istioReconciler.GetNetworkingStatus(ctx, adminer)
}

// EnableIstioMode 动态启用 Istio 模式
func (r *AdminerReconciler) EnableIstioMode(ctx context.Context) error {
	if r.useIstio {
		return nil // 已经启用
	}
	
	return r.SetupIstioSupport(ctx)
}

// DisableIstioMode 禁用 Istio 模式，回退到 Ingress
func (r *AdminerReconciler) DisableIstioMode() {
	r.useIstio = false
	r.istioReconciler = nil
}

// NewAdminerReconcilerWithIstio 创建支持 Istio 的 Adminer 控制器
func NewAdminerReconcilerWithIstio(
	client client.Client,
	scheme *runtime.Scheme,
	config *rest.Config,
	adminerDomain string,
	tlsEnabled bool,
	image string,
	secretName string,
	secretNamespace string,
) *AdminerReconciler {
	reconciler := &AdminerReconciler{
		Client:          client,
		Scheme:          scheme,
		Config:          config,
		adminerDomain:   adminerDomain,
		tlsEnabled:      tlsEnabled,
		image:           image,
		secretName:      secretName,
		secretNamespace: secretNamespace,
		useIstio:        false, // 默认不启用，通过 SetupIstioSupport 启用
	}
	
	return reconciler
}