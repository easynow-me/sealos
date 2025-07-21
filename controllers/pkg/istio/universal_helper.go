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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UniversalIstioNetworkingHelper 通用的Istio网络配置助手
// 可用于Terminal、Resources、Devbox等控制器
type UniversalIstioNetworkingHelper struct {
	client            client.Client
	networkingManager NetworkingManager
	domainClassifier  *DomainClassifier
	config            *NetworkConfig
	appType           string // terminal, resources, devbox等
}

// NewUniversalIstioNetworkingHelper 创建通用网络配置助手
func NewUniversalIstioNetworkingHelper(
	client client.Client,
	config *NetworkConfig,
	appType string,
) *UniversalIstioNetworkingHelper {
	return NewUniversalIstioNetworkingHelperWithScheme(client, nil, config, appType)
}

// NewUniversalIstioNetworkingHelperWithScheme 创建通用网络配置助手（带 Scheme）
func NewUniversalIstioNetworkingHelperWithScheme(
	client client.Client,
	scheme *runtime.Scheme,
	config *NetworkConfig,
	appType string,
) *UniversalIstioNetworkingHelper {
	return &UniversalIstioNetworkingHelper{
		client:            client,
		networkingManager: NewOptimizedNetworkingManagerWithScheme(client, scheme, config),
		domainClassifier:  NewDomainClassifier(config),
		config:            config,
		appType:           appType,
	}
}

// AppNetworkingParams 应用网络配置参数
type AppNetworkingParams struct {
	// 基础信息
	Name      string
	Namespace string
	AppType   string
	
	// 网络配置
	Hosts              []string          // 如果为空，将自动生成
	ServiceName        string
	ServicePort        int32
	Protocol           Protocol
	
	// 可选配置
	CustomDomain       string            // 用户指定的自定义域名
	Timeout            *time.Duration
	SecretHeader       string            // Terminal专用
	CorsPolicy         *CorsPolicy
	Headers            map[string]string
	
	// 证书配置
	TLSEnabled         bool
	CustomCertSecret   string            // 自定义域名的证书Secret名称
	
	// 标签和注解
	Labels             map[string]string
	Annotations        map[string]string
	
	// 对象引用（用于设置OwnerReference）
	OwnerObject        metav1.Object
}

// CreateOrUpdateNetworking 创建或更新网络配置
func (h *UniversalIstioNetworkingHelper) CreateOrUpdateNetworking(
	ctx context.Context,
	params *AppNetworkingParams,
) error {
	// 构建网络配置规范
	spec := h.buildNetworkingSpec(params)
	
	// 验证配置
	if err := ValidateNetworkingSpec(spec); err != nil {
		return fmt.Errorf("invalid networking spec: %w", err)
	}
	
	// 检查是否已存在
	status, err := h.networkingManager.GetNetworkingStatus(ctx, params.Name, params.Namespace)
	if err != nil {
		// 不存在，创建新的网络配置
		return h.networkingManager.CreateAppNetworking(ctx, spec)
	}
	
	// 存在但可能需要更新
	if h.needsUpdate(params, status) {
		return h.networkingManager.UpdateAppNetworking(ctx, spec)
	}
	
	return nil
}

// DeleteNetworking 删除网络配置
func (h *UniversalIstioNetworkingHelper) DeleteNetworking(
	ctx context.Context,
	name, namespace string,
) error {
	return h.networkingManager.DeleteAppNetworking(ctx, name, namespace)
}

// GetNetworkingStatus 获取网络状态
func (h *UniversalIstioNetworkingHelper) GetNetworkingStatus(
	ctx context.Context,
	name, namespace string,
) (*NetworkingStatus, error) {
	return h.networkingManager.GetNetworkingStatus(ctx, name, namespace)
}

// GetOptimalDomain 获取最优域名
func (h *UniversalIstioNetworkingHelper) GetOptimalDomain(params *AppNetworkingParams) string {
	// 如果用户指定了自定义域名，直接使用
	if params.CustomDomain != "" {
		return params.CustomDomain
	}
	
	// 如果已有hosts，使用第一个
	if len(params.Hosts) > 0 {
		return params.Hosts[0]
	}
	
	// 自动生成域名
	tenantID := h.extractTenantID(params.Namespace)
	domainAllocator := NewDomainAllocator(h.config)
	
	// 根据应用类型生成合适的域名前缀
	var appName string
	switch params.AppType {
	case "terminal":
		appName = fmt.Sprintf("terminal-%s", params.Name)
	case "database", "adminer":
		appName = fmt.Sprintf("db-%s", params.Name)
	default:
		appName = params.Name
	}
	
	return domainAllocator.GenerateAppDomain(tenantID, appName)
}

// AnalyzeDomainRequirements 分析域名需求
func (h *UniversalIstioNetworkingHelper) AnalyzeDomainRequirements(params *AppNetworkingParams) *DomainAnalysis {
	domain := h.GetOptimalDomain(params)
	isPublic := h.domainClassifier.IsPublicDomain(domain)
	
	analysis := &DomainAnalysis{
		Domain:            domain,
		IsPublicDomain:    isPublic,
		NeedsGateway:      !isPublic,
		UseSystemGateway:  isPublic,
		GatewayReference:  h.getGatewayReference(params.Name, params.Namespace, isPublic),
		CertificateNeeds:  h.analyzeCertificateNeeds(domain, isPublic, params.TLSEnabled),
	}
	
	return analysis
}

// buildNetworkingSpec 构建网络配置规范
func (h *UniversalIstioNetworkingHelper) buildNetworkingSpec(params *AppNetworkingParams) *AppNetworkingSpec {
	// 确定域名
	domain := h.GetOptimalDomain(params)
	hosts := []string{domain}
	if len(params.Hosts) > 0 {
		hosts = params.Hosts
	}
	
	// 分析域名类型
	classification := h.domainClassifier.ClassifyHosts(hosts)
	
	spec := &AppNetworkingSpec{
		Name:        params.Name,
		Namespace:   params.Namespace,
		TenantID:    h.extractTenantID(params.Namespace),
		AppName:     params.AppType,
		Protocol:    params.Protocol,
		Hosts:       hosts,
		ServiceName: params.ServiceName,
		ServicePort: params.ServicePort,
		
		// 高级配置
		Timeout:      params.Timeout,
		CorsPolicy:   params.CorsPolicy,
		Headers:      params.Headers,
		SecretHeader: params.SecretHeader,
		
		// 标签和注解
		Labels:      h.buildLabels(params, classification),
		Annotations: h.buildAnnotations(params, classification),
		
		// 传递 OwnerObject
		OwnerObject: params.OwnerObject,
	}
	
	// TLS配置
	if params.TLSEnabled {
		spec.TLSConfig = h.buildTLSConfig(params, hosts, classification)
	}
	
	return spec
}

// buildLabels 构建标签
func (h *UniversalIstioNetworkingHelper) buildLabels(
	params *AppNetworkingParams,
	classification *HostClassification,
) map[string]string {
	labels := make(map[string]string)
	
	// 复制用户标签
	for k, v := range params.Labels {
		labels[k] = v
	}
	
	// 添加系统标签
	labels["app.kubernetes.io/name"] = params.Name
	labels["app.kubernetes.io/component"] = "networking"
	labels["app.kubernetes.io/managed-by"] = fmt.Sprintf("%s-controller", params.AppType)
	labels["sealos.io/app-name"] = params.AppType
	
	// 添加域名类型标签
	if classification.AllPublic {
		labels["domain-type"] = "public"
		labels["gateway-type"] = "shared"
	} else if classification.AllCustom {
		labels["domain-type"] = "custom"
		labels["gateway-type"] = "dedicated"
	} else {
		labels["domain-type"] = "mixed"
		labels["gateway-type"] = "hybrid"
	}
	
	return labels
}

// buildAnnotations 构建注解
func (h *UniversalIstioNetworkingHelper) buildAnnotations(
	params *AppNetworkingParams,
	classification *HostClassification,
) map[string]string {
	annotations := make(map[string]string)
	
	// 复制用户注解
	for k, v := range params.Annotations {
		annotations[k] = v
	}
	
	// 添加系统注解
	annotations["sealos.io/converted-from"] = "controller"
	annotations["sealos.io/optimization-version"] = "v2"
	
	// 添加优化相关注解
	if classification.AllPublic {
		annotations["sealos.io/gateway-optimization"] = "public-domain-shared-gateway"
		annotations["sealos.io/gateway-reference"] = h.config.DefaultGateway
	} else if classification.AllCustom {
		annotations["sealos.io/gateway-optimization"] = "custom-domain-dedicated-gateway"
		annotations["sealos.io/gateway-reference"] = fmt.Sprintf("%s/%s-gateway", params.Namespace, params.Name)
	} else {
		annotations["sealos.io/gateway-optimization"] = "mixed-domain-hybrid-gateway"
		annotations["sealos.io/gateway-reference"] = "multiple"
	}
	
	return annotations
}

// buildTLSConfig 构建TLS配置
func (h *UniversalIstioNetworkingHelper) buildTLSConfig(
	params *AppNetworkingParams,
	hosts []string,
	classification *HostClassification,
) *TLSConfig {
	// 只为自定义域名设置TLS配置
	if len(classification.CustomHosts) == 0 {
		return nil
	}
	
	secretName := params.CustomCertSecret
	if secretName == "" {
		secretName = fmt.Sprintf("%s-tls", params.Name)
	}
	
	return &TLSConfig{
		SecretName: secretName,
		Hosts:      classification.CustomHosts,
	}
}

// getGatewayReference 获取Gateway引用
func (h *UniversalIstioNetworkingHelper) getGatewayReference(name string, namespace string, isPublic bool) string {
	if isPublic {
		return h.config.DefaultGateway
	}
	return fmt.Sprintf("%s/%s-gateway", namespace, name)
}

// analyzeCertificateNeeds 分析证书需求
func (h *UniversalIstioNetworkingHelper) analyzeCertificateNeeds(
	domain string,
	isPublic bool,
	tlsEnabled bool,
) CertificateNeeds {
	if !tlsEnabled {
		return CertificateNeeds{Required: false}
	}
	
	if isPublic {
		return CertificateNeeds{
			Required:    false,
			UseSystem:   true,
			SystemCert:  h.config.DefaultTLSSecret,
		}
	}
	
	return CertificateNeeds{
		Required:   true,
		UseSystem:  false,
		CustomCert: fmt.Sprintf("%s-tls", strings.ReplaceAll(domain, ".", "-")),
	}
}

// needsUpdate 检查是否需要更新
func (h *UniversalIstioNetworkingHelper) needsUpdate(
	params *AppNetworkingParams,
	status *NetworkingStatus,
) bool {
	// 基本状态检查
	if !status.VirtualServiceReady || !status.GatewayReady {
		return true
	}
	
	// 域名变化检查
	expectedDomain := h.GetOptimalDomain(params)
	if len(status.Hosts) == 0 || status.Hosts[0] != expectedDomain {
		return true
	}
	
	// TLS状态检查
	if params.TLSEnabled != status.TLSEnabled {
		return true
	}
	
	return false
}

// extractTenantID 从命名空间提取租户ID
func (h *UniversalIstioNetworkingHelper) extractTenantID(namespace string) string {
	if len(namespace) > 3 && namespace[:3] == "ns-" {
		return namespace[3:]
	}
	return namespace
}

// DomainAnalysis 域名分析结果
type DomainAnalysis struct {
	Domain            string
	IsPublicDomain    bool
	NeedsGateway      bool
	UseSystemGateway  bool
	GatewayReference  string
	CertificateNeeds  CertificateNeeds
}

// CertificateNeeds 证书需求
type CertificateNeeds struct {
	Required   bool   // 是否需要证书
	UseSystem  bool   // 是否使用系统证书
	SystemCert string // 系统证书名称
	CustomCert string // 自定义证书名称
}

// GetDomainAnalysisForApp 为应用获取域名分析（便捷函数）
func GetDomainAnalysisForApp(
	config *NetworkConfig,
	appType, name, namespace string,
	customDomain string,
) *DomainAnalysis {
	helper := NewUniversalIstioNetworkingHelper(nil, config, appType)
	params := &AppNetworkingParams{
		Name:         name,
		Namespace:    namespace,
		AppType:      appType,
		CustomDomain: customDomain,
	}
	return helper.AnalyzeDomainRequirements(params)
}