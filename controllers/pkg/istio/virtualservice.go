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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var (
	// Istio VirtualService GVK
	virtualServiceGVK = schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualService",
	}
)

// virtualServiceController VirtualService 控制器实现
type virtualServiceController struct {
	client client.Client
	config *NetworkConfig
}

// NewVirtualServiceController 创建新的 VirtualService 控制器
func NewVirtualServiceController(client client.Client, config *NetworkConfig) VirtualServiceController {
	return &virtualServiceController{
		client: client,
		config: config,
	}
}

func (v *virtualServiceController) Create(ctx context.Context, config *VirtualServiceConfig) error {
	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(virtualServiceGVK)
	vs.SetName(config.Name)
	vs.SetNamespace(config.Namespace)

	// 设置标签
	labels := make(map[string]string)
	for k, val := range config.Labels {
		labels[k] = val
	}
	// 添加默认标签
	labels["app.kubernetes.io/managed-by"] = "sealos-istio"
	labels["app.kubernetes.io/component"] = "networking"
	vs.SetLabels(labels)

	// 构建 VirtualService spec
	spec := v.buildVirtualServiceSpec(config)
	// 确保所有值都可以深拷贝
	safeSpec := makeSafeForDeepCopy(spec)
	if err := unstructured.SetNestedMap(vs.Object, safeSpec.(map[string]interface{}), "spec"); err != nil {
		return fmt.Errorf("failed to set virtualservice spec: %w", err)
	}

	return v.client.Create(ctx, vs)
}

func (v *virtualServiceController) Update(ctx context.Context, config *VirtualServiceConfig) error {
	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(virtualServiceGVK)

	key := types.NamespacedName{
		Name:      config.Name,
		Namespace: config.Namespace,
	}

	if err := v.client.Get(ctx, key, vs); err != nil {
		return fmt.Errorf("failed to get virtualservice: %w", err)
	}

	// 更新 spec
	spec := v.buildVirtualServiceSpec(config)
	// 确保所有值都可以深拷贝
	safeSpec := makeSafeForDeepCopy(spec)
	if err := unstructured.SetNestedMap(vs.Object, safeSpec.(map[string]interface{}), "spec"); err != nil {
		return fmt.Errorf("failed to set virtualservice spec: %w", err)
	}

	// 更新标签
	labels := vs.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	for k, val := range config.Labels {
		labels[k] = val
	}
	vs.SetLabels(labels)

	return v.client.Update(ctx, vs)
}

func (v *virtualServiceController) Delete(ctx context.Context, name, namespace string) error {
	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(virtualServiceGVK)
	vs.SetName(name)
	vs.SetNamespace(namespace)

	return client.IgnoreNotFound(v.client.Delete(ctx, vs))
}

func (v *virtualServiceController) Get(ctx context.Context, name, namespace string) (*VirtualService, error) {
	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(virtualServiceGVK)

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	if err := v.client.Get(ctx, key, vs); err != nil {
		return nil, err
	}

	return v.parseVirtualService(vs)
}

func (v *virtualServiceController) Suspend(ctx context.Context, name, namespace string) error {
	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(virtualServiceGVK)

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	if err := v.client.Get(ctx, key, vs); err != nil {
		return fmt.Errorf("failed to get virtualservice: %w", err)
	}

	// 通过设置路由到不存在的服务来暂停
	suspendedRoute := []interface{}{
		map[string]interface{}{
			"match": []interface{}{
				map[string]interface{}{
					"uri": map[string]interface{}{
						"prefix": "/",
					},
				},
			},
			"fault": map[string]interface{}{
				"abort": map[string]interface{}{
					"percentage": map[string]interface{}{
						"value": int64(100),
					},
					"httpStatus": int64(503),
				},
			},
		},
	}

	if err := unstructured.SetNestedSlice(vs.Object, suspendedRoute, "spec", "http"); err != nil {
		return fmt.Errorf("failed to suspend virtualservice: %w", err)
	}

	// 添加暂停标签
	labels := vs.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["network.sealos.io/suspended"] = "true"
	vs.SetLabels(labels)

	return v.client.Update(ctx, vs)
}

func (v *virtualServiceController) Resume(ctx context.Context, name, namespace string) error {
	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(virtualServiceGVK)

	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	if err := v.client.Get(ctx, key, vs); err != nil {
		return fmt.Errorf("failed to get virtualservice: %w", err)
	}

	// 移除暂停标签
	labels := vs.GetLabels()
	if labels != nil {
		delete(labels, "network.sealos.io/suspended")
		vs.SetLabels(labels)
	}

	// 这里需要恢复原始的路由配置
	// 由于我们没有存储原始配置，这里只能返回错误或要求重新创建
	return fmt.Errorf("resume requires recreating virtualservice with original configuration")
}

// buildVirtualServiceSpec 构建 VirtualService 规范
func (v *virtualServiceController) buildVirtualServiceSpec(config *VirtualServiceConfig) map[string]interface{} {
	spec := map[string]interface{}{
		"hosts":    stringSliceToInterface(config.Hosts),
		"gateways": stringSliceToInterface(config.Gateways),
	}

	// 构建 HTTP 路由
	httpRoutes := v.buildHTTPRoutes(config)
	if len(httpRoutes) > 0 {
		spec["http"] = httpRoutes
	}

	return spec
}

// buildHTTPRoutes 构建 HTTP 路由
func (v *virtualServiceController) buildHTTPRoutes(config *VirtualServiceConfig) []interface{} {
	routes := []interface{}{}

	// 基础路由配置
	route := map[string]interface{}{
		"match": []interface{}{
			v.buildMatch(config),
		},
		"route": []interface{}{
			map[string]interface{}{
				"destination": map[string]interface{}{
					"host": config.ServiceName,
					"port": map[string]interface{}{
						"number": int64(config.ServicePort),
					},
				},
			},
		},
	}

	// 添加超时配置
	if config.Timeout != nil {
		route["timeout"] = config.Timeout.String()
	}

	// 添加重试配置
	if config.Retries != nil {
		retries := map[string]interface{}{
			"attempts": int64(config.Retries.Attempts),
		}
		if config.Retries.PerTryTimeout != nil {
			retries["perTryTimeout"] = config.Retries.PerTryTimeout.String()
		}
		route["retries"] = retries
	}

	// 添加 CORS 配置
	if config.CorsPolicy != nil {
		route["corsPolicy"] = v.buildCorsPolicy(config.CorsPolicy)
	}

	// 添加请求头配置
	if len(config.Headers) > 0 {
		headers := map[string]interface{}{
			"request": map[string]interface{}{
				"set": config.Headers,
			},
		}
		route["headers"] = headers
	}

	routes = append(routes, route)
	return routes
}

// buildMatch 构建匹配规则
func (v *virtualServiceController) buildMatch(config *VirtualServiceConfig) map[string]interface{} {
	match := map[string]interface{}{
		"uri": map[string]interface{}{
			"prefix": "/",
		},
	}

	// 根据协议添加特定匹配规则
	switch config.Protocol {
	case ProtocolWebSocket:
		match["headers"] = map[string]interface{}{
			"upgrade": map[string]interface{}{
				"exact": "websocket",
			},
		}
	case ProtocolGRPC:
		match["headers"] = map[string]interface{}{
			"content-type": map[string]interface{}{
				"prefix": "application/grpc",
			},
		}
	}

	return match
}

// buildCorsPolicy 构建 CORS 策略
func (v *virtualServiceController) buildCorsPolicy(cors *CorsPolicy) map[string]interface{} {
	policy := map[string]interface{}{}

	if len(cors.AllowOrigins) > 0 {
		origins := make([]interface{}, 0, len(cors.AllowOrigins))
		for _, origin := range cors.AllowOrigins {
			if origin == "*" {
				origins = append(origins, map[string]interface{}{
					"regex": ".*",
				})
			} else {
				origins = append(origins, map[string]interface{}{
					"exact": origin,
				})
			}
		}
		policy["allowOrigins"] = origins
	}

	if len(cors.AllowMethods) > 0 {
		policy["allowMethods"] = stringSliceToInterface(cors.AllowMethods)
	}

	if len(cors.AllowHeaders) > 0 {
		policy["allowHeaders"] = stringSliceToInterface(cors.AllowHeaders)
	}

	policy["allowCredentials"] = cors.AllowCredentials

	if cors.MaxAge != nil {
		policy["maxAge"] = cors.MaxAge.String()
	}

	return policy
}

// parseVirtualService 解析 VirtualService 资源
func (v *virtualServiceController) parseVirtualService(vs *unstructured.Unstructured) (*VirtualService, error) {
	name := vs.GetName()
	namespace := vs.GetNamespace()

	// 获取主机列表
	hosts, err := v.extractHosts(vs)
	if err != nil {
		return nil, err
	}

	// 获取 Gateway 列表
	gateways, err := v.extractGateways(vs)
	if err != nil {
		return nil, err
	}

	// 获取服务信息
	serviceName, servicePort, protocol, err := v.extractServiceInfo(vs)
	if err != nil {
		return nil, err
	}

	// 检查是否暂停
	suspended := v.isSuspended(vs)

	// 检查就绪状态
	ready := v.isVirtualServiceReady(vs)

	return &VirtualService{
		Name:        name,
		Namespace:   namespace,
		Hosts:       hosts,
		Gateways:    gateways,
		ServiceName: serviceName,
		ServicePort: servicePort,
		Protocol:    protocol,
		Suspended:   suspended,
		Ready:       ready,
	}, nil
}

// extractHosts 提取主机列表
func (v *virtualServiceController) extractHosts(vs *unstructured.Unstructured) ([]string, error) {
	hostsInterface, found, err := unstructured.NestedSlice(vs.Object, "spec", "hosts")
	if err != nil || !found {
		return nil, fmt.Errorf("failed to get hosts from virtualservice spec")
	}

	hosts := make([]string, 0, len(hostsInterface))
	for _, hostInterface := range hostsInterface {
		if host, ok := hostInterface.(string); ok {
			hosts = append(hosts, host)
		}
	}

	return hosts, nil
}

// extractGateways 提取 Gateway 列表
func (v *virtualServiceController) extractGateways(vs *unstructured.Unstructured) ([]string, error) {
	gatewaysInterface, found, err := unstructured.NestedSlice(vs.Object, "spec", "gateways")
	if err != nil || !found {
		return nil, nil
	}

	gateways := make([]string, 0, len(gatewaysInterface))
	for _, gatewayInterface := range gatewaysInterface {
		if gateway, ok := gatewayInterface.(string); ok {
			gateways = append(gateways, gateway)
		}
	}

	return gateways, nil
}

// extractServiceInfo 提取服务信息
func (v *virtualServiceController) extractServiceInfo(vs *unstructured.Unstructured) (string, int32, Protocol, error) {
	// 获取 HTTP 路由
	httpRoutes, found, err := unstructured.NestedSlice(vs.Object, "spec", "http")
	if err != nil || !found || len(httpRoutes) == 0 {
		return "", 0, ProtocolHTTP, fmt.Errorf("no http routes found")
	}

	// 获取第一个路由的目标服务
	firstRoute, ok := httpRoutes[0].(map[string]interface{})
	if !ok {
		return "", 0, ProtocolHTTP, fmt.Errorf("invalid http route format")
	}

	routeSlice, found, err := unstructured.NestedSlice(firstRoute, "route")
	if err != nil || !found || len(routeSlice) == 0 {
		return "", 0, ProtocolHTTP, fmt.Errorf("no route destinations found")
	}

	destination, ok := routeSlice[0].(map[string]interface{})
	if !ok {
		return "", 0, ProtocolHTTP, fmt.Errorf("invalid route destination format")
	}

	destMap, found, err := unstructured.NestedMap(destination, "destination")
	if err != nil || !found {
		return "", 0, ProtocolHTTP, fmt.Errorf("no destination found")
	}

	serviceName, found, err := unstructured.NestedString(destMap, "host")
	if err != nil || !found {
		return "", 0, ProtocolHTTP, fmt.Errorf("no service host found")
	}

	port, found, err := unstructured.NestedInt64(destMap, "port", "number")
	if err != nil || !found {
		return serviceName, 0, ProtocolHTTP, fmt.Errorf("no service port found")
	}

	// 检测协议
	protocol := v.detectProtocol(firstRoute)

	return serviceName, int32(port), protocol, nil
}

// detectProtocol 检测协议类型
func (v *virtualServiceController) detectProtocol(route map[string]interface{}) Protocol {
	// 检查匹配规则中的头部
	matches, found, err := unstructured.NestedSlice(route, "match")
	if err != nil || !found || len(matches) == 0 {
		return ProtocolHTTP
	}

	firstMatch, ok := matches[0].(map[string]interface{})
	if !ok {
		return ProtocolHTTP
	}

	headers, found, err := unstructured.NestedMap(firstMatch, "headers")
	if err != nil || !found {
		return ProtocolHTTP
	}

	// 检查 WebSocket
	if upgrade, found, _ := unstructured.NestedString(headers, "upgrade", "exact"); found && strings.ToLower(upgrade) == "websocket" {
		return ProtocolWebSocket
	}

	// 检查 gRPC
	if contentType, found, _ := unstructured.NestedString(headers, "content-type", "prefix"); found && strings.Contains(strings.ToLower(contentType), "grpc") {
		return ProtocolGRPC
	}

	return ProtocolHTTP
}

// isSuspended 检查是否暂停
func (v *virtualServiceController) isSuspended(vs *unstructured.Unstructured) bool {
	labels := vs.GetLabels()
	if labels == nil {
		return false
	}

	suspended, exists := labels["network.sealos.io/suspended"]
	return exists && suspended == "true"
}

// isVirtualServiceReady 检查 VirtualService 是否就绪
func (v *virtualServiceController) isVirtualServiceReady(vs *unstructured.Unstructured) bool {
	// 检查状态条件
	conditions, found, err := unstructured.NestedSlice(vs.Object, "status", "conditions")
	if err != nil || !found {
		// 如果没有状态信息，假设已就绪
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

// CreateOrUpdateWithOwner 创建或更新 VirtualService（支持设置 OwnerReference）
func (v *virtualServiceController) CreateOrUpdateWithOwner(ctx context.Context, config *VirtualServiceConfig, owner metav1.Object, scheme *runtime.Scheme) error {
	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(virtualServiceGVK)
	vs.SetName(config.Name)
	vs.SetNamespace(config.Namespace)

	result, err := controllerutil.CreateOrUpdate(ctx, v.client, vs, func() error {
		// 设置标签
		labels := vs.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		for k, val := range config.Labels {
			labels[k] = val
		}
		labels["app.kubernetes.io/managed-by"] = "sealos-istio"
		labels["app.kubernetes.io/component"] = "networking"
		vs.SetLabels(labels)

		// 构建并设置 spec
		spec := v.buildVirtualServiceSpec(config)
		// 确保所有值都可以深拷贝
		safeSpec := makeSafeForDeepCopy(spec)
		if err := unstructured.SetNestedMap(vs.Object, safeSpec.(map[string]interface{}), "spec"); err != nil {
			return fmt.Errorf("failed to set virtualservice spec: %w", err)
		}

		// 设置所有者引用
		if owner != nil && scheme != nil {
			return controllerutil.SetControllerReference(owner, vs, scheme)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create or update virtualservice: %w", err)
	}

	if result == controllerutil.OperationResultCreated {
		// VirtualService 已创建
	} else if result == controllerutil.OperationResultUpdated {
		// VirtualService 已更新
	}

	return nil
}
