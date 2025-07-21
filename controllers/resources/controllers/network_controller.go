/*
Copyright 2025.

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
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/labring/sealos/controllers/pkg/istio"
)

// NetworkReconciler reconciles Namespace, Ingress, VirtualService and Service objects to manage network traffic
type NetworkReconciler struct {
	Client           client.Client
	Log              logr.Logger
	networkingManager istio.NetworkingManager
	useIstio         bool
}

const (
	NetworkStatusAnnoKey   = "network.sealos.io/status"
	NetworkSuspend         = "Suspend"
	NetworkResume          = "Resume"
	NetworkResumeCompleted = "ResumeCompleted"
	NodePortLabelKey       = "network.sealos.io/original-nodeport"
	IngressClassKey        = "kubernetes.io/ingress.class"

	Disable = "disable"
	True    = "true"
)

// retryUpdateOnConflict retries the update operation when there's a resource version conflict
func retryUpdateOnConflict(ctx context.Context, c client.Client, obj client.Object, updateFunc func()) error {
	return wait.PollImmediate(100*time.Millisecond, 3*time.Second, func() (bool, error) {
		updateFunc()
		err := c.Update(ctx, obj)
		if err != nil {
			if errors.IsConflict(err) {
				// Resource version conflict, need to get the latest version and retry
				key := client.ObjectKeyFromObject(obj)
				if getErr := c.Get(ctx, key, obj); getErr != nil {
					return false, getErr
				}
				return false, nil // Retry with updated object
			}
			return false, err // Other errors should not be retried
		}
		return true, nil // Success
	})
}

//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.istio.io,resources=gateways,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=gateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.istio.io,resources=destinationrules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=destinationrules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;update;patch

func (r *NetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("Namespace", req.Namespace, "Name", req.NamespacedName)

	logger.Info("Reconciling Network")
	// Fetch the namespace
	ns := corev1.Namespace{}
	keyObj := client.ObjectKey{Name: req.Namespace}
	if req.Namespace == "" && req.Name != "" {
		keyObj = client.ObjectKey{Name: req.Name}
	}
	if err := r.Client.Get(ctx, keyObj, &ns); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip if namespace is terminating
	if ns.Status.Phase == corev1.NamespaceTerminating {
		logger.Info("namespace is terminating")
		return ctrl.Result{}, nil
	}

	// Check network status annotation
	networkStatus, ok := ns.Annotations[NetworkStatusAnnoKey]
	if !ok {
		logger.Info("no network status annotation found")
		return ctrl.Result{}, nil
	}

	logger.Info("network status", "status", networkStatus)

	// Skip completed state
	if networkStatus == NetworkResumeCompleted {
		logger.Info("skipping completed network status")
		return ctrl.Result{}, nil
	}

	switch networkStatus {
	case NetworkSuspend:
		// If NamespacedName.Namespace is empty, then req is the namespace itself, and req.namespacedname.name is the Name of the namespace
		if req.NamespacedName.Namespace == "" {
			// Handle namespace suspension
			if err := r.suspendNetworkResources(ctx, req.Name); err != nil {
				logger.Error(err, "failed to suspend network resources")
				return ctrl.Result{}, err
			}
			break
		}
		if err := r.handleResource(ctx, req.NamespacedName, ns); err != nil {
			logger.Error(err, "failed to handle resource")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	case NetworkResume:
		namespace := req.Namespace
		if req.Namespace == "" {
			namespace = req.Name
		}
		// Handle namespace resumption
		if err := r.resumeNetworkResources(ctx, namespace); err != nil {
			logger.Error(err, "failed to resume network resources")
			return ctrl.Result{}, err
		}
		// Update namespace status
		if ns.Annotations == nil {
			ns.Annotations = make(map[string]string)
		}
		if err := retryUpdateOnConflict(ctx, r.Client, &ns, func() {
			ns.Annotations[NetworkStatusAnnoKey] = NetworkResumeCompleted
		}); err != nil {
			logger.Error(err, "failed to update namespace network status to ResumeCompleted")
			return ctrl.Result{}, err
		}
	default:
		logger.Error(fmt.Errorf("unknown network status"), "", "status", networkStatus)
	}

	return ctrl.Result{}, nil
}

func (r *NetworkReconciler) handleResource(ctx context.Context, key client.ObjectKey, ns corev1.Namespace) error {
	// Only process resources in suspended namespaces
	networkStatus, ok := ns.Annotations[NetworkStatusAnnoKey]
	if !ok || networkStatus != NetworkSuspend {
		return nil
	}

	// 根据配置决定处理 Istio 还是 Ingress 资源
	if r.useIstio {
		return r.handleIstioResource(ctx, key)
	}
	return r.handleIngressResource(ctx, key)
}

func (r *NetworkReconciler) handleIngressResource(ctx context.Context, key client.ObjectKey) error {
	// Try fetching as Ingress
	ingress := networkingv1.Ingress{}
	if err := r.Client.Get(ctx, key, &ingress); err == nil {
		if ingress.Annotations == nil {
			ingress.Annotations = make(map[string]string)
		}
		if ingress.Annotations[IngressClassKey] != Disable {
			if err := retryUpdateOnConflict(ctx, r.Client, &ingress, func() {
				ingress.Annotations[IngressClassKey] = Disable
			}); err != nil {
				return fmt.Errorf("failed to suspend ingress %s: %w", key.Name, err)
			}
			r.Log.V(1).Info("Suspended ingress", "name", key.Name)
		}
		return nil
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get ingress %s: %w", key.Name, err)
	}

	// Try fetching as Service
	svc := corev1.Service{}
	if err := r.Client.Get(ctx, key, &svc); err == nil {
		if svc.Spec.Type == corev1.ServiceTypeNodePort && (svc.Labels == nil || svc.Labels[NodePortLabelKey] != True) {
			if svc.Labels == nil {
				svc.Labels = make(map[string]string)
			}
			if err := retryUpdateOnConflict(ctx, r.Client, &svc, func() {
				svc.Labels[NodePortLabelKey] = True
				svc.Spec.Type = corev1.ServiceTypeClusterIP
			}); err != nil {
				return fmt.Errorf("failed to suspend service %s: %w", key.Name, err)
			}
			r.Log.V(1).Info("Suspended service", "name", key.Name)
		}
		return nil
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get service %s: %w", key.Name, err)
	}

	return nil
}

func (r *NetworkReconciler) handleIstioResource(ctx context.Context, key client.ObjectKey) error {
	// Try fetching as VirtualService
	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualService",
	})
	
	if err := r.Client.Get(ctx, key, vs); err == nil {
		// 检查是否已经暂停
		if annotations := vs.GetAnnotations(); annotations != nil {
			if annotations["network.sealos.io/suspended"] == "true" {
				return nil // 已经暂停
			}
		}
		
		// 添加暂停注解
		annotations := vs.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations["network.sealos.io/suspended"] = "true"
		
		// 备份原始 HTTP 路由
		spec, found, err := unstructured.NestedMap(vs.Object, "spec")
		if err == nil && found {
			if httpRoutes, found, _ := unstructured.NestedSlice(spec, "http"); found {
				annotations["network.sealos.io/original-http"] = r.encodeRoutes(httpRoutes)
			}
		}
		vs.SetAnnotations(annotations)
		
		// 设置暂停路由
		suspendRoute := []interface{}{
			map[string]interface{}{
				"match": []interface{}{
					map[string]interface{}{
						"uri": map[string]interface{}{
							"prefix": "/",
						},
					},
				},
				"directResponse": map[string]interface{}{
					"status": 503,
					"body": map[string]interface{}{
						"string": "Service temporarily suspended for resource management",
					},
				},
			},
		}
		
		if err := retryUpdateOnConflict(ctx, r.Client, vs, func() {
			unstructured.SetNestedSlice(vs.Object, suspendRoute, "spec", "http")
		}); err != nil {
			return fmt.Errorf("failed to suspend virtual service %s: %w", key.Name, err)
		}
		r.Log.V(1).Info("Suspended virtual service", "name", key.Name)
		return nil
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get virtual service %s: %w", key.Name, err)
	}

	// Try fetching as Service (NodePort services still need to be handled in Istio mode)
	svc := corev1.Service{}
	if err := r.Client.Get(ctx, key, &svc); err == nil {
		if svc.Spec.Type == corev1.ServiceTypeNodePort && (svc.Labels == nil || svc.Labels[NodePortLabelKey] != True) {
			if svc.Labels == nil {
				svc.Labels = make(map[string]string)
			}
			if err := retryUpdateOnConflict(ctx, r.Client, &svc, func() {
				svc.Labels[NodePortLabelKey] = True
				svc.Spec.Type = corev1.ServiceTypeClusterIP
			}); err != nil {
				return fmt.Errorf("failed to suspend service %s: %w", key.Name, err)
			}
			r.Log.V(1).Info("Suspended service", "name", key.Name)
		}
		return nil
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get service %s: %w", key.Name, err)
	}

	return nil
}

func (r *NetworkReconciler) suspendNetworkResources(ctx context.Context, namespace string) error {
	// 根据配置决定使用 Istio 还是 Ingress
	if r.useIstio && r.networkingManager != nil {
		return r.suspendIstioResources(ctx, namespace)
	}
	
	return r.suspendIngressResources(ctx, namespace)
}

func (r *NetworkReconciler) suspendIngressResources(ctx context.Context, namespace string) error {
	// Suspend Ingresses
	ingressList := networkingv1.IngressList{}
	if err := r.Client.List(ctx, &ingressList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list ingresses in namespace %s: %w", namespace, err)
	}
	for _, ingress := range ingressList.Items {
		if ingress.Annotations == nil {
			ingress.Annotations = make(map[string]string)
		}
		if ingress.Annotations[IngressClassKey] != Disable {
			if err := retryUpdateOnConflict(ctx, r.Client, &ingress, func() {
				ingress.Annotations[IngressClassKey] = Disable
			}); err != nil {
				return fmt.Errorf("failed to suspend ingress %s: %w", ingress.Name, err)
			}
			r.Log.V(1).Info("Suspended ingress", "name", ingress.Name)
		}
	}

	// Suspend NodePort Services
	serviceList := corev1.ServiceList{}
	if err := r.Client.List(ctx, &serviceList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list services in namespace %s: %w", namespace, err)
	}
	for _, svc := range serviceList.Items {
		if svc.Spec.Type != corev1.ServiceTypeNodePort {
			continue
		}
		if svc.Labels == nil {
			svc.Labels = make(map[string]string)
		}
		if err := retryUpdateOnConflict(ctx, r.Client, &svc, func() {
			svc.Labels[NodePortLabelKey] = True
			svc.Spec.Type = corev1.ServiceTypeClusterIP
		}); err != nil {
			return fmt.Errorf("failed to suspend service %s: %w", svc.Name, err)
		}
		r.Log.V(1).Info("Suspended service", "name", svc.Name)
	}

	return nil
}

func (r *NetworkReconciler) resumeNetworkResources(ctx context.Context, namespace string) error {
	// 根据配置决定使用 Istio 还是 Ingress
	if r.useIstio && r.networkingManager != nil {
		return r.resumeIstioResources(ctx, namespace)
	}
	
	return r.resumeIngressResources(ctx, namespace)
}

func (r *NetworkReconciler) resumeIngressResources(ctx context.Context, namespace string) error {
	// Resume Ingresses
	ingressList := networkingv1.IngressList{}
	if err := r.Client.List(ctx, &ingressList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list ingresses in namespace %s: %w", namespace, err)
	}
	for _, ingress := range ingressList.Items {
		if ingress.Annotations == nil || ingress.Annotations[IngressClassKey] != Disable {
			continue
		}
		if err := retryUpdateOnConflict(ctx, r.Client, &ingress, func() {
			ingress.Annotations[IngressClassKey] = "nginx"
		}); err != nil {
			return fmt.Errorf("failed to resume ingress %s: %w", ingress.Name, err)
		}
		r.Log.V(1).Info("Resumed ingress", "name", ingress.Name)
	}

	// Resume NodePort Services
	serviceList := corev1.ServiceList{}
	if err := r.Client.List(ctx, &serviceList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list services in namespace %s: %w", namespace, err)
	}
	for _, svc := range serviceList.Items {
		if svc.Labels == nil || svc.Labels[NodePortLabelKey] != True {
			continue
		}
		if err := retryUpdateOnConflict(ctx, r.Client, &svc, func() {
			svc.Spec.Type = corev1.ServiceTypeNodePort
			delete(svc.Labels, NodePortLabelKey)
		}); err != nil {
			return fmt.Errorf("failed to resume service %s: %w", svc.Name, err)
		}
		r.Log.V(1).Info("Resumed service", "name", svc.Name)
	}

	return nil
}

func (r *NetworkReconciler) suspendIstioResources(ctx context.Context, namespace string) error {
	// Suspend VirtualServices by setting traffic to 0
	vsList := &unstructured.UnstructuredList{}
	vsList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualServiceList",
	})
	
	if err := r.Client.List(ctx, vsList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list virtual services in namespace %s: %w", namespace, err)
	}
	
	for _, vs := range vsList.Items {
		// 检查是否已经暂停
		if annotations := vs.GetAnnotations(); annotations != nil {
			if annotations["network.sealos.io/suspended"] == "true" {
				continue
			}
		}
		
		// 添加暂停注解
		annotations := vs.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations["network.sealos.io/suspended"] = "true"
		vs.SetAnnotations(annotations)
		
		// 修改 VirtualService 规则，将流量重定向到 503 页面
		spec, found, err := unstructured.NestedMap(vs.Object, "spec")
		if err != nil || !found {
			continue
		}
		
		// 备份原始 HTTP 路由
		if httpRoutes, found, _ := unstructured.NestedSlice(spec, "http"); found {
			annotations["network.sealos.io/original-http"] = r.encodeRoutes(httpRoutes)
			vs.SetAnnotations(annotations)
		}
		
		// 设置暂停路由
		suspendRoute := []interface{}{
			map[string]interface{}{
				"match": []interface{}{
					map[string]interface{}{
						"uri": map[string]interface{}{
							"prefix": "/",
						},
					},
				},
				"directResponse": map[string]interface{}{
					"status": 503,
					"body": map[string]interface{}{
						"string": "Service temporarily suspended for resource management",
					},
				},
			},
		}
		
		if err := retryUpdateOnConflict(ctx, r.Client, &vs, func() {
			unstructured.SetNestedSlice(vs.Object, suspendRoute, "spec", "http")
		}); err != nil {
			return fmt.Errorf("failed to suspend virtual service %s: %w", vs.GetName(), err)
		}
		r.Log.V(1).Info("Suspended virtual service", "name", vs.GetName())
	}
	
	// 同样暂停 NodePort Services
	serviceList := corev1.ServiceList{}
	if err := r.Client.List(ctx, &serviceList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list services in namespace %s: %w", namespace, err)
	}
	for _, svc := range serviceList.Items {
		if svc.Spec.Type != corev1.ServiceTypeNodePort {
			continue
		}
		if svc.Labels == nil {
			svc.Labels = make(map[string]string)
		}
		if err := retryUpdateOnConflict(ctx, r.Client, &svc, func() {
			svc.Labels[NodePortLabelKey] = True
			svc.Spec.Type = corev1.ServiceTypeClusterIP
		}); err != nil {
			return fmt.Errorf("failed to suspend service %s: %w", svc.Name, err)
		}
		r.Log.V(1).Info("Suspended service", "name", svc.Name)
	}
	
	return nil
}

func (r *NetworkReconciler) resumeIstioResources(ctx context.Context, namespace string) error {
	// Resume VirtualServices
	vsList := &unstructured.UnstructuredList{}
	vsList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualServiceList",
	})
	
	if err := r.Client.List(ctx, vsList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list virtual services in namespace %s: %w", namespace, err)
	}
	
	for _, vs := range vsList.Items {
		// 检查是否被暂停
		annotations := vs.GetAnnotations()
		if annotations == nil || annotations["network.sealos.io/suspended"] != "true" {
			continue
		}
		
		// 恢复原始路由
		if originalHTTP, exists := annotations["network.sealos.io/original-http"]; exists {
			if routes := r.decodeRoutes(originalHTTP); routes != nil {
				unstructured.SetNestedSlice(vs.Object, routes, "spec", "http")
			}
			delete(annotations, "network.sealos.io/original-http")
		}
		
		// 移除暂停注解
		if err := retryUpdateOnConflict(ctx, r.Client, &vs, func() {
			delete(annotations, "network.sealos.io/suspended")
			vs.SetAnnotations(annotations)
		}); err != nil {
			return fmt.Errorf("failed to resume virtual service %s: %w", vs.GetName(), err)
		}
		r.Log.V(1).Info("Resumed virtual service", "name", vs.GetName())
	}
	
	// Resume NodePort Services
	serviceList := corev1.ServiceList{}
	if err := r.Client.List(ctx, &serviceList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list services in namespace %s: %w", namespace, err)
	}
	for _, svc := range serviceList.Items {
		if svc.Labels == nil || svc.Labels[NodePortLabelKey] != True {
			continue
		}
		if err := retryUpdateOnConflict(ctx, r.Client, &svc, func() {
			svc.Spec.Type = corev1.ServiceTypeNodePort
			delete(svc.Labels, NodePortLabelKey)
		}); err != nil {
			return fmt.Errorf("failed to resume service %s: %w", svc.Name, err)
		}
		r.Log.V(1).Info("Resumed service", "name", svc.Name)
	}
	
	return nil
}

// encodeRoutes 编码路由为字符串
func (r *NetworkReconciler) encodeRoutes(routes []interface{}) string {
	// 简单的 JSON 编码，生产环境可能需要更复杂的序列化
	data, _ := json.Marshal(routes)
	return string(data)
}

// decodeRoutes 解码路由字符串
func (r *NetworkReconciler) decodeRoutes(encoded string) []interface{} {
	var routes []interface{}
	if err := json.Unmarshal([]byte(encoded), &routes); err != nil {
		return nil
	}
	return routes
}

// SuspendedNamespaceHandler enqueues requests for Ingress and Service objects only in suspended namespaces
type SuspendedNamespaceHandler struct {
	Client client.Client
	Logger logr.Logger
}

func (e *SuspendedNamespaceHandler) Create(ctx context.Context, evt event.TypedCreateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if isNil(evt.Object) {
		e.Logger.Error(nil, "CreateEvent received with no metadata", "event", evt)
		return
	}

	ns := corev1.Namespace{}
	if err := e.Client.Get(ctx, types.NamespacedName{Name: evt.Object.GetNamespace()}, &ns); err != nil {
		e.Logger.Error(err, "failed to get namespace", "namespace", evt.Object.GetNamespace())
		return
	}

	networkStatus, ok := ns.Annotations[NetworkStatusAnnoKey]
	if !ok || networkStatus != NetworkSuspend {
		return
	}

	item := reconcile.Request{NamespacedName: types.NamespacedName{
		Name:      evt.Object.GetName(),
		Namespace: evt.Object.GetNamespace(),
	}}
	q.Add(item)
}

func (e *SuspendedNamespaceHandler) Update(ctx context.Context, evt event.TypedUpdateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if !isNil(evt.ObjectNew) {
		ns := corev1.Namespace{}
		if err := e.Client.Get(ctx, types.NamespacedName{Name: evt.ObjectNew.GetNamespace()}, &ns); err != nil {
			e.Logger.Error(err, "failed to get namespace", "namespace", evt.ObjectNew.GetNamespace())
			return
		}

		networkStatus, ok := ns.Annotations[NetworkStatusAnnoKey]
		if !ok || networkStatus != NetworkSuspend {
			return
		}

		item := reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      evt.ObjectNew.GetName(),
			Namespace: evt.ObjectNew.GetNamespace(),
		}}
		q.Add(item)
	} else if !isNil(evt.ObjectOld) {
		ns := corev1.Namespace{}
		if err := e.Client.Get(ctx, types.NamespacedName{Name: evt.ObjectOld.GetNamespace()}, &ns); err != nil {
			e.Logger.Error(err, "failed to get namespace", "namespace", evt.ObjectOld.GetNamespace())
			return
		}

		networkStatus, ok := ns.Annotations[NetworkStatusAnnoKey]
		if !ok || networkStatus != NetworkSuspend {
			return
		}

		item := reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      evt.ObjectOld.GetName(),
			Namespace: evt.ObjectOld.GetNamespace(),
		}}
		q.Add(item)
	} else {
		e.Logger.Error(nil, "UpdateEvent received with no metadata", "event", evt)
	}
}

func (e *SuspendedNamespaceHandler) Delete(ctx context.Context, evt event.TypedDeleteEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// No action needed for delete events
}

func (e *SuspendedNamespaceHandler) Generic(ctx context.Context, evt event.TypedGenericEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// No action needed for generic events
}

func isNil(arg any) bool {
	if v := reflect.ValueOf(arg); !v.IsValid() || ((v.Kind() == reflect.Ptr ||
		v.Kind() == reflect.Interface ||
		v.Kind() == reflect.Slice ||
		v.Kind() == reflect.Map ||
		v.Kind() == reflect.Chan ||
		v.Kind() == reflect.Func) && v.IsNil()) {
		return true
	}
	return false
}

func (r *NetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Log = ctrl.Log.WithName("controllers").WithName("Network")
	r.Client = mgr.GetClient()
	suspendedHandler := &SuspendedNamespaceHandler{Client: r.Client, Logger: r.Log}

	// 初始化 Istio 支持
	ctx := context.Background()
	if err := r.SetupIstioSupport(ctx); err != nil {
		r.Log.Error(err, "failed to setup Istio support, continuing with Ingress mode")
		r.useIstio = false
		r.networkingManager = nil
	}

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}, builder.WithPredicates(NetworkAnnotationPredicate{})).
		Watches(
			&networkingv1.Ingress{},
			suspendedHandler,
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return true
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					newIngress, ok := e.ObjectNew.(*networkingv1.Ingress)
					if !ok {
						return false
					}
					return newIngress.Annotations != nil && newIngress.Annotations[IngressClassKey] != Disable
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return false
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			}),
		).
		Watches(
			&corev1.Service{},
			suspendedHandler,
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					svc, ok := e.Object.(*corev1.Service)
					if !ok {
						return false
					}
					return svc.Spec.Type == corev1.ServiceTypeNodePort
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					newSvc, ok := e.ObjectNew.(*corev1.Service)
					if !ok {
						return false
					}
					return newSvc.Spec.Type == corev1.ServiceTypeNodePort && (newSvc.Labels == nil || newSvc.Labels[NodePortLabelKey] != True)
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return false
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			}),
		)

	// 如果启用了 Istio，添加对 VirtualService 的监听
	if r.useIstio {
		virtualServiceType := &unstructured.Unstructured{}
		virtualServiceType.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "networking.istio.io",
			Version: "v1beta1",
			Kind:    "VirtualService",
		})
		
		controllerBuilder = controllerBuilder.Watches(
			virtualServiceType,
			suspendedHandler,
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return true
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					// 监听 VirtualService 的变化
					return true
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return false
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			}),
		)
	}

	return controllerBuilder.Complete(r)
}

// NetworkAnnotationPredicate filters namespace events based on network status annotation changes
type NetworkAnnotationPredicate struct {
	predicate.Funcs
}

func (NetworkAnnotationPredicate) Create(e event.CreateEvent) bool {
	networkStatus, ok := e.Object.GetAnnotations()[NetworkStatusAnnoKey]
	return ok && networkStatus != NetworkResumeCompleted
}

func (NetworkAnnotationPredicate) Update(e event.UpdateEvent) bool {
	oldObj, ok1 := e.ObjectOld.(*corev1.Namespace)
	newObj, ok2 := e.ObjectNew.(*corev1.Namespace)
	if !ok1 || !ok2 || newObj.Annotations == nil {
		return false
	}
	oldStatus := oldObj.Annotations[NetworkStatusAnnoKey]
	newStatus := newObj.Annotations[NetworkStatusAnnoKey]
	return oldStatus != newStatus && newStatus != NetworkResumeCompleted
}

func (NetworkAnnotationPredicate) Delete(e event.DeleteEvent) bool {
	return false
}

func (NetworkAnnotationPredicate) Generic(e event.GenericEvent) bool {
	return false
}

// SetupIstioSupport 设置 Resources 控制器的 Istio 支持
func (r *NetworkReconciler) SetupIstioSupport(ctx context.Context) error {
	logger := log.FromContext(ctx)
	
	// 检查是否启用 Istio
	useIstio := os.Getenv("USE_ISTIO")
	if useIstio != "true" {
		logger.Info("Istio support is disabled for Resources controller")
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
		logger.Info("Istio is not installed, falling back to Ingress mode for Resources controller")
		r.useIstio = false
		return nil
	}
	
	// 构建 Istio 网络配置
	config := r.buildIstioNetworkConfig()
	
	// 🎯 使用优化的 Istio 网络管理器
	r.networkingManager = istio.NewOptimizedNetworkingManager(r.Client, config)
	
	// 验证 Istio 安装
	if err := r.validateIstioInstallation(ctx); err != nil {
		logger.Error(err, "Istio validation failed, falling back to Ingress mode for Resources controller")
		r.useIstio = false
		r.networkingManager = nil
		return nil
	}
	
	r.useIstio = true
	logger.Info("Istio support enabled for Resources controller")
	
	return nil
}

// buildIstioNetworkConfig 构建 Istio 网络配置（使用智能Gateway优化）
func (r *NetworkReconciler) buildIstioNetworkConfig() *istio.NetworkConfig {
	config := istio.DefaultNetworkConfig()
	
	// 基础域名配置
	if baseDomain := os.Getenv("ISTIO_BASE_DOMAIN"); baseDomain != "" {
		config.BaseDomain = baseDomain
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
	
	// Resources 控制器用于网络管理，设置通用的域名模板
	config.DomainTemplates = map[string]string{
		"resources": "{{.Hash}}.{{.TenantID}}.{{.BaseDomain}}", // 通用资源域名模板
	}
	
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
func (r *NetworkReconciler) configurePublicDomains(config *istio.NetworkConfig) {
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
		// 设置一些常见的公共域名
		config.PublicDomains = append(config.PublicDomains, "sealos.io", "cloud.sealos.io")
		config.PublicDomainPatterns = append(config.PublicDomainPatterns, "*.sealos.io", "*.cloud.sealos.io")
	}
}

// validateIstioInstallation 验证 Istio 是否已安装
func (r *NetworkReconciler) validateIstioInstallation(ctx context.Context) error {
	isEnabled, err := istio.IsIstioEnabled(r.Client)
	if err != nil {
		return fmt.Errorf("failed to check Istio installation: %w", err)
	}
	
	if !isEnabled {
		return fmt.Errorf("Istio is not installed or enabled in the cluster")
	}
	
	return nil
}

// IsIstioEnabled 检查是否启用了 Istio 模式
func (r *NetworkReconciler) IsIstioEnabled() bool {
	return r.useIstio
}

// EnableIstioMode 动态启用 Istio 模式
func (r *NetworkReconciler) EnableIstioMode(ctx context.Context) error {
	if r.useIstio {
		return nil // 已经启用
	}
	
	return r.SetupIstioSupport(ctx)
}

// DisableIstioMode 禁用 Istio 模式，回退到 Ingress
func (r *NetworkReconciler) DisableIstioMode() {
	r.useIstio = false
	r.networkingManager = nil
}

// GetNetworkingMode 获取当前网络模式
func (r *NetworkReconciler) GetNetworkingMode() string {
	if r.useIstio {
		return "Istio"
	}
	return "Ingress"
}
