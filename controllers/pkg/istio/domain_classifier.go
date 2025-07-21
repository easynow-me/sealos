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
	"fmt"
	"strings"
)

// DomainClassifier 域名分类器
type DomainClassifier struct {
	publicDomains  []string
	systemGateway  string
	systemNamespace string
}

// NewDomainClassifier 创建域名分类器
func NewDomainClassifier(config *NetworkConfig) *DomainClassifier {
	// 构建公共域名列表（完全基于配置，无硬编码）
	publicDomains := []string{}
	
	// 1. 添加基础域名及其子域名模式
	if config.BaseDomain != "" {
		publicDomains = append(publicDomains, 
			config.BaseDomain,                    // 精确匹配
			"."+config.BaseDomain,               // 子域名匹配
		)
	}
	
	// 2. 添加明确配置的公共域名（精确匹配）
	publicDomains = append(publicDomains, config.PublicDomains...)
	
	// 3. 添加公共域名模式（支持通配符）
	publicDomains = append(publicDomains, config.PublicDomainPatterns...)
	
	// 4. 添加保留域名（全部视为公共域名，无过滤）
	publicDomains = append(publicDomains, config.ReservedDomains...)
	
	return &DomainClassifier{
		publicDomains:   deduplicateSlice(publicDomains),
		systemGateway:   getSystemGateway(config),
		systemNamespace: getSystemNamespace(config),
	}
}

// IsPublicDomain 判断域名是否为公共域名
func (dc *DomainClassifier) IsPublicDomain(host string) bool {
	if host == "" {
		return false
	}
	
	host = strings.ToLower(host)
	
	for _, domainPattern := range dc.publicDomains {
		if dc.matchesDomainPattern(host, domainPattern) {
			return true
		}
	}
	
	return false
}

// matchesDomainPattern 检查域名是否匹配域名模式（支持通配符）
func (dc *DomainClassifier) matchesDomainPattern(host, pattern string) bool {
	pattern = strings.ToLower(pattern)
	host = strings.ToLower(host)
	
	// 1. 精确匹配
	if host == pattern {
		return true
	}
	
	// 2. 通配符匹配（*.example.com）
	if strings.HasPrefix(pattern, "*.") {
		baseDomain := pattern[2:] // 移除 "*."
		return host == baseDomain || strings.HasSuffix(host, "."+baseDomain)
	}
	
	// 3. 子域名前缀匹配（.example.com）
	if strings.HasPrefix(pattern, ".") {
		return strings.HasSuffix(host, pattern)
	}
	
	// 4. 后缀匹配（兼容原有逻辑）
	if strings.HasSuffix(host, pattern) {
		return true
	}
	
	return false
}

// ClassifyHosts 对主机列表进行分类
func (dc *DomainClassifier) ClassifyHosts(hosts []string) *HostClassification {
	classification := &HostClassification{
		PublicHosts:  []string{},
		CustomHosts:  []string{},
		AllPublic:    true,
		AllCustom:    true,
		Mixed:        false,
	}
	
	for _, host := range hosts {
		if dc.IsPublicDomain(host) {
			classification.PublicHosts = append(classification.PublicHosts, host)
			classification.AllCustom = false
		} else {
			classification.CustomHosts = append(classification.CustomHosts, host)
			classification.AllPublic = false
		}
	}
	
	classification.Mixed = !classification.AllPublic && !classification.AllCustom
	
	return classification
}

// ShouldCreateGateway 判断是否需要创建Gateway
func (dc *DomainClassifier) ShouldCreateGateway(spec *AppNetworkingSpec) bool {
	// 用户明确指定了TLS配置且使用自定义域名
	if spec.TLSConfig != nil {
		classification := dc.ClassifyHosts(spec.TLSConfig.Hosts)
		if len(classification.CustomHosts) > 0 {
			return true
		}
	}
	
	// 分类主机
	classification := dc.ClassifyHosts(spec.Hosts)
	
	// 如果有自定义域名，需要创建Gateway
	if len(classification.CustomHosts) > 0 {
		return true
	}
	
	// 如果都是公共域名，不需要创建Gateway
	return false
}

// GetGatewayReference 获取Gateway引用
func (dc *DomainClassifier) GetGatewayReference(spec *AppNetworkingSpec) string {
	// 检查是否需要创建自定义Gateway
	if dc.ShouldCreateGateway(spec) {
		return fmt.Sprintf("%s/%s-gateway", spec.Namespace, spec.Name)
	}
	
	// 使用系统Gateway
	return dc.systemGateway
}

// GetGatewayReferencesForHosts 为不同类型的主机获取Gateway引用
func (dc *DomainClassifier) GetGatewayReferencesForHosts(hosts []string, appName string) []string {
	classification := dc.ClassifyHosts(hosts)
	gateways := []string{}
	
	// 公共域名使用系统Gateway
	if len(classification.PublicHosts) > 0 {
		gateways = append(gateways, dc.systemGateway)
	}
	
	// 自定义域名使用应用Gateway
	if len(classification.CustomHosts) > 0 {
		gateways = append(gateways, fmt.Sprintf("%s-gateway", appName))
	}
	
	return gateways
}

// BuildOptimizedGatewayConfig 构建优化的Gateway配置
func (dc *DomainClassifier) BuildOptimizedGatewayConfig(spec *AppNetworkingSpec) *GatewayConfig {
	classification := dc.ClassifyHosts(spec.Hosts)
	
	// 只为自定义域名创建Gateway
	if len(classification.CustomHosts) == 0 {
		return nil
	}
	
	// 创建只包含自定义域名的Gateway配置
	config := &GatewayConfig{
		Name:      fmt.Sprintf("%s-gateway", spec.Name),
		Namespace: spec.Namespace,
		Hosts:     classification.CustomHosts, // 只包含自定义域名
		Labels:    buildGatewayLabels(spec, "custom-domain"),
	}
	
	// TLS配置（只为自定义域名）
	if spec.TLSConfig != nil {
		customTLSHosts := []string{}
		for _, host := range spec.TLSConfig.Hosts {
			if !dc.IsPublicDomain(host) {
				customTLSHosts = append(customTLSHosts, host)
			}
		}
		
		if len(customTLSHosts) > 0 {
			config.TLSConfig = &TLSConfig{
				SecretName: spec.TLSConfig.SecretName,
				Hosts:      customTLSHosts,
			}
		}
	}
	
	return config
}

// BuildOptimizedVirtualServiceConfig 构建优化的VirtualService配置
func (dc *DomainClassifier) BuildOptimizedVirtualServiceConfig(spec *AppNetworkingSpec) *VirtualServiceConfig {
	classification := dc.ClassifyHosts(spec.Hosts)
	
	// 智能选择Gateway
	gateways := []string{}
	if len(classification.PublicHosts) > 0 {
		gateways = append(gateways, dc.systemGateway)
	}
	if len(classification.CustomHosts) > 0 {
		gateways = append(gateways, fmt.Sprintf("%s/%s-gateway", spec.Namespace, spec.Name))
	}
	
	config := &VirtualServiceConfig{
		Name:        fmt.Sprintf("%s-vs", spec.Name),
		Namespace:   spec.Namespace,
		Hosts:       spec.Hosts,
		Gateways:    gateways, // 智能选择的Gateway列表
		Protocol:    spec.Protocol,
		ServiceName: spec.ServiceName,
		ServicePort: spec.ServicePort,
		Timeout:     spec.Timeout,
		Retries:     spec.Retries,
		CorsPolicy:  spec.CorsPolicy,
		Headers:     spec.Headers,
		Labels:      buildVirtualServiceLabels(spec, classification),
	}
	
	return config
}

// HostClassification 主机分类结果
type HostClassification struct {
	PublicHosts []string // 公共域名列表
	CustomHosts []string // 自定义域名列表
	AllPublic   bool     // 是否全部为公共域名
	AllCustom   bool     // 是否全部为自定义域名
	Mixed       bool     // 是否混合类型
}

// getSystemGateway 获取系统Gateway名称
func getSystemGateway(config *NetworkConfig) string {
	if config.DefaultGateway != "" {
		return config.DefaultGateway
	}
	return "istio-system/sealos-gateway"
}

// getSystemNamespace 获取系统命名空间
func getSystemNamespace(config *NetworkConfig) string {
	gateway := getSystemGateway(config)
	if strings.Contains(gateway, "/") {
		parts := strings.Split(gateway, "/")
		return parts[0]
	}
	return "istio-system"
}

// buildGatewayLabels 构建Gateway标签
func buildGatewayLabels(spec *AppNetworkingSpec, gatewayType string) map[string]string {
	labels := make(map[string]string)
	
	// 复制用户标签
	for k, v := range spec.Labels {
		labels[k] = v
	}
	
	// 添加系统标签
	labels["app.kubernetes.io/name"] = spec.Name
	labels["app.kubernetes.io/component"] = "networking"
	labels["app.kubernetes.io/managed-by"] = "sealos-istio"
	labels["sealos.io/app-name"] = spec.AppName
	labels["gateway-type"] = gatewayType
	
	return labels
}

// buildVirtualServiceLabels 构建VirtualService标签
func buildVirtualServiceLabels(spec *AppNetworkingSpec, classification *HostClassification) map[string]string {
	labels := make(map[string]string)
	
	// 复制用户标签
	for k, v := range spec.Labels {
		labels[k] = v
	}
	
	// 添加系统标签
	labels["app.kubernetes.io/name"] = spec.Name
	labels["app.kubernetes.io/component"] = "networking"
	labels["app.kubernetes.io/managed-by"] = "sealos-istio"
	labels["sealos.io/app-name"] = spec.AppName
	
	// 添加域名类型标签
	if classification.AllPublic {
		labels["domain-type"] = "public"
		labels["network.sealos.io/gateway-type"] = "shared"
	} else if classification.AllCustom {
		labels["domain-type"] = "custom"
		labels["network.sealos.io/gateway-type"] = "dedicated"
	} else {
		labels["domain-type"] = "mixed"
		labels["network.sealos.io/gateway-type"] = "mixed"
	}
	
	return labels
}

// ValidateCustomDomainCertificates 验证自定义域名的证书配置
func (dc *DomainClassifier) ValidateCustomDomainCertificates(spec *AppNetworkingSpec) error {
	// 首先检查是否有自定义域名
	classification := dc.ClassifyHosts(spec.Hosts)
	
	// 如果没有自定义域名，不需要验证
	if len(classification.CustomHosts) == 0 {
		return nil
	}
	
	// 有自定义域名时，必须有TLS配置
	if spec.TLSConfig == nil {
		return fmt.Errorf("TLS configuration is required for custom domains: %v", classification.CustomHosts)
	}
	
	// 检查TLS secret name
	if spec.TLSConfig.SecretName == "" {
		return fmt.Errorf("TLS secret name is required for custom domains: %v", classification.CustomHosts)
	}
	
	// 验证自定义域名的证书名称规范
	if !isValidSecretName(spec.TLSConfig.SecretName) {
		return fmt.Errorf("invalid certificate secret name: %s", spec.TLSConfig.SecretName)
	}
	
	// 验证TLS hosts必须覆盖所有自定义域名
	tlsClassification := dc.ClassifyHosts(spec.TLSConfig.Hosts)
	missingHosts := []string{}
	for _, customHost := range classification.CustomHosts {
		found := false
		for _, tlsHost := range tlsClassification.CustomHosts {
			if customHost == tlsHost {
				found = true
				break
			}
		}
		if !found {
			missingHosts = append(missingHosts, customHost)
		}
	}
	
	if len(missingHosts) > 0 {
		return fmt.Errorf("TLS hosts must cover all custom domains, missing: %v", missingHosts)
	}
	
	return nil
}

// GenerateCertificateSecretName 为自定义域名生成证书Secret名称
func (dc *DomainClassifier) GenerateCertificateSecretName(host string) string {
	if dc.IsPublicDomain(host) {
		return "" // 公共域名不需要自定义证书
	}
	
	// 为自定义域名生成证书名称
	secretName := strings.ReplaceAll(host, "*", "wildcard")
	secretName = strings.ReplaceAll(secretName, ".", "-")
	return fmt.Sprintf("%s-tls", secretName)
}

// isValidSecretName 验证Secret名称是否有效
func isValidSecretName(name string) bool {
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	
	// 简单的DNS标签验证
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') || 
			 (char >= '0' && char <= '9') || 
			 char == '-') {
			return false
		}
	}
	
	return !strings.HasPrefix(name, "-") && !strings.HasSuffix(name, "-")
}

// deduplicateSlice 去重字符串切片
func deduplicateSlice(slice []string) []string {
	if len(slice) == 0 {
		return slice
	}
	
	seen := make(map[string]bool)
	result := []string{}
	
	for _, item := range slice {
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	
	return result
}

// ValidateNetworkConfig 验证网络配置的合理性
func ValidateNetworkConfig(config *NetworkConfig) error {
	if config == nil {
		return fmt.Errorf("network config cannot be nil")
	}
	
	// 验证基础配置
	if config.DefaultGateway == "" {
		return fmt.Errorf("default gateway must be specified")
	}
	
	// 验证公共域名配置
	if len(config.PublicDomains) == 0 && 
	   len(config.PublicDomainPatterns) == 0 && 
	   config.BaseDomain == "" {
		return fmt.Errorf("at least one public domain configuration is required (PublicDomains, PublicDomainPatterns, or BaseDomain)")
	}
	
	// 验证公共域名格式
	for _, domain := range config.PublicDomains {
		if err := validateDomainFormat(domain); err != nil {
			return fmt.Errorf("invalid public domain '%s': %w", domain, err)
		}
	}
	
	// 验证公共域名模式格式
	for _, pattern := range config.PublicDomainPatterns {
		if err := validateDomainPatternFormat(pattern); err != nil {
			return fmt.Errorf("invalid public domain pattern '%s': %w", pattern, err)
		}
	}
	
	// 验证基础域名格式
	if config.BaseDomain != "" {
		if err := validateDomainFormat(config.BaseDomain); err != nil {
			return fmt.Errorf("invalid base domain '%s': %w", config.BaseDomain, err)
		}
	}
	
	return nil
}

// validateDomainFormat 验证域名格式
func validateDomainFormat(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain cannot be empty")
	}
	
	if len(domain) > 253 {
		return fmt.Errorf("domain length cannot exceed 253 characters")
	}
	
	// 检查是否包含无效字符
	if strings.Contains(domain, " ") {
		return fmt.Errorf("domain cannot contain spaces")
	}
	
	// 基本的域名格式检查
	if strings.HasPrefix(domain, ".") && len(domain) == 1 {
		return fmt.Errorf("invalid domain format")
	}
	
	return nil
}

// validateDomainPatternFormat 验证域名模式格式（支持通配符）
func validateDomainPatternFormat(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("domain pattern cannot be empty")
	}
	
	// 基础格式验证
	if err := validateDomainFormat(pattern); err != nil {
		// 对于通配符模式，忽略某些基础验证错误
		if !strings.HasPrefix(pattern, "*.") && !strings.HasPrefix(pattern, ".") {
			return err
		}
	}
	
	// 验证通配符使用
	if strings.Count(pattern, "*") > 1 {
		return fmt.Errorf("only one wildcard (*) is allowed per pattern")
	}
	
	if strings.Contains(pattern, "*") && !strings.HasPrefix(pattern, "*.") {
		return fmt.Errorf("wildcard (*) can only be used as subdomain prefix (*.example.com)")
	}
	
	return nil
}

// GetPublicDomainSummary 获取公共域名配置摘要（用于调试和日志）
func GetPublicDomainSummary(config *NetworkConfig) string {
	if config == nil {
		return "config is nil"
	}
	
	summary := []string{}
	
	if config.BaseDomain != "" {
		summary = append(summary, fmt.Sprintf("BaseDomain: %s", config.BaseDomain))
	}
	
	if len(config.PublicDomains) > 0 {
		summary = append(summary, fmt.Sprintf("PublicDomains: %v", config.PublicDomains))
	}
	
	if len(config.PublicDomainPatterns) > 0 {
		summary = append(summary, fmt.Sprintf("PublicDomainPatterns: %v", config.PublicDomainPatterns))
	}
	
	if len(config.ReservedDomains) > 0 {
		summary = append(summary, fmt.Sprintf("ReservedDomains: %v", config.ReservedDomains))
	}
	
	if len(summary) == 0 {
		return "no public domains configured"
	}
	
	return strings.Join(summary, "; ")
}