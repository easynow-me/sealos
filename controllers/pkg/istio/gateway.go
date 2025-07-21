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

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var (
	// Istio Gateway GVK
	gatewayGVK = schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "Gateway",
	}
)

// gatewayController Gateway 控制器实现
type gatewayController struct {
	client client.Client
	config *NetworkConfig
}

// NewGatewayController 创建新的 Gateway 控制器
func NewGatewayController(client client.Client, config *NetworkConfig) GatewayController {
	return &gatewayController{
		client: client,
		config: config,
	}
}

func (g *gatewayController) Create(ctx context.Context, config *GatewayConfig) error {
	gateway := &unstructured.Unstructured{}
	gateway.SetGroupVersionKind(gatewayGVK)
	gateway.SetName(config.Name)
	gateway.SetNamespace(config.Namespace)

	// 设置标签
	labels := make(map[string]string)
	for k, v := range config.Labels {
		labels[k] = v
	}
	// 添加默认标签
	labels["app.kubernetes.io/managed-by"] = "sealos-istio"
	labels["app.kubernetes.io/component"] = "networking"
	gateway.SetLabels(labels)

	// 构建 Gateway spec
	spec := g.buildGatewaySpec(config)
	// 确保所有值都可以深拷贝
	safeSpec := makeSafeForDeepCopy(spec)
	if err := unstructured.SetNestedMap(gateway.Object, safeSpec.(map[string]interface{}), "spec"); err != nil {
		return fmt.Errorf("failed to set gateway spec: %w", err)
	}

	return g.client.Create(ctx, gateway)
}

func (g *gatewayController) Update(ctx context.Context, config *GatewayConfig) error {
	gateway := &unstructured.Unstructured{}
	gateway.SetGroupVersionKind(gatewayGVK)

	key := types.NamespacedName{
		Name:      config.Name,
		Namespace: config.Namespace,
	}

	if err := g.client.Get(ctx, key, gateway); err != nil {
		return fmt.Errorf("failed to get gateway: %w", err)
	}

	// 更新 spec
	spec := g.buildGatewaySpec(config)
	// 确保所有值都可以深拷贝
	safeSpec := makeSafeForDeepCopy(spec)
	if err := unstructured.SetNestedMap(gateway.Object, safeSpec.(map[string]interface{}), "spec"); err != nil {
		return fmt.Errorf("failed to set gateway spec: %w", err)
	}

	// 更新标签
	labels := gateway.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	for k, v := range config.Labels {
		labels[k] = v
	}
	gateway.SetLabels(labels)

	return g.client.Update(ctx, gateway)
}

func (g *gatewayController) Delete(ctx context.Context, name, namespace string) error {
	gateway := &unstructured.Unstructured{}
	gateway.SetGroupVersionKind(gatewayGVK)
	gateway.SetName(name)
	gateway.SetNamespace(namespace)

	return client.IgnoreNotFound(g.client.Delete(ctx, gateway))
}

func (g *gatewayController) Get(ctx context.Context, name, namespace string) (*Gateway, error) {
	gateway := &unstructured.Unstructured{}
	gateway.SetGroupVersionKind(gatewayGVK)

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	if err := g.client.Get(ctx, key, gateway); err != nil {
		return nil, err
	}

	return g.parseGateway(gateway)
}

func (g *gatewayController) Exists(ctx context.Context, name, namespace string) (bool, error) {
	_, err := g.Get(ctx, name, namespace)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// buildGatewaySpec 构建 Gateway 规范
func (g *gatewayController) buildGatewaySpec(config *GatewayConfig) map[string]interface{} {
	// 默认选择器
	selector := map[string]interface{}{
		"istio": "ingressgateway",
	}

	// 使用配置的选择器
	if g.config.GatewaySelector != nil {
		selector = make(map[string]interface{})
		for k, v := range g.config.GatewaySelector {
			selector[k] = v
		}
	}

	// 构建服务器配置
	servers := g.buildServers(config)

	return map[string]interface{}{
		"selector": selector,
		"servers":  servers,
	}
}

// buildServers 构建服务器配置
func (g *gatewayController) buildServers(config *GatewayConfig) []interface{} {
	servers := []interface{}{}

	// HTTP 服务器
	httpServer := map[string]interface{}{
		"port": map[string]interface{}{
			"number":   int64(80),
			"name":     "http",
			"protocol": "HTTP",
		},
		"hosts": stringSliceToInterface(config.Hosts),
	}
	servers = append(servers, httpServer)

	// HTTPS 服务器（如果启用了 TLS）
	if config.TLSConfig != nil && len(config.TLSConfig.Hosts) > 0 {
		httpsServer := map[string]interface{}{
			"port": map[string]interface{}{
				"number":   int64(443),
				"name":     "https",
				"protocol": "HTTPS",
			},
			"hosts": stringSliceToInterface(config.TLSConfig.Hosts),
			"tls": map[string]interface{}{
				"mode":           "SIMPLE",
				"credentialName": config.TLSConfig.SecretName,
			},
		}
		servers = append(servers, httpsServer)
	}

	return servers
}

// parseGateway 解析 Gateway 资源
func (g *gatewayController) parseGateway(gateway *unstructured.Unstructured) (*Gateway, error) {
	name := gateway.GetName()
	namespace := gateway.GetNamespace()

	// 获取主机列表
	hosts, err := g.extractHosts(gateway)
	if err != nil {
		return nil, err
	}

	// 检查 TLS 配置
	hasTLS, err := g.hasTLSConfig(gateway)
	if err != nil {
		return nil, err
	}

	// 检查就绪状态
	ready := g.isGatewayReady(gateway)

	return &Gateway{
		Name:      name,
		Namespace: namespace,
		Hosts:     hosts,
		TLS:       hasTLS,
		Ready:     ready,
	}, nil
}

// extractHosts 提取主机列表
func (g *gatewayController) extractHosts(gateway *unstructured.Unstructured) ([]string, error) {
	servers, found, err := unstructured.NestedSlice(gateway.Object, "spec", "servers")
	if err != nil || !found {
		return nil, fmt.Errorf("failed to get servers from gateway spec")
	}

	hostSet := make(map[string]bool)
	for _, serverInterface := range servers {
		server, ok := serverInterface.(map[string]interface{})
		if !ok {
			continue
		}

		hostsInterface, found, err := unstructured.NestedSlice(server, "hosts")
		if err != nil || !found {
			continue
		}

		for _, hostInterface := range hostsInterface {
			if host, ok := hostInterface.(string); ok {
				hostSet[host] = true
			}
		}
	}

	hosts := make([]string, 0, len(hostSet))
	for host := range hostSet {
		hosts = append(hosts, host)
	}

	return hosts, nil
}

// hasTLSConfig 检查是否有 TLS 配置
func (g *gatewayController) hasTLSConfig(gateway *unstructured.Unstructured) (bool, error) {
	servers, found, err := unstructured.NestedSlice(gateway.Object, "spec", "servers")
	if err != nil || !found {
		return false, nil
	}

	for _, serverInterface := range servers {
		server, ok := serverInterface.(map[string]interface{})
		if !ok {
			continue
		}

		_, found, err := unstructured.NestedMap(server, "tls")
		if err != nil {
			continue
		}
		if found {
			return true, nil
		}
	}

	return false, nil
}

// isGatewayReady 检查 Gateway 是否就绪
func (g *gatewayController) isGatewayReady(gateway *unstructured.Unstructured) bool {
	// 检查状态条件
	conditions, found, err := unstructured.NestedSlice(gateway.Object, "status", "conditions")
	if err != nil || !found {
		// 如果没有状态信息，假设已就绪（旧版本 Istio）
		return true
	}

	for _, conditionInterface := range conditions {
		condition, ok := conditionInterface.(map[string]interface{})
		if !ok {
			continue
		}

		conditionType, found, err := unstructured.NestedString(condition, "type")
		if err != nil || !found {
			continue
		}

		if conditionType == "Ready" {
			status, found, err := unstructured.NestedString(condition, "status")
			if err != nil || !found {
				continue
			}
			return status == "True"
		}
	}

	// 默认认为就绪
	return true
}

// CreateOrUpdate 创建或更新 Gateway（工具方法）
func (g *gatewayController) CreateOrUpdate(ctx context.Context, config *GatewayConfig, owner metav1.Object, scheme *runtime.Scheme) error {
	gateway := &unstructured.Unstructured{}
	gateway.SetGroupVersionKind(gatewayGVK)
	gateway.SetName(config.Name)
	gateway.SetNamespace(config.Namespace)

	result, err := controllerutil.CreateOrUpdate(ctx, g.client, gateway, func() error {
		// 设置标签
		labels := gateway.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		for k, v := range config.Labels {
			labels[k] = v
		}
		labels["app.kubernetes.io/managed-by"] = "sealos-istio"
		labels["app.kubernetes.io/component"] = "networking"
		gateway.SetLabels(labels)

		// 构建并设置 spec
		spec := g.buildGatewaySpec(config)
		// 确保所有值都可以深拷贝
		safeSpec := makeSafeForDeepCopy(spec)
		if err := unstructured.SetNestedMap(gateway.Object, safeSpec.(map[string]interface{}), "spec"); err != nil {
			return fmt.Errorf("failed to set gateway spec: %w", err)
		}

		// 设置所有者引用
		if owner != nil && scheme != nil {
			return controllerutil.SetControllerReference(owner, gateway, scheme)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create or update gateway: %w", err)
	}

	if result == controllerutil.OperationResultCreated {
		// Gateway 已创建
	} else if result == controllerutil.OperationResultUpdated {
		// Gateway 已更新
	}

	return nil
}

// stringSliceToInterface converts []string to []interface{} for unstructured objects
func stringSliceToInterface(strings []string) []interface{} {
	result := make([]interface{}, len(strings))
	for i, s := range strings {
		result[i] = s
	}
	return result
}

// makeSafeForDeepCopy recursively converts all values in a map to ensure they can be deep copied
func makeSafeForDeepCopy(obj interface{}) interface{} {
	switch v := obj.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, val := range v {
			result[k] = makeSafeForDeepCopy(val)
		}
		return result
	case map[string]string:
		// Convert map[string]string to map[string]interface{}
		result := make(map[string]interface{})
		for k, val := range v {
			result[k] = val
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, val := range v {
			result[i] = makeSafeForDeepCopy(val)
		}
		return result
	case []string:
		return stringSliceToInterface(v)
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	case float32:
		return float64(v)
	case float64:
		return v
	case bool:
		return v
	default:
		// string and other types should be fine as-is
		return v
	}
}

// CreateOrUpdateWithOwner 创建或更新 Gateway（支持设置 OwnerReference）
func (g *gatewayController) CreateOrUpdateWithOwner(ctx context.Context, config *GatewayConfig, owner metav1.Object, scheme *runtime.Scheme) error {
	gateway := &unstructured.Unstructured{}
	gateway.SetGroupVersionKind(gatewayGVK)
	gateway.SetName(config.Name)
	gateway.SetNamespace(config.Namespace)

	result, err := controllerutil.CreateOrUpdate(ctx, g.client, gateway, func() error {
		// 设置标签
		labels := gateway.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		for k, val := range config.Labels {
			labels[k] = val
		}
		labels["app.kubernetes.io/managed-by"] = "sealos-istio"
		labels["app.kubernetes.io/component"] = "networking"
		gateway.SetLabels(labels)

		// 构建并设置 spec
		spec := g.buildGatewaySpec(config)
		// 确保所有值都可以深拷贝
		safeSpec := makeSafeForDeepCopy(spec)
		if err := unstructured.SetNestedMap(gateway.Object, safeSpec.(map[string]interface{}), "spec"); err != nil {
			return fmt.Errorf("failed to set gateway spec: %w", err)
		}

		// 设置所有者引用
		if owner != nil && scheme != nil {
			if err := controllerutil.SetControllerReference(owner, gateway, scheme); err != nil {
				return fmt.Errorf("failed to set owner reference: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create or update gateway: %w", err)
	}

	// 记录操作结果（注：可以在需要时添加日志）
	_ = result // 避免未使用变量警告

	return nil
}
