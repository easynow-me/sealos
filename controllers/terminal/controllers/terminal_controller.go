/*
Copyright 2022 labring.

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
	"strings"
	"time"

	nanoid "github.com/matoous/go-nanoid/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/labring/sealos/controllers/pkg/istio"
	"github.com/labring/sealos/controllers/pkg/utils/label"
	terminalv1 "github.com/labring/sealos/controllers/terminal/api/v1"
)

const TerminalPartOf = "terminal"

const (
	Protocol            = "https://"
	FinalizerName       = "terminal.sealos.io/finalizer"
	HostnameLength      = 8
	KeepaliveAnnotation = "lastUpdateTime"
	LetterBytes         = "abcdefghijklmnopqrstuvwxyz0123456789"
)

const (
	DefaultDomain          = "cloud.sealos.io"
	DefaultPort            = ""
	DefaultSecretName      = "wildcard-cert"
	DefaultSecretNamespace = "sealos-system"
)

// request and limit for terminal pod
const (
	CPURequest    = "0.01"
	MemoryRequest = "16Mi"
	CPULimit      = "0.3"
	MemoryLimit   = "256Mi"
)

const (
	SecretHeaderPrefix = "X-SEALOS-"
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

// retryStatusUpdateOnConflict retries the status update operation when there's a resource version conflict
func retryStatusUpdateOnConflict(ctx context.Context, c client.Client, obj client.Object, updateFunc func()) error {
	return wait.PollImmediate(100*time.Millisecond, 3*time.Second, func() (bool, error) {
		updateFunc()
		err := c.Status().Update(ctx, obj)
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

// TerminalReconciler reconciles a Terminal object
type TerminalReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	recorder        record.EventRecorder
	Config          *rest.Config
	CtrConfig       *Config
	istioReconciler *IstioNetworkingReconciler            // 保留向后兼容
	istioHelper     *istio.UniversalIstioNetworkingHelper // 🎯 新增通用助手
	useIstio        bool
}

//+kubebuilder:rbac:groups=terminal.sealos.io,resources=terminals,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=terminal.sealos.io,resources=terminals/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=terminal.sealos.io,resources=terminals/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=gateways,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=gateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.istio.io,resources=destinationrules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=destinationrules/status,verbs=get;update;patch

func (r *TerminalReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx, "terminal", req.NamespacedName)
	terminal := &terminalv1.Terminal{}
	if err := r.Get(ctx, req.NamespacedName, terminal); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if terminal.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.AddFinalizer(terminal, FinalizerName) {
			if err := retryUpdateOnConflict(ctx, r.Client, terminal, func() {
				controllerutil.AddFinalizer(terminal, FinalizerName)
			}); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if controllerutil.RemoveFinalizer(terminal, FinalizerName) {
			if err := retryUpdateOnConflict(ctx, r.Client, terminal, func() {
				controllerutil.RemoveFinalizer(terminal, FinalizerName)
			}); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if err := r.fillDefaultValue(ctx, terminal); err != nil {
		return ctrl.Result{}, err
	}

	if isExpired(terminal) {
		if err := r.Delete(ctx, terminal); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("delete expired terminal success")
		return ctrl.Result{}, nil
	}

	if terminal.Status.ServiceName == "" {
		if err := retryStatusUpdateOnConflict(ctx, r.Client, terminal, func() {
			terminal.Status.ServiceName = terminal.Name + "-svc" + rand.String(5)
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	if terminal.Status.SecretHeader == "" {
		if err := retryStatusUpdateOnConflict(ctx, r.Client, terminal, func() {
			terminal.Status.SecretHeader = r.generateSecretHeader()
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	recLabels := label.RecommendedLabels(&label.Recommended{
		Name:      terminal.Name,
		ManagedBy: label.DefaultManagedBy,
		PartOf:    TerminalPartOf,
	})

	//Note: Fixme: For `Forward Compatibility` usage only, old resource controller need this label.
	recLabels["TerminalID"] = terminal.Name

	var hostname string
	if err := r.syncDeployment(ctx, terminal, &hostname, recLabels); err != nil {
		logger.Error(err, "create deployment failed")
		r.recorder.Eventf(terminal, corev1.EventTypeWarning, "Create deployment failed", "%v", err)
		return ctrl.Result{}, err
	}

	if err := r.syncService(ctx, terminal, recLabels); err != nil {
		logger.Error(err, "create service failed")
		r.recorder.Eventf(terminal, corev1.EventTypeWarning, "Create service failed", "%v", err)
		return ctrl.Result{}, err
	}

	if err := r.syncNetworking(ctx, terminal, hostname, recLabels); err != nil {
		logger.Error(err, "create networking failed")
		r.recorder.Eventf(terminal, corev1.EventTypeWarning, "Create networking failed", "%v", err)
		return ctrl.Result{}, err
	}

	r.recorder.Eventf(terminal, corev1.EventTypeNormal, "Created", "create terminal success: %v", terminal.Name)
	duration, _ := time.ParseDuration(terminal.Spec.Keepalived)
	return ctrl.Result{RequeueAfter: duration}, nil
}

func (r *TerminalReconciler) syncNetworking(ctx context.Context, terminal *terminalv1.Terminal, hostname string, recLabels map[string]string) error {
	// 根据配置决定使用 Istio 还是 Ingress
	if r.useIstio && r.istioReconciler != nil {
		return r.syncIstioNetworking(ctx, terminal, hostname, recLabels)
	}

	// 回退到原有的 Ingress 模式
	return r.syncIngress(ctx, terminal, hostname, recLabels)
}

func (r *TerminalReconciler) syncIngress(ctx context.Context, terminal *terminalv1.Terminal, hostname string, recLabels map[string]string) error {
	var err error
	host := hostname + "." + r.CtrConfig.Global.CloudDomain
	switch terminal.Spec.IngressType {
	case terminalv1.Nginx:
		err = r.syncNginxIngress(ctx, terminal, host, recLabels)
	}
	return err
}

func (r *TerminalReconciler) syncIstioNetworking(ctx context.Context, terminal *terminalv1.Terminal, hostname string, recLabels map[string]string) error {
	// 🎯 使用智能Gateway的优化网络配置
	if r.istioHelper != nil {
		return r.syncOptimizedIstioNetworking(ctx, terminal, hostname, recLabels)
	}

	// 回退到原有实现（向后兼容）
	if err := r.istioReconciler.SyncIstioNetworking(ctx, terminal, hostname); err != nil {
		return err
	}

	// 更新 Terminal 状态中的域名
	host := hostname + "." + r.CtrConfig.Global.CloudDomain
	domain := Protocol + host + r.getPort()
	if terminal.Status.Domain != domain {
		return retryStatusUpdateOnConflict(ctx, r.Client, terminal, func() {
			terminal.Status.Domain = domain
		})
	}

	return nil
}

// syncOptimizedIstioNetworking 使用智能Gateway的优化网络配置
func (r *TerminalReconciler) syncOptimizedIstioNetworking(ctx context.Context, terminal *terminalv1.Terminal, hostname string, recLabels map[string]string) error {
	// 构建域名
	host := hostname + "." + r.CtrConfig.Global.CloudDomain

	// 🎯 使用通用助手的智能网络配置
	params := &istio.AppNetworkingParams{
		Name:        terminal.Name,
		Namespace:   terminal.Namespace,
		AppType:     "terminal",
		Hosts:       []string{host},
		ServiceName: terminal.Status.ServiceName,
		ServicePort: 8080,
		Protocol:    istio.ProtocolWebSocket, // Terminal使用WebSocket协议

		// Terminal专用配置
		Timeout:      &[]time.Duration{86400 * time.Second}[0], // 24小时超时，支持长时间SSH会话
		SecretHeader: terminal.Status.SecretHeader,             // Terminal安全头

		// CORS 配置
		CorsPolicy: &istio.CorsPolicy{
			AllowOrigins:     r.buildTerminalCorsOrigins(),
			AllowMethods:     []string{"PUT", "GET", "POST", "PATCH", "OPTIONS"},
			AllowHeaders:     []string{"content-type", "authorization"},
			AllowCredentials: false,
		},

		// 响应头部配置（安全头部）
		ResponseHeaders: r.buildSecurityResponseHeaders(),

		// TLS 配置
		TLSEnabled: r.CtrConfig.Global.CloudPort == "" || r.CtrConfig.Global.CloudPort == "443",

		// 标签和注解
		Labels: recLabels,
		Annotations: map[string]string{
			"sealos.io/converted-from": "terminal-controller",
			"sealos.io/gateway-type":   "optimized", // 标记使用优化Gateway
			"sealos.io/protocol":       "websocket", // 标记协议类型
		},

		// 设置 Owner Reference
		OwnerObject: terminal,
	}

	// 🎯 关键：使用通用助手创建优化的网络配置（自动选择Gateway）
	if err := r.istioHelper.CreateOrUpdateNetworking(ctx, params); err != nil {
		return fmt.Errorf("failed to sync optimized istio networking: %w", err)
	}

	// 🎯 分析域名需求（展示智能Gateway选择过程）
	analysis := r.istioHelper.AnalyzeDomainRequirements(params)

	// 更新 Terminal 状态中的域名和Gateway信息
	domain := Protocol + host + r.getPort()

	return retryStatusUpdateOnConflict(ctx, r.Client, terminal, func() {
		terminal.Status.Domain = domain

		// 🎯 添加Gateway优化状态信息
		if terminal.Annotations == nil {
			terminal.Annotations = make(map[string]string)
		}
		terminal.Annotations["sealos.io/gateway-type"] = "optimized"
		terminal.Annotations["sealos.io/domain-type"] = func() string {
			if analysis.IsPublicDomain {
				return "public"
			}
			return "custom"
		}()
		terminal.Annotations["sealos.io/gateway-reference"] = analysis.GatewayReference
	})
}

// buildTerminalCorsOrigins 构建Terminal的CORS源 - 使用精确匹配的terminal子域名
func (r *TerminalReconciler) buildTerminalCorsOrigins() []string {
	corsOrigins := []string{}

	// 检查是否启用了 TLS
	tlsEnabled := r.CtrConfig.Global.CloudPort == "" || r.CtrConfig.Global.CloudPort == "443"

	if tlsEnabled {
		// 添加精确的 terminal 子域名
		corsOrigins = append(corsOrigins, fmt.Sprintf("https://terminal.%s", r.CtrConfig.Global.CloudDomain))

		// 如果使用了 istioReconciler，获取公共域名配置
		if r.istioReconciler != nil && r.istioReconciler.config != nil {
			for _, publicDomain := range r.istioReconciler.config.PublicDomains {
				// 处理通配符域名 (如 *.cloud.sealos.io)
				if len(publicDomain) > 2 && publicDomain[0:2] == "*." {
					baseDomain := publicDomain[2:]
					corsOrigins = append(corsOrigins, fmt.Sprintf("https://terminal.%s", baseDomain))
				} else {
					// 精确域名
					corsOrigins = append(corsOrigins, fmt.Sprintf("https://terminal.%s", publicDomain))
				}
			}
		}
	} else {
		// HTTP 模式
		corsOrigins = append(corsOrigins, fmt.Sprintf("http://terminal.%s", r.CtrConfig.Global.CloudDomain))

		if r.istioReconciler != nil && r.istioReconciler.config != nil {
			for _, publicDomain := range r.istioReconciler.config.PublicDomains {
				if len(publicDomain) > 2 && publicDomain[0:2] == "*." {
					baseDomain := publicDomain[2:]
					corsOrigins = append(corsOrigins, fmt.Sprintf("http://terminal.%s", baseDomain))
				} else {
					corsOrigins = append(corsOrigins, fmt.Sprintf("http://terminal.%s", publicDomain))
				}
			}
		}
	}

	// 去重
	uniqueOrigins := make([]string, 0, len(corsOrigins))
	seen := make(map[string]bool)
	for _, origin := range corsOrigins {
		if !seen[origin] {
			uniqueOrigins = append(uniqueOrigins, origin)
			seen[origin] = true
		}
	}

	return uniqueOrigins
}

// buildSecurityResponseHeaders 构建安全响应头部
func (r *TerminalReconciler) buildSecurityResponseHeaders() map[string]string {
	headers := make(map[string]string)

	// 设置 X-Frame-Options，防止点击劫持
	headers["X-Frame-Options"] = ""

	// 设置 X-Content-Type-Options，防止 MIME 类型嗅探
	headers["X-Content-Type-Options"] = "nosniff"

	// 设置 X-XSS-Protection，虽然现代浏览器已经内置了 XSS 保护
	headers["X-XSS-Protection"] = "1; mode=block"

	// 设置 Referrer-Policy
	headers["Referrer-Policy"] = "strict-origin-when-cross-origin"

	// 对于 WebSocket 应用，通常不需要设置 CSP，因为它主要处理二进制流
	// 但我们可以设置一个基本的 CSP 来增强安全性
	headers["Content-Security-Policy"] = "default-src 'self'; connect-src 'self' wss:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline' 'unsafe-eval';"

	return headers
}

func (r *TerminalReconciler) syncNginxIngress(ctx context.Context, terminal *terminalv1.Terminal, host string, recLabels map[string]string) error {
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      terminal.Name,
			Namespace: terminal.Namespace,
			Labels:    recLabels,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		expectIngress := r.createNginxIngress(terminal, host)
		ingress.ObjectMeta.Annotations = expectIngress.ObjectMeta.Annotations
		ingress.Spec.Rules = expectIngress.Spec.Rules
		ingress.Spec.TLS = expectIngress.Spec.TLS
		return controllerutil.SetControllerReference(terminal, ingress, r.Scheme)
	}); err != nil {
		return err
	}

	domain := Protocol + host + r.getPort()
	if terminal.Status.Domain != domain {
		return retryStatusUpdateOnConflict(ctx, r.Client, terminal, func() {
			terminal.Status.Domain = domain
		})
	}

	return nil
}

func (r *TerminalReconciler) syncService(ctx context.Context, terminal *terminalv1.Terminal, recLabels map[string]string) error {
	expectServiceSpec := corev1.ServiceSpec{
		Selector: recLabels,
		Type:     corev1.ServiceTypeClusterIP,
		Ports: []corev1.ServicePort{
			{Name: "tty", Port: 8080, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      terminal.Status.ServiceName,
			Namespace: terminal.Namespace,
			Labels:    recLabels,
		},
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		// only update some specific fields
		service.Spec.Selector = expectServiceSpec.Selector
		service.Spec.Type = expectServiceSpec.Type
		if len(service.Spec.Ports) == 0 {
			service.Spec.Ports = expectServiceSpec.Ports
		} else {
			service.Spec.Ports[0].Name = expectServiceSpec.Ports[0].Name
			service.Spec.Ports[0].Port = expectServiceSpec.Ports[0].Port
			service.Spec.Ports[0].TargetPort = expectServiceSpec.Ports[0].TargetPort
			service.Spec.Ports[0].Protocol = expectServiceSpec.Ports[0].Protocol
		}
		return controllerutil.SetControllerReference(terminal, service, r.Scheme)
	}); err != nil {
		return err
	}
	return nil
}

func (r *TerminalReconciler) syncDeployment(ctx context.Context, terminal *terminalv1.Terminal, hostname *string, recLabels map[string]string) error {
	var (
		objectMeta      metav1.ObjectMeta
		selector        *metav1.LabelSelector
		templateObjMeta metav1.ObjectMeta
		ports           []corev1.ContainerPort
		envs            []corev1.EnvVar
		containers      []corev1.Container
	)

	objectMeta = metav1.ObjectMeta{
		Name:      terminal.Name,
		Namespace: terminal.Namespace,
		Labels:    recLabels,
	}
	selector = &metav1.LabelSelector{
		MatchLabels: recLabels,
	}
	templateObjMeta = metav1.ObjectMeta{
		Labels: recLabels,
	}
	ports = []corev1.ContainerPort{
		{
			Name:          "http",
			Protocol:      corev1.ProtocolTCP,
			ContainerPort: 8080,
		},
	}
	envs = []corev1.EnvVar{
		{Name: "APISERVER", Value: terminal.Spec.APIServer},
		{Name: "USER_TOKEN", Value: terminal.Spec.Token},
		{Name: "NAMESPACE", Value: terminal.Namespace},
		{Name: "USER_NAME", Value: terminal.Spec.User},
		// Add secret header
		{Name: "AUTH_HEADER", Value: terminal.Status.SecretHeader},
	}

	containers = []corev1.Container{
		{
			Name:  "tty",
			Image: terminal.Spec.TTYImage,
			Ports: ports,
			Env:   envs,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"cpu":    resource.MustParse(CPURequest),
					"memory": resource.MustParse(MemoryRequest),
				},
				Limits: corev1.ResourceList{
					"cpu":    resource.MustParse(CPULimit),
					"memory": resource.MustParse(MemoryLimit),
				},
			},
		},
	}

	expectDeploymentSpec := appsv1.DeploymentSpec{
		Replicas: terminal.Spec.Replicas,
		Selector: selector,
		Template: corev1.PodTemplateSpec{
			ObjectMeta: templateObjMeta,
			Spec: corev1.PodSpec{
				Containers: containers,
			},
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: objectMeta,
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		// only update some specific fields
		deployment.Spec.Replicas = expectDeploymentSpec.Replicas
		deployment.Spec.Selector = expectDeploymentSpec.Selector
		deployment.Spec.Template.ObjectMeta.Labels = expectDeploymentSpec.Template.Labels
		if len(deployment.Spec.Template.Spec.Containers) == 0 {
			deployment.Spec.Template.Spec.Containers = containers
		} else {
			deployment.Spec.Template.Spec.Containers[0].Name = containers[0].Name
			deployment.Spec.Template.Spec.Containers[0].Image = containers[0].Image
			deployment.Spec.Template.Spec.Containers[0].Ports = containers[0].Ports
			deployment.Spec.Template.Spec.Containers[0].Env = containers[0].Env
			deployment.Spec.Template.Spec.Containers[0].Resources = containers[0].Resources
		}

		if deployment.Spec.Template.Spec.Hostname == "" {
			letterID, err := nanoid.Generate(LetterBytes, HostnameLength)
			if err != nil {
				return err
			}
			// to keep pace with ingress host, hostname must start with a lower case letter
			*hostname = "t" + letterID
			deployment.Spec.Template.Spec.Hostname = *hostname
		} else {
			*hostname = deployment.Spec.Template.Spec.Hostname
		}

		return controllerutil.SetControllerReference(terminal, deployment, r.Scheme)
	}); err != nil {
		return err
	}

	if terminal.Status.AvailableReplicas != deployment.Status.AvailableReplicas {
		return retryStatusUpdateOnConflict(ctx, r.Client, terminal, func() {
			terminal.Status.AvailableReplicas = deployment.Status.AvailableReplicas
		})
	}

	return nil
}

func (r *TerminalReconciler) fillDefaultValue(ctx context.Context, terminal *terminalv1.Terminal) error {
	hasUpdate := false
	if terminal.Spec.APIServer == "" {
		terminal.Spec.APIServer = r.Config.Host
		hasUpdate = true
	}

	if _, ok := terminal.ObjectMeta.Annotations[KeepaliveAnnotation]; !ok {
		terminal.ObjectMeta.Annotations[KeepaliveAnnotation] = time.Now().Format(time.RFC3339)
		hasUpdate = true
	}

	if hasUpdate {
		return retryUpdateOnConflict(ctx, r.Client, terminal, func() {
			if terminal.ObjectMeta.Annotations == nil {
				terminal.ObjectMeta.Annotations = make(map[string]string)
			}
			terminal.ObjectMeta.Annotations[KeepaliveAnnotation] = time.Now().Format(time.RFC3339)
		})
	}

	return nil
}

// isExpired return true if the terminal has expired
func isExpired(terminal *terminalv1.Terminal) bool {
	anno := terminal.ObjectMeta.Annotations
	lastUpdateTime, err := time.Parse(time.RFC3339, anno[KeepaliveAnnotation])
	if err != nil {
		// treat parse errors as not expired
		return false
	}

	duration, _ := time.ParseDuration(terminal.Spec.Keepalived)
	return lastUpdateTime.Add(duration).Before(time.Now())
}

func (r *TerminalReconciler) getPort() string {
	if r.CtrConfig.Global.CloudPort == "" || r.CtrConfig.Global.CloudPort == "80" || r.CtrConfig.Global.CloudPort == "443" {
		return ""
	}
	return ":" + r.CtrConfig.Global.CloudPort
}

func (r *TerminalReconciler) generateSecretHeader() string {
	return SecretHeaderPrefix + strings.ToUpper(rand.String(5))
}

// SetupWithManager sets up the controller with the Manager.
func (r *TerminalReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("sealos-terminal-controller")
	r.Config = mgr.GetConfig()

	// 初始化 Istio 支持
	ctx := context.Background()
	if err := r.SetupIstioSupport(ctx); err != nil {
		r.recorder.Eventf(&terminalv1.Terminal{}, corev1.EventTypeWarning, "IstioSetupFailed", "Failed to setup Istio support: %v", err)
		// 不返回错误，继续使用 Ingress 模式
	}

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&terminalv1.Terminal{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&appsv1.Deployment{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Owns(&corev1.Service{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&networkingv1.Ingress{}, builder.WithPredicates(predicate.GenerationChangedPredicate{}))

	// 如果启用了 Istio，添加对 Istio 资源的监听
	if r.useIstio {
		// 使用 unstructured 类型来监听 Istio CRDs
		virtualServiceType := &unstructured.Unstructured{}
		virtualServiceType.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "networking.istio.io",
			Version: "v1beta1",
			Kind:    "VirtualService",
		})

		gatewayType := &unstructured.Unstructured{}
		gatewayType.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "networking.istio.io",
			Version: "v1beta1",
			Kind:    "Gateway",
		})

		controllerBuilder = controllerBuilder.
			Owns(virtualServiceType).
			Owns(gatewayType)
	}

	return controllerBuilder.Complete(r)
}
