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
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// networkingManager 网络管理器实现
type networkingManager struct {
	client            client.Client
	config            *NetworkConfig
	gatewayController GatewayController
	vsController      VirtualServiceController
	domainAllocator   DomainAllocator
	certManager       CertificateManager
}

// NewNetworkingManager 创建新的网络管理器
func NewNetworkingManager(client client.Client, config *NetworkConfig) NetworkingManager {
	gatewayCtrl := NewGatewayController(client, config)
	vsCtrl := NewVirtualServiceController(client, config)
	domainAlloc := NewDomainAllocator(config)
	certMgr := NewCertificateManager(client, config)

	return &networkingManager{
		client:            client,
		config:            config,
		gatewayController: gatewayCtrl,
		vsController:      vsCtrl,
		domainAllocator:   domainAlloc,
		certManager:       certMgr,
	}
}

func (m *networkingManager) CreateAppNetworking(ctx context.Context, spec *AppNetworkingSpec) error {
	// 1. 分配域名（如果没有指定）
	if len(spec.Hosts) == 0 {
		domain := m.domainAllocator.GenerateAppDomain(spec.TenantID, spec.AppName)
		spec.Hosts = []string{domain}
	}

	// 2. 验证自定义域名（如果有）
	for _, host := range spec.Hosts {
		if !strings.HasSuffix(host, m.config.BaseDomain) {
			if err := m.domainAllocator.ValidateCustomDomain(host); err != nil {
				return fmt.Errorf("invalid custom domain %s: %w", host, err)
			}
		}
	}

	// 3. 创建/更新证书（如果启用了 TLS）
	if m.config.TLSEnabled && spec.TLSConfig != nil {
		for _, host := range spec.TLSConfig.Hosts {
			if err := m.certManager.CreateOrUpdate(ctx, host, spec.Namespace); err != nil {
				return fmt.Errorf("failed to create certificate for %s: %w", host, err)
			}
		}
	}

	// 4. 创建 Gateway（如果需要）
	gatewayName := m.getGatewayName(spec)
	if !m.config.SharedGatewayEnabled || spec.TLSConfig != nil {
		gatewayConfig := &GatewayConfig{
			Name:      gatewayName,
			Namespace: spec.Namespace,
			Hosts:     spec.Hosts,
			TLSConfig: spec.TLSConfig,
			Labels:    m.buildLabels(spec),
		}

		if err := m.gatewayController.Create(ctx, gatewayConfig); err != nil {
			return fmt.Errorf("failed to create gateway: %w", err)
		}
	}

	// 5. 创建 VirtualService
	vsConfig := m.buildVirtualServiceConfig(spec, gatewayName)
	if err := m.vsController.Create(ctx, vsConfig); err != nil {
		return fmt.Errorf("failed to create virtualservice: %w", err)
	}

	return nil
}

func (m *networkingManager) UpdateAppNetworking(ctx context.Context, spec *AppNetworkingSpec) error {
	// 1. 更新证书（如果需要）
	if m.config.TLSEnabled && spec.TLSConfig != nil {
		for _, host := range spec.TLSConfig.Hosts {
			if err := m.certManager.CreateOrUpdate(ctx, host, spec.Namespace); err != nil {
				return fmt.Errorf("failed to update certificate for %s: %w", host, err)
			}
		}
	}

	// 2. 更新 Gateway（如果存在）
	gatewayName := m.getGatewayName(spec)
	if exists, err := m.gatewayController.Exists(ctx, gatewayName, spec.Namespace); err != nil {
		return fmt.Errorf("failed to check gateway existence: %w", err)
	} else if exists {
		gatewayConfig := &GatewayConfig{
			Name:      gatewayName,
			Namespace: spec.Namespace,
			Hosts:     spec.Hosts,
			TLSConfig: spec.TLSConfig,
			Labels:    m.buildLabels(spec),
		}

		if err := m.gatewayController.Update(ctx, gatewayConfig); err != nil {
			return fmt.Errorf("failed to update gateway: %w", err)
		}
	}

	// 3. 更新 VirtualService
	vsConfig := m.buildVirtualServiceConfig(spec, gatewayName)
	if err := m.vsController.Update(ctx, vsConfig); err != nil {
		return fmt.Errorf("failed to update virtualservice: %w", err)
	}

	return nil
}

func (m *networkingManager) DeleteAppNetworking(ctx context.Context, name, namespace string) error {
	// 1. 删除 VirtualService
	vsName := m.getVirtualServiceName(name)
	if err := m.vsController.Delete(ctx, vsName, namespace); err != nil {
		return fmt.Errorf("failed to delete virtualservice: %w", err)
	}

	// 2. 删除 Gateway（如果存在且不是共享的）
	gatewayName := m.getGatewayNameFromApp(name)
	if !m.config.SharedGatewayEnabled {
		if err := m.gatewayController.Delete(ctx, gatewayName, namespace); err != nil {
			return fmt.Errorf("failed to delete gateway: %w", err)
		}
	}

	return nil
}

func (m *networkingManager) SuspendNetworking(ctx context.Context, namespace string) error {
	// 获取命名空间中的所有 VirtualService
	vsList := &unstructured.UnstructuredList{}
	vsList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualServiceList",
	})

	if err := m.client.List(ctx, vsList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list virtualservices: %w", err)
	}

	// 暂停每个 VirtualService
	for _, vs := range vsList.Items {
		if err := m.vsController.Suspend(ctx, vs.GetName(), vs.GetNamespace()); err != nil {
			return fmt.Errorf("failed to suspend virtualservice %s: %w", vs.GetName(), err)
		}
	}

	return nil
}

func (m *networkingManager) ResumeNetworking(ctx context.Context, namespace string) error {
	// 获取命名空间中的所有暂停的 VirtualService
	vsList := &unstructured.UnstructuredList{}
	vsList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualServiceList",
	})

	labelSelector := client.MatchingLabels{"network.sealos.io/suspended": "true"}
	if err := m.client.List(ctx, vsList, client.InNamespace(namespace), labelSelector); err != nil {
		return fmt.Errorf("failed to list suspended virtualservices: %w", err)
	}

	// 恢复每个 VirtualService
	for _, vs := range vsList.Items {
		if err := m.vsController.Resume(ctx, vs.GetName(), vs.GetNamespace()); err != nil {
			return fmt.Errorf("failed to resume virtualservice %s: %w", vs.GetName(), err)
		}
	}

	return nil
}

func (m *networkingManager) GetNetworkingStatus(ctx context.Context, name, namespace string) (*NetworkingStatus, error) {
	status := &NetworkingStatus{
		LastUpdated: time.Now(),
	}

	// 检查 VirtualService 状态
	vsName := m.getVirtualServiceName(name)
	vs, err := m.vsController.Get(ctx, vsName, namespace)
	if err != nil {
		status.LastError = fmt.Sprintf("VirtualService error: %v", err)
		return status, nil
	}

	status.VirtualServiceReady = vs.Ready
	status.Hosts = vs.Hosts

	// 检查 Gateway 状态（如果存在）
	gatewayName := m.getGatewayNameFromApp(name)
	if gateway, err := m.gatewayController.Get(ctx, gatewayName, namespace); err == nil {
		status.GatewayReady = gateway.Ready
		status.TLSEnabled = gateway.TLS
	} else {
		// 可能使用共享 Gateway
		status.GatewayReady = true
	}

	return status, nil
}

// buildVirtualServiceConfig 构建 VirtualService 配置
func (m *networkingManager) buildVirtualServiceConfig(spec *AppNetworkingSpec, gatewayName string) *VirtualServiceConfig {
	config := &VirtualServiceConfig{
		Name:        m.getVirtualServiceName(spec.Name),
		Namespace:   spec.Namespace,
		Hosts:       spec.Hosts,
		Protocol:    spec.Protocol,
		ServiceName: spec.ServiceName,
		ServicePort: spec.ServicePort,
		Timeout:     spec.Timeout,
		Retries:     spec.Retries,
		CorsPolicy:  spec.CorsPolicy,
		Headers:     spec.Headers,
		Labels:      m.buildLabels(spec),
	}

	// 设置 Gateway
	if m.config.SharedGatewayEnabled && spec.TLSConfig == nil {
		config.Gateways = []string{m.config.DefaultGateway}
	} else {
		config.Gateways = []string{gatewayName}
	}

	// 处理 Terminal 专用的 SecretHeader
	if spec.SecretHeader != "" && spec.Headers == nil {
		config.Headers = make(map[string]string)
	}
	if spec.SecretHeader != "" {
		config.Headers[spec.SecretHeader] = "1"
		config.Headers["Authorization"] = "" // 清除 Authorization 头
	}

	return config
}

// buildLabels 构建标签
func (m *networkingManager) buildLabels(spec *AppNetworkingSpec) map[string]string {
	labels := make(map[string]string)

	// 复制用户标签
	for k, v := range spec.Labels {
		labels[k] = v
	}

	// 添加默认标签
	labels["app.kubernetes.io/name"] = spec.AppName
	labels["app.kubernetes.io/managed-by"] = "sealos-istio"
	labels["app.kubernetes.io/component"] = "networking"
	labels["sealos.io/tenant"] = spec.TenantID
	labels["sealos.io/app-name"] = spec.AppName

	return labels
}

// getGatewayName 获取 Gateway 名称
func (m *networkingManager) getGatewayName(spec *AppNetworkingSpec) string {
	if m.config.SharedGatewayEnabled && spec.TLSConfig == nil {
		return m.config.DefaultGateway
	}
	return fmt.Sprintf("%s-gateway", spec.Name)
}

// getGatewayNameFromApp 从应用名称获取 Gateway 名称
func (m *networkingManager) getGatewayNameFromApp(appName string) string {
	return fmt.Sprintf("%s-gateway", appName)
}

// getVirtualServiceName 获取 VirtualService 名称
func (m *networkingManager) getVirtualServiceName(appName string) string {
	return fmt.Sprintf("%s-vs", appName)
}
