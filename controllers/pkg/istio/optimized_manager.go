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
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// optimizedNetworkingManager 优化的网络管理器实现
type optimizedNetworkingManager struct {
	client            client.Client
	scheme            *runtime.Scheme
	config            *NetworkConfig
	gatewayController GatewayController
	vsController      VirtualServiceController
	domainAllocator   DomainAllocator
	certManager       CertificateManager
	domainClassifier  *DomainClassifier // 新增域名分类器
}

// NewOptimizedNetworkingManager 创建优化的网络管理器
func NewOptimizedNetworkingManager(client client.Client, config *NetworkConfig) NetworkingManager {
	return NewOptimizedNetworkingManagerWithScheme(client, nil, config)
}

// NewOptimizedNetworkingManagerWithScheme 创建优化的网络管理器（带 Scheme）
func NewOptimizedNetworkingManagerWithScheme(client client.Client, scheme *runtime.Scheme, config *NetworkConfig) NetworkingManager {
	// 验证配置合理性
	if err := ValidateNetworkConfig(config); err != nil {
		panic(fmt.Sprintf("Invalid network configuration: %v", err))
	}

	gatewayCtrl := NewGatewayController(client, config)
	vsCtrl := NewVirtualServiceController(client, config)
	domainAlloc := NewDomainAllocator(config)
	certMgr := NewCertificateManager(client, config)
	domainClassifier := NewDomainClassifier(config)

	return &optimizedNetworkingManager{
		client:            client,
		scheme:            scheme,
		config:            config,
		gatewayController: gatewayCtrl,
		vsController:      vsCtrl,
		domainAllocator:   domainAlloc,
		certManager:       certMgr,
		domainClassifier:  domainClassifier,
	}
}

func (m *optimizedNetworkingManager) CreateAppNetworking(ctx context.Context, spec *AppNetworkingSpec) error {
	// 1. 分配域名（如果没有指定）
	if len(spec.Hosts) == 0 {
		domain := m.domainAllocator.GenerateAppDomain(spec.TenantID, spec.AppName)
		spec.Hosts = []string{domain}
	}

	// 2. 验证自定义域名
	if err := m.validateCustomDomains(spec.Hosts); err != nil {
		return err
	}

	// 3. 验证自定义域名的证书配置
	if err := m.domainClassifier.ValidateCustomDomainCertificates(spec); err != nil {
		return fmt.Errorf("certificate validation failed: %w", err)
	}

	// 4. 创建/更新证书（如果启用了 TLS）
	if err := m.handleCertificates(ctx, spec); err != nil {
		return err
	}

	// 5. 智能创建 Gateway（只为自定义域名创建）
	if err := m.createOptimizedGateway(ctx, spec); err != nil {
		return err
	}

	// 6. 创建优化的 VirtualService
	if err := m.createOptimizedVirtualService(ctx, spec); err != nil {
		return err
	}

	return nil
}

func (m *optimizedNetworkingManager) UpdateAppNetworking(ctx context.Context, spec *AppNetworkingSpec) error {
	// 1. 更新证书（如果需要）
	if err := m.handleCertificates(ctx, spec); err != nil {
		return err
	}

	// 2. 智能创建或更新 Gateway
	if err := m.updateOptimizedGateway(ctx, spec); err != nil {
		return err
	}

	// 3. 更新 VirtualService
	if err := m.updateOptimizedVirtualService(ctx, spec); err != nil {
		return err
	}

	return nil
}

func (m *optimizedNetworkingManager) DeleteAppNetworking(ctx context.Context, name, namespace string) error {
	// 1. 删除 VirtualService
	vsName := fmt.Sprintf("%s-vs", name)
	if err := m.vsController.Delete(ctx, vsName, namespace); err != nil {
		return fmt.Errorf("failed to delete virtualservice: %w", err)
	}

	// 2. 删除 Gateway（如果存在且不是系统Gateway）
	gatewayName := fmt.Sprintf("%s-gateway", name)
	if exists, err := m.gatewayController.Exists(ctx, gatewayName, namespace); err != nil {
		return fmt.Errorf("failed to check gateway existence: %w", err)
	} else if exists {
		if err := m.gatewayController.Delete(ctx, gatewayName, namespace); err != nil {
			return fmt.Errorf("failed to delete gateway: %w", err)
		}
	}

	return nil
}

func (m *optimizedNetworkingManager) SuspendNetworking(ctx context.Context, namespace string) error {
	// 获取命名空间中的所有 VirtualService
	vsList := &unstructured.UnstructuredList{}
	vsList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualServiceList",
	})

	labelSelector := client.MatchingLabels{"app.kubernetes.io/managed-by": "sealos-istio"}
	if err := m.client.List(ctx, vsList, client.InNamespace(namespace), labelSelector); err != nil {
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

func (m *optimizedNetworkingManager) ResumeNetworking(ctx context.Context, namespace string) error {
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

func (m *optimizedNetworkingManager) GetNetworkingStatus(ctx context.Context, name, namespace string) (*NetworkingStatus, error) {
	status := &NetworkingStatus{
		LastUpdated: time.Now(),
	}

	// 检查 VirtualService 状态
	vsName := fmt.Sprintf("%s-vs", name)
	vs, err := m.vsController.Get(ctx, vsName, namespace)
	if err != nil {
		status.LastError = fmt.Sprintf("VirtualService error: %v", err)
		return status, nil
	}

	status.VirtualServiceReady = vs.Ready
	status.Hosts = vs.Hosts

	// 检查 Gateway 状态
	gatewayName := fmt.Sprintf("%s-gateway", name)
	if gateway, err := m.gatewayController.Get(ctx, gatewayName, namespace); err == nil {
		status.GatewayReady = gateway.Ready
		status.TLSEnabled = gateway.TLS
	} else {
		// 使用系统 Gateway，假设总是就绪
		status.GatewayReady = true
	}

	return status, nil
}

// validateCustomDomains 验证自定义域名
func (m *optimizedNetworkingManager) validateCustomDomains(hosts []string) error {
	for _, host := range hosts {
		if !m.domainClassifier.IsPublicDomain(host) {
			if err := m.domainAllocator.ValidateCustomDomain(host); err != nil {
				return fmt.Errorf("invalid custom domain %s: %w", host, err)
			}
		}
	}
	return nil
}

// handleCertificates 处理证书创建/更新
func (m *optimizedNetworkingManager) handleCertificates(ctx context.Context, spec *AppNetworkingSpec) error {
	if !m.config.TLSEnabled || spec.TLSConfig == nil {
		return nil
	}

	// 只为自定义域名创建证书，公共域名使用系统证书
	for _, host := range spec.TLSConfig.Hosts {
		if !m.domainClassifier.IsPublicDomain(host) {
			// 创建或更新证书
			if err := m.certManager.CreateOrUpdate(ctx, host, spec.Namespace); err != nil {
				return fmt.Errorf("failed to create certificate for custom domain %s: %w", host, err)
			}

			// 验证证书是否就绪（只有当证书管理器支持此方法时）
			if certMgr, ok := m.certManager.(*certificateManager); ok {
				secretName := certMgr.getSecretName(host)
				if ready, err := certMgr.IsCertificateReady(ctx, secretName, spec.Namespace); err != nil {
					return fmt.Errorf("failed to check certificate readiness for %s: %w", host, err)
				} else if !ready {
					return fmt.Errorf("certificate not ready for custom domain %s, please check cert-manager status", host)
				}
			}
		}
	}

	return nil
}

// createOptimizedGateway 智能创建Gateway
func (m *optimizedNetworkingManager) createOptimizedGateway(ctx context.Context, spec *AppNetworkingSpec) error {
	// 使用域名分类器构建优化的Gateway配置
	gatewayConfig := m.domainClassifier.BuildOptimizedGatewayConfig(spec)

	// 如果不需要创建Gateway（全部为公共域名），直接返回
	if gatewayConfig == nil {
		return nil
	}

	// 🎯 使用支持 OwnerReference 的方法创建Gateway
	if spec.OwnerObject != nil && m.scheme != nil {
		if err := m.gatewayController.CreateOrUpdateWithOwner(ctx, gatewayConfig, spec.OwnerObject, m.scheme); err != nil {
			return fmt.Errorf("failed to create optimized gateway with owner: %w", err)
		}
	} else {
		if err := m.gatewayController.Create(ctx, gatewayConfig); err != nil {
			return fmt.Errorf("failed to create optimized gateway: %w", err)
		}
	}

	return nil
}

// updateOptimizedGateway 智能更新Gateway
func (m *optimizedNetworkingManager) updateOptimizedGateway(ctx context.Context, spec *AppNetworkingSpec) error {
	gatewayConfig := m.domainClassifier.BuildOptimizedGatewayConfig(spec)

	if gatewayConfig == nil {
		// 不需要Gateway，删除如果存在
		gatewayName := fmt.Sprintf("%s-gateway", spec.Name)
		if exists, err := m.gatewayController.Exists(ctx, gatewayName, spec.Namespace); err != nil {
			return fmt.Errorf("failed to check gateway existence: %w", err)
		} else if exists {
			if err := m.gatewayController.Delete(ctx, gatewayName, spec.Namespace); err != nil {
				return fmt.Errorf("failed to delete unnecessary gateway: %w", err)
			}
		}
		return nil
	}

	// 🎯 使用支持 OwnerReference 的方法（总是使用 CreateOrUpdate）
	if spec.OwnerObject != nil && m.scheme != nil {
		if err := m.gatewayController.CreateOrUpdateWithOwner(ctx, gatewayConfig, spec.OwnerObject, m.scheme); err != nil {
			return fmt.Errorf("failed to update optimized gateway with owner: %w", err)
		}
	} else {
		// 检查Gateway是否存在
		if exists, err := m.gatewayController.Exists(ctx, gatewayConfig.Name, gatewayConfig.Namespace); err != nil {
			return fmt.Errorf("failed to check gateway existence: %w", err)
		} else if exists {
			// 更新Gateway
			if err := m.gatewayController.Update(ctx, gatewayConfig); err != nil {
				return fmt.Errorf("failed to update gateway: %w", err)
			}
		} else {
			// 创建Gateway
			if err := m.gatewayController.Create(ctx, gatewayConfig); err != nil {
				return fmt.Errorf("failed to create gateway: %w", err)
			}
		}
	}

	return nil
}

// createOptimizedVirtualService 创建优化的VirtualService
func (m *optimizedNetworkingManager) createOptimizedVirtualService(ctx context.Context, spec *AppNetworkingSpec) error {
	// 使用域名分类器构建优化的VirtualService配置
	vsConfig := m.domainClassifier.BuildOptimizedVirtualServiceConfig(spec)

	// 处理 Terminal 专用的 SecretHeader
	if spec.SecretHeader != "" {
		if vsConfig.Headers == nil {
			vsConfig.Headers = make(map[string]string)
		}
		vsConfig.Headers[spec.SecretHeader] = "1"
		vsConfig.Headers["Authorization"] = "" // 清除 Authorization 头
	}

	// 🎯 使用支持 OwnerReference 的方法
	if spec.OwnerObject != nil && m.scheme != nil {
		if err := m.vsController.CreateOrUpdateWithOwner(ctx, vsConfig, spec.OwnerObject, m.scheme); err != nil {
			return fmt.Errorf("failed to create optimized virtualservice with owner: %w", err)
		}
	} else {
		if err := m.vsController.Create(ctx, vsConfig); err != nil {
			return fmt.Errorf("failed to create optimized virtualservice: %w", err)
		}
	}

	return nil
}

// updateOptimizedVirtualService 更新优化的VirtualService
func (m *optimizedNetworkingManager) updateOptimizedVirtualService(ctx context.Context, spec *AppNetworkingSpec) error {
	vsConfig := m.domainClassifier.BuildOptimizedVirtualServiceConfig(spec)

	// 处理 Terminal 专用的 SecretHeader
	if spec.SecretHeader != "" {
		if vsConfig.Headers == nil {
			vsConfig.Headers = make(map[string]string)
		}
		vsConfig.Headers[spec.SecretHeader] = "1"
		vsConfig.Headers["Authorization"] = "" // 清除 Authorization 头
	}

	// 🎯 使用支持 OwnerReference 的方法（总是使用 CreateOrUpdate）
	if spec.OwnerObject != nil && m.scheme != nil {
		if err := m.vsController.CreateOrUpdateWithOwner(ctx, vsConfig, spec.OwnerObject, m.scheme); err != nil {
			return fmt.Errorf("failed to update optimized virtualservice with owner: %w", err)
		}
	} else {
		// 检查VirtualService是否存在
		if _, err := m.vsController.Get(ctx, vsConfig.Name, vsConfig.Namespace); err != nil {
			// 不存在，创建
			if err := m.vsController.Create(ctx, vsConfig); err != nil {
				return fmt.Errorf("failed to create virtualservice: %w", err)
			}
		} else {
			// 存在，更新
			if err := m.vsController.Update(ctx, vsConfig); err != nil {
				return fmt.Errorf("failed to update virtualservice: %w", err)
			}
		}
	}

	return nil
}

// 添加便捷方法供控制器使用

// CreateAppNetworkingWithDomainClassification 创建带域名分类的应用网络配置
func CreateAppNetworkingWithDomainClassification(
	ctx context.Context,
	client client.Client,
	config *NetworkConfig,
	spec *AppNetworkingSpec,
) error {
	manager := NewOptimizedNetworkingManager(client, config)
	return manager.CreateAppNetworking(ctx, spec)
}

// IsPublicDomain 检查域名是否为公共域名（供外部使用）
func IsPublicDomain(config *NetworkConfig, host string) bool {
	classifier := NewDomainClassifier(config)
	return classifier.IsPublicDomain(host)
}

// GetOptimalGatewayReference 获取最优Gateway引用（供外部使用）
func GetOptimalGatewayReference(config *NetworkConfig, spec *AppNetworkingSpec) string {
	classifier := NewDomainClassifier(config)
	return classifier.GetGatewayReference(spec)
}
