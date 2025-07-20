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
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// DefaultNetworkConfig 返回默认的网络配置
func DefaultNetworkConfig() *NetworkConfig {
	return &NetworkConfig{
		BaseDomain:       "cloud.sealos.io",
		DefaultGateway:   "istio-system/cluster-gateway",
		DefaultTLSSecret: "wildcard-cert",
		TLSEnabled:       true,
		DomainTemplates: map[string]string{
			"app":      "{{.AppName}}-{{.Hash}}.{{.TenantID}}.{{.BaseDomain}}",
			"terminal": "terminal-{{.Hash}}.{{.TenantID}}.{{.BaseDomain}}",
			"database": "db-{{.Hash}}.{{.TenantID}}.{{.BaseDomain}}",
		},
		ReservedDomains: []string{
			"api", "www", "mail", "ftp", "admin", "root", "system",
			"console", "dashboard", "management", "cluster", "istio",
			"kubernetes", "k8s", "sealos", "cloud",
		},
		CertManager: "cert-manager",
		AutoTLS:     true,
		GatewaySelector: map[string]string{
			"istio": "ingressgateway",
		},
		SharedGatewayEnabled: true,
	}
}

// BuildTerminalNetworkingSpec 构建 Terminal 的网络配置规范
func BuildTerminalNetworkingSpec(name, namespace, tenantID, serviceName, secretHeader string, config *NetworkConfig) *AppNetworkingSpec {
	domainAlloc := NewDomainAllocator(config)
	domain := domainAlloc.(*domainAllocator).GetDomainForTerminal(tenantID, name)

	spec := &AppNetworkingSpec{
		Name:         name,
		Namespace:    namespace,
		TenantID:     tenantID,
		AppName:      "terminal",
		Protocol:     ProtocolWebSocket,
		Hosts:        []string{domain},
		ServiceName:  serviceName,
		ServicePort:  8080,
		SecretHeader: secretHeader,
		Timeout:      &[]time.Duration{86400 * time.Second}[0], // 24小时
		CorsPolicy: &CorsPolicy{
			AllowOrigins:     []string{fmt.Sprintf("https://%s", config.BaseDomain), fmt.Sprintf("https://*.%s", config.BaseDomain)},
			AllowMethods:     []string{"PUT", "GET", "POST", "PATCH", "OPTIONS"},
			AllowHeaders:     []string{"content-type", "authorization"},
			AllowCredentials: false,
		},
		Labels: map[string]string{
			"app.kubernetes.io/name":      name,
			"app.kubernetes.io/component": "terminal",
			"sealos.io/app-name":          "terminal",
		},
	}

	// 如果启用 TLS，添加 TLS 配置
	if config.TLSEnabled {
		spec.TLSConfig = &TLSConfig{
			SecretName: config.DefaultTLSSecret,
			Hosts:      []string{domain},
		}
	}

	return spec
}

// BuildAdminerNetworkingSpec 构建 DB Adminer 的网络配置规范
func BuildAdminerNetworkingSpec(name, namespace, tenantID, serviceName string, tlsEnabled bool, config *NetworkConfig) *AppNetworkingSpec {
	domainAlloc := NewDomainAllocator(config)
	domain := domainAlloc.(*domainAllocator).GetDomainForDatabase(tenantID, name)

	// 构建 CORS origins
	corsOrigins := []string{}
	if tlsEnabled {
		corsOrigins = []string{fmt.Sprintf("https://%s", config.BaseDomain), fmt.Sprintf("https://*.%s", config.BaseDomain)}
	} else {
		corsOrigins = []string{fmt.Sprintf("http://%s", config.BaseDomain), fmt.Sprintf("http://*.%s", config.BaseDomain)}
	}

	spec := &AppNetworkingSpec{
		Name:        name,
		Namespace:   namespace,
		TenantID:    tenantID,
		AppName:     "adminer",
		Protocol:    ProtocolHTTP,
		Hosts:       []string{domain},
		ServiceName: serviceName,
		ServicePort: 8080,
		Timeout:     &[]time.Duration{86400 * time.Second}[0], // 24小时
		CorsPolicy: &CorsPolicy{
			AllowOrigins:     corsOrigins,
			AllowMethods:     []string{"PUT", "GET", "POST", "PATCH", "OPTIONS"},
			AllowHeaders:     []string{"content-type", "authorization"},
			AllowCredentials: false,
		},
		Labels: map[string]string{
			"app.kubernetes.io/name":      name,
			"app.kubernetes.io/component": "database",
			"sealos.io/app-name":          "adminer",
		},
	}

	// 如果启用 TLS，添加 TLS 配置
	if tlsEnabled {
		spec.TLSConfig = &TLSConfig{
			SecretName: config.DefaultTLSSecret,
			Hosts:      []string{domain},
		}
	}

	return spec
}

// ConvertIngressAnnotationsToIstio 将 Ingress annotations 转换为 Istio 配置
func ConvertIngressAnnotationsToIstio(annotations map[string]string) (*AppNetworkingSpec, error) {
	spec := &AppNetworkingSpec{
		Protocol: ProtocolHTTP,
	}

	// 解析协议
	if backendProtocol, exists := annotations["nginx.ingress.kubernetes.io/backend-protocol"]; exists {
		switch backendProtocol {
		case "GRPC", "grpc":
			spec.Protocol = ProtocolGRPC
		case "HTTPS", "https":
			spec.Protocol = ProtocolHTTPS
		}
	}

	// 检查 WebSocket
	if _, exists := annotations["nginx.ingress.kubernetes.io/websocket-services"]; exists {
		spec.Protocol = ProtocolWebSocket
	}

	// 解析超时
	if timeout, exists := annotations["nginx.ingress.kubernetes.io/proxy-read-timeout"]; exists {
		if duration, err := time.ParseDuration(timeout + "s"); err == nil {
			spec.Timeout = &duration
		}
	}

	// 解析 CORS
	if corsEnabled, exists := annotations["nginx.ingress.kubernetes.io/enable-cors"]; exists && corsEnabled == "true" {
		spec.CorsPolicy = &CorsPolicy{
			AllowCredentials: false,
		}

		if allowOrigin, exists := annotations["nginx.ingress.kubernetes.io/cors-allow-origin"]; exists {
			spec.CorsPolicy.AllowOrigins = []string{allowOrigin}
		}

		if allowMethods, exists := annotations["nginx.ingress.kubernetes.io/cors-allow-methods"]; exists {
			// 简化处理，实际应该解析逗号分隔的字符串
			spec.CorsPolicy.AllowMethods = []string{allowMethods}
		}

		if allowHeaders, exists := annotations["nginx.ingress.kubernetes.io/cors-allow-headers"]; exists {
			// 简化处理，实际应该解析逗号分隔的字符串
			spec.CorsPolicy.AllowHeaders = []string{allowHeaders}
		}

		if allowCredentials, exists := annotations["nginx.ingress.kubernetes.io/cors-allow-credentials"]; exists {
			spec.CorsPolicy.AllowCredentials = allowCredentials == "true"
		}
	}

	return spec, nil
}

// ProtocolRequiresLongTimeout 检查协议是否需要长超时
func ProtocolRequiresLongTimeout(protocol Protocol) bool {
	return protocol == ProtocolWebSocket || protocol == ProtocolGRPC
}

// GetDefaultTimeout 获取协议的默认超时时间
func GetDefaultTimeout(protocol Protocol) time.Duration {
	switch protocol {
	case ProtocolWebSocket:
		return 86400 * time.Second // 24小时
	case ProtocolGRPC:
		return 0 // gRPC 流连接无超时
	default:
		return 30 * time.Second // 默认30秒
	}
}

// ValidateNetworkingSpec 验证网络配置规范
func ValidateNetworkingSpec(spec *AppNetworkingSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("name is required")
	}

	if spec.Namespace == "" {
		return fmt.Errorf("namespace is required")
	}

	if spec.ServiceName == "" {
		return fmt.Errorf("serviceName is required")
	}

	if spec.ServicePort <= 0 {
		return fmt.Errorf("servicePort must be positive")
	}

	if len(spec.Hosts) == 0 {
		return fmt.Errorf("at least one host is required")
	}

	return nil
}

// MergeLabels 合并标签
func MergeLabels(base, additional map[string]string) map[string]string {
	result := make(map[string]string)

	// 复制基础标签
	for k, v := range base {
		result[k] = v
	}

	// 添加额外标签
	for k, v := range additional {
		result[k] = v
	}

	return result
}

// IsIstioEnabled 检查集群是否启用了 Istio
func IsIstioEnabled(client Client) (bool, error) {
	// 检查 Istio CRD 是否存在
	gatewayList := &unstructured.UnstructuredList{}
	gatewayList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "GatewayList",
	})

	if err := client.List(context.Background(), gatewayList); err != nil {
		return false, nil // 如果 CRD 不存在，认为 Istio 未启用
	}

	return true, nil
}
