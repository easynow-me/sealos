/*
Copyright 2023 labring.

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
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	adminerv1 "github.com/labring/sealos/controllers/db/adminer/api/v1"
	"github.com/labring/sealos/controllers/pkg/istio"
	"github.com/labring/sealos/controllers/pkg/utils/label"
)

const (
	protocolHTTPS       = "https://"
	protocolHTTP        = "http://"
	FinalizerName       = "adminer.db.sealos.io/finalizer"
	HostnameLength      = 8
	KeepaliveAnnotation = "lastUpdateTime"
	LetterBytes         = "abcdefghijklmnopqrstuvwxyz0123456789"
)

const (
	AdminerPartOf = "adminer"
)

const (
	DefaultDomain          = "cloud.sealos.io"
	DefaultSecretName      = "wildcard-cloud-sealos-io-cert"
	DefaultSecretNamespace = "sealos-system"
	DefaultImage           = "docker.io/labring4docker/adminer:v4.8.1"
)

var (
	defaultReplicas = int32(1)
)

// request and limit for adminer pod
const (
	CPURequest    = "0.02"
	MemoryRequest = "32Mi"
	CPULimit      = "0.1"
	MemoryLimit   = "128Mi"
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

// AdminerReconciler reconciles a Adminer object
type AdminerReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	recorder        record.EventRecorder
	Config          *rest.Config
	adminerDomain   string
	tlsEnabled      bool
	image           string
	secretName      string
	secretNamespace string
	istioReconciler *AdminerIstioNetworkingReconciler     // 保留向后兼容
	istioHelper     *istio.UniversalIstioNetworkingHelper // 🎯 新增通用助手
	useIstio        bool
}

//+kubebuilder:rbac:groups=adminer.db.sealos.io,resources=adminers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=adminer.db.sealos.io,resources=adminers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=adminer.db.sealos.io,resources=adminers/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=gateways,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=gateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.istio.io,resources=destinationrules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=destinationrules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates/status,verbs=get;update;patch

//-kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Adminer object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *AdminerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx, "adminer", req.NamespacedName)
	adminer := &adminerv1.Adminer{}
	if err := r.Get(ctx, req.NamespacedName, adminer); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if adminer.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.AddFinalizer(adminer, FinalizerName) {
			if err := retryUpdateOnConflict(ctx, r.Client, adminer, func() {
				controllerutil.AddFinalizer(adminer, FinalizerName)
			}); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if controllerutil.RemoveFinalizer(adminer, FinalizerName) {
			if err := retryUpdateOnConflict(ctx, r.Client, adminer, func() {
				controllerutil.RemoveFinalizer(adminer, FinalizerName)
			}); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if err := r.fillDefaultValue(ctx, adminer); err != nil {
		return ctrl.Result{}, err
	}

	if isExpired(adminer) {
		if err := r.Delete(ctx, adminer); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("delete expired adminer success")
		return ctrl.Result{}, nil
	}

	recLabels := label.RecommendedLabels(&label.Recommended{
		Name:      adminer.Name,
		ManagedBy: label.DefaultManagedBy,
		PartOf:    AdminerPartOf,
	})

	if err := r.syncSecret(ctx, adminer, recLabels); err != nil {
		logger.Error(err, "create secret failed")
		r.recorder.Eventf(adminer, corev1.EventTypeWarning, "Create secret failed", "%v", err)
		return ctrl.Result{}, err
	}

	var hostname string
	if err := r.syncDeployment(ctx, adminer, &hostname, recLabels); err != nil {
		logger.Error(err, "create deployment failed")
		r.recorder.Eventf(adminer, corev1.EventTypeWarning, "Create deployment failed", "%v", err)
		return ctrl.Result{}, err
	}

	if err := r.syncService(ctx, adminer, recLabels); err != nil {
		logger.Error(err, "create service failed")
		r.recorder.Eventf(adminer, corev1.EventTypeWarning, "Create service failed", "%v", err)
		return ctrl.Result{}, err
	}

	if err := r.syncNetworking(ctx, adminer, hostname, recLabels); err != nil {
		logger.Error(err, "create networking failed")
		r.recorder.Eventf(adminer, corev1.EventTypeWarning, "Create networking failed", "%v", err)
		return ctrl.Result{}, err
	}

	// if err := r.waitEndpoints(ctx, adminer); err != nil {
	// 	logger.Error(err, "endpoint wait failed")
	// 	r.recorder.Eventf(adminer, corev1.EventTypeWarning, "endpoint wait failed", "%v", err)
	// 	return ctrl.Result{}, err
	// }

	r.recorder.Eventf(adminer, corev1.EventTypeNormal, "Created", "create adminer success: %v", adminer.Name)
	duration, _ := time.ParseDuration(adminer.Spec.Keepalived)
	return ctrl.Result{RequeueAfter: duration}, nil
}

// func (r *AdminerReconciler) waitEndpoints(ctx context.Context, adminer *adminerv1.Adminer) error {
// 	expectEp := &corev1.Endpoints{
// 		ObjectMeta: metav1.ObjectMeta{
// 			Name:      adminer.Name,
// 			Namespace: adminer.Namespace,
// 		},
// 	}
// 	if err := r.Get(ctx, client.ObjectKeyFromObject(expectEp), expectEp); err != nil {
// 		return err
// 	}
// 	if len(expectEp.Subsets) == 0 {
// 		return fmt.Errorf("endpoints not ready")
// 	}
// 	set := sets.NewString()
// 	for _, subsets := range expectEp.Subsets {
// 		for _, addr := range subsets.Addresses {
// 			if addr.IP != "" && addr.TargetRef != nil && addr.TargetRef.Kind == "Pod" {
// 				set = set.Insert(addr.IP)
// 			}
// 		}
// 	}

// 	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
// 		adminer.Status.AvailableReplicas = int32(set.Len())
// 		return r.Status().Update(ctx, adminer)
// 	})
// }

func (r *AdminerReconciler) syncSecret(ctx context.Context, adminer *adminerv1.Adminer, recLabels map[string]string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      adminer.Name,
			Namespace: adminer.Namespace,
			Labels:    recLabels,
		},
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		expectSecret := r.createSecret(adminer)
		secret.StringData = expectSecret.StringData
		secret.Type = expectSecret.Type
		return controllerutil.SetControllerReference(adminer, secret, r.Scheme)
	}); err != nil {
		return err
	}
	return nil
}

func (r *AdminerReconciler) syncDeployment(ctx context.Context, adminer *adminerv1.Adminer, hostname *string, recLabels map[string]string) error {
	objectMeta := metav1.ObjectMeta{
		Name:      adminer.Name,
		Namespace: adminer.Namespace,
		Labels:    recLabels,
	}

	selector := &metav1.LabelSelector{
		MatchLabels: recLabels,
	}

	templateObjMeta := metav1.ObjectMeta{
		Labels: recLabels,
	}

	containers := []corev1.Container{
		{
			Name:  "adminer",
			Image: r.image,
			Ports: []corev1.ContainerPort{
				{
					Name:          "http",
					Protocol:      corev1.ProtocolTCP,
					ContainerPort: 8080,
				},
			},
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
			VolumeMounts: []corev1.VolumeMount{
				{
					Name: "servers-volume",
					// Note: we cannot use subpath since k8s cannot auto update it's mounts.
					// @see: https://kubernetes.io/docs/concepts/configuration/secret/#mounted-secrets-are-updated-automatically
					MountPath: "/var/www/html/servers",
					// MountPath: "/var/www/html/servers.php",
					// SubPath:   "servers.php",
					ReadOnly: true,
				},
			},
			// probes
			StartupProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/",
						Port: intstr.FromInt(8080),
					},
				},
				InitialDelaySeconds: 1,
				PeriodSeconds:       1,
				FailureThreshold:    30,
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/",
						Port: intstr.FromInt(8080),
					},
				},
				InitialDelaySeconds: 1,
				PeriodSeconds:       1,
				TimeoutSeconds:      1,
			},
		},
	}

	volumes := []corev1.Volume{
		{
			Name: "servers-volume",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: adminer.Name,
					// Items: []corev1.KeyToPath{
					// 	{
					// 		Key:  "servers",
					// 		Path: "servers.php",
					// 	},
					// },
				},
			},
		},
	}

	expectDeployment := &appsv1.Deployment{
		ObjectMeta: objectMeta,
		Spec: appsv1.DeploymentSpec{
			Replicas: &defaultReplicas,
			Selector: selector,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: templateObjMeta,
				Spec: corev1.PodSpec{
					Containers: containers,
					Volumes:    volumes,
				},
			},
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: objectMeta,
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		// only update some specific fields
		deployment.Spec.Replicas = expectDeployment.Spec.Replicas
		deployment.Spec.Selector = expectDeployment.Spec.Selector
		deployment.Spec.Template.ObjectMeta.Labels = expectDeployment.Spec.Template.Labels
		if len(deployment.Spec.Template.Spec.Containers) == 0 {
			deployment.Spec.Template.Spec.Containers = containers
		} else {
			deployment.Spec.Template.Spec.Containers[0].Name = containers[0].Name
			deployment.Spec.Template.Spec.Containers[0].Image = containers[0].Image
			deployment.Spec.Template.Spec.Containers[0].Ports = containers[0].Ports
			deployment.Spec.Template.Spec.Containers[0].Resources = containers[0].Resources
			deployment.Spec.Template.Spec.Containers[0].VolumeMounts = containers[0].VolumeMounts
		}
		if len(deployment.Spec.Template.Spec.Volumes) == 0 {
			deployment.Spec.Template.Spec.Volumes = volumes
		} else {
			deployment.Spec.Template.Spec.Volumes[0].Name = volumes[0].Name
			deployment.Spec.Template.Spec.Volumes[0].VolumeSource = volumes[0].VolumeSource
		}

		if deployment.Spec.Template.Spec.Hostname == "" {
			letterID, err := nanoid.Generate(LetterBytes, HostnameLength)
			if err != nil {
				return err
			}
			// to keep pace with ingress host, hostname must start with a lower case letter
			*hostname = "a" + letterID
			deployment.Spec.Template.Spec.Hostname = *hostname
		} else {
			*hostname = deployment.Spec.Template.Spec.Hostname
		}

		return controllerutil.SetControllerReference(adminer, deployment, r.Scheme)
	}); err != nil {
		return err
	}

	return retryStatusUpdateOnConflict(ctx, r.Client, adminer, func() {
		adminer.Status.AvailableReplicas = deployment.Status.ReadyReplicas
	})
}

func (r *AdminerReconciler) syncService(ctx context.Context, adminer *adminerv1.Adminer, recLabels map[string]string) error {
	expectServiceSpec := corev1.ServiceSpec{
		Selector: recLabels,
		Type:     corev1.ServiceTypeClusterIP,
		Ports: []corev1.ServicePort{
			{Name: "adminer", Port: 8080, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      adminer.Name,
			Namespace: adminer.Namespace,
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
		return controllerutil.SetControllerReference(adminer, service, r.Scheme)
	}); err != nil {
		return err
	}
	return nil
}

func (r *AdminerReconciler) syncNetworking(ctx context.Context, adminer *adminerv1.Adminer, hostname string, recLabels map[string]string) error {
	// 根据配置决定使用 Istio 还是 Ingress
	if r.useIstio && r.istioReconciler != nil {
		return r.syncIstioNetworking(ctx, adminer, hostname, recLabels)
	}

	// 回退到原有的 Ingress 模式
	return r.syncIngress(ctx, adminer, hostname, recLabels)
}

func (r *AdminerReconciler) syncIngress(ctx context.Context, adminer *adminerv1.Adminer, hostname string, recLabels map[string]string) error {
	var err error
	host := hostname + "." + r.adminerDomain
	switch adminer.Spec.IngressType {
	case adminerv1.Nginx:
		err = r.syncNginxIngress(ctx, adminer, host, recLabels)
	}
	return err
}

func (r *AdminerReconciler) syncIstioNetworking(ctx context.Context, adminer *adminerv1.Adminer, hostname string, recLabels map[string]string) error {
	// 🎯 使用智能Gateway的优化网络配置
	if r.istioHelper != nil {
		return r.syncOptimizedIstioNetworking(ctx, adminer, hostname, recLabels)
	}

	// 回退到原有实现（向后兼容）
	if err := r.istioReconciler.SyncIstioNetworking(ctx, adminer, hostname); err != nil {
		return err
	}

	// 更新 Adminer 状态中的域名
	host := hostname + "." + r.adminerDomain
	var protocol string
	if r.tlsEnabled {
		protocol = protocolHTTPS
	} else {
		protocol = protocolHTTP
	}
	domain := protocol + host
	if adminer.Status.Domain != domain {
		return retryStatusUpdateOnConflict(ctx, r.Client, adminer, func() {
			adminer.Status.Domain = domain
		})
	}

	return nil
}

// syncOptimizedIstioNetworking 使用智能Gateway的优化网络配置
func (r *AdminerReconciler) syncOptimizedIstioNetworking(ctx context.Context, adminer *adminerv1.Adminer, hostname string, recLabels map[string]string) error {
	// 构建域名
	host := hostname + "." + r.adminerDomain

	// 🎯 使用通用助手的智能网络配置
	params := &istio.AppNetworkingParams{
		Name:        adminer.Name,
		Namespace:   adminer.Namespace,
		AppType:     "adminer",
		Hosts:       []string{host},
		ServiceName: adminer.Name,
		ServicePort: 8080,
		Protocol:    istio.ProtocolHTTP,

		// 数据库管理器专用配置
		Timeout: &[]time.Duration{86400 * time.Second}[0], // 24小时超时

		// CORS 配置
		CorsPolicy: &istio.CorsPolicy{
			AllowOrigins:     r.buildCorsOrigins(),
			AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
			AllowHeaders:     []string{"content-type", "authorization", "cookie", "x-requested-with"},
			AllowCredentials: true,
		},

		// 安全头部配置（设置为响应头部）
		ResponseHeaders: r.buildSecurityHeaders(),

		// TLS 配置
		TLSEnabled: r.tlsEnabled,

		// 标签和注解
		Labels: recLabels,
		Annotations: map[string]string{
			"sealos.io/converted-from": "adminer-controller",
		},

		// 设置 Owner Reference
		OwnerObject: adminer,
	}

	// 🎯 关键：使用通用助手创建优化的网络配置（自动选择Gateway）
	if err := r.istioHelper.CreateOrUpdateNetworking(ctx, params); err != nil {
		return fmt.Errorf("failed to sync optimized istio networking: %w", err)
	}

	// 🎯 分析域名需求（展示智能Gateway选择过程）
	analysis := r.istioHelper.AnalyzeDomainRequirements(params)

	// 更新 Adminer 状态中的域名和Gateway信息
	var protocol string
	if r.tlsEnabled {
		protocol = protocolHTTPS
	} else {
		protocol = protocolHTTP
	}
	domain := protocol + host

	return retryStatusUpdateOnConflict(ctx, r.Client, adminer, func() {
		adminer.Status.Domain = domain

		// 🎯 添加Gateway优化状态信息
		if adminer.Annotations == nil {
			adminer.Annotations = make(map[string]string)
		}
		adminer.Annotations["sealos.io/gateway-type"] = "optimized"
		adminer.Annotations["sealos.io/domain-type"] = func() string {
			if analysis.IsPublicDomain {
				return "public"
			}
			return "custom"
		}()
		adminer.Annotations["sealos.io/gateway-reference"] = analysis.GatewayReference
	})
}

// buildCorsOrigins 构建CORS源 - 使用精确匹配的adminer子域名
func (r *AdminerReconciler) buildCorsOrigins() []string {
	corsOrigins := []string{}
	if r.tlsEnabled {
		// 添加精确的 adminer 子域名
		corsOrigins = append(corsOrigins, fmt.Sprintf("https://adminer.%s", r.adminerDomain))

		// 如果有配置的公共域名，也添加它们的 adminer 子域名
		if r.istioReconciler != nil && r.istioReconciler.config != nil {
			for _, publicDomain := range r.istioReconciler.config.PublicDomains {
				// 处理通配符域名 (如 *.cloud.sealos.io)
				if len(publicDomain) > 2 && publicDomain[0:2] == "*." {
					baseDomain := publicDomain[2:]
					corsOrigins = append(corsOrigins, fmt.Sprintf("https://adminer.%s", baseDomain))
				} else {
					// 精确域名
					corsOrigins = append(corsOrigins, fmt.Sprintf("https://adminer.%s", publicDomain))
				}
			}
		}
	} else {
		// HTTP 模式
		corsOrigins = append(corsOrigins, fmt.Sprintf("http://adminer.%s", r.adminerDomain))

		if r.istioReconciler != nil && r.istioReconciler.config != nil {
			for _, publicDomain := range r.istioReconciler.config.PublicDomains {
				if len(publicDomain) > 2 && publicDomain[0:2] == "*." {
					baseDomain := publicDomain[2:]
					corsOrigins = append(corsOrigins, fmt.Sprintf("http://adminer.%s", baseDomain))
				} else {
					corsOrigins = append(corsOrigins, fmt.Sprintf("http://adminer.%s", publicDomain))
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

// buildSecurityHeaders 构建安全响应头部
func (r *AdminerReconciler) buildSecurityHeaders() map[string]string {
	headers := make(map[string]string)

	// 设置 X-Frame-Options，允许 iframe 嵌入（响应头部）
	headers["X-Frame-Options"] = ""

	// 设置 Content Security Policy（响应头部）
	cspValue := fmt.Sprintf("default-src * blob: data: *.%s %s; img-src * data: blob: resource: *.%s %s; connect-src * wss: blob: resource:; style-src 'self' 'unsafe-inline' blob: *.%s %s resource:; script-src 'self' 'unsafe-inline' 'unsafe-eval' blob: *.%s %s resource: *.baidu.com *.bdstatic.com; frame-src 'self' %s *.%s mailto: tel: weixin: mtt: *.baidu.com; frame-ancestors 'self' https://%s https://*.%s",
		r.adminerDomain, r.adminerDomain,
		r.adminerDomain, r.adminerDomain,
		r.adminerDomain, r.adminerDomain,
		r.adminerDomain, r.adminerDomain,
		r.adminerDomain, r.adminerDomain,
		r.adminerDomain, r.adminerDomain)

	headers["Content-Security-Policy"] = cspValue

	// 设置 XSS 保护（响应头部）
	headers["X-Xss-Protection"] = "1; mode=block"

	return headers
}

func (r *AdminerReconciler) syncNginxIngress(ctx context.Context, adminer *adminerv1.Adminer, host string, recLabels map[string]string) error {
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      adminer.Name,
			Namespace: adminer.Namespace,
			Labels:    recLabels,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		expectIngress := r.createNginxIngress(adminer, host)
		ingress.ObjectMeta.Annotations = expectIngress.ObjectMeta.Annotations
		ingress.Spec.Rules = expectIngress.Spec.Rules
		ingress.Spec.TLS = expectIngress.Spec.TLS
		return controllerutil.SetControllerReference(adminer, ingress, r.Scheme)
	}); err != nil {
		return err
	}

	protocol := protocolHTTPS
	if !r.tlsEnabled {
		protocol = protocolHTTP
	}

	domain := protocol + host
	if adminer.Status.Domain != domain {
		return retryStatusUpdateOnConflict(ctx, r.Client, adminer, func() {
			adminer.Status.Domain = domain
		})
	}

	return nil
}

func (r *AdminerReconciler) fillDefaultValue(ctx context.Context, adminer *adminerv1.Adminer) error {
	if _, ok := adminer.ObjectMeta.Annotations[KeepaliveAnnotation]; !ok {
		return retryUpdateOnConflict(ctx, r.Client, adminer, func() {
			if adminer.ObjectMeta.Annotations == nil {
				adminer.ObjectMeta.Annotations = make(map[string]string)
			}
			adminer.ObjectMeta.Annotations[KeepaliveAnnotation] = time.Now().Format(time.RFC3339)
		})
	}

	return nil
}

// isExpired return true if the adminer has expired
func isExpired(adminer *adminerv1.Adminer) bool {
	anno := adminer.ObjectMeta.Annotations
	lastUpdateTime, err := time.Parse(time.RFC3339, anno[KeepaliveAnnotation])
	if err != nil {
		// treat parse errors as not expired
		return false
	}

	duration, _ := time.ParseDuration(adminer.Spec.Keepalived)
	return lastUpdateTime.Add(duration).Before(time.Now())
}

func getDomain() string {
	domain := os.Getenv("DOMAIN")
	if domain == "" {
		return DefaultDomain
	}
	return domain
}

func getTLSEnabled() bool {
	tlsEnabled := os.Getenv("TLS_ENABLED")
	if tlsEnabled == "" {
		return true
	}
	return tlsEnabled == "true" || tlsEnabled == "1" || tlsEnabled == "on"
}

func getImage() string {
	image := os.Getenv("IMAGE")
	if image == "" {
		return DefaultImage
	}
	return image
}

func getSecretName() string {
	secretName := os.Getenv("SECRET_NAME")
	if secretName == "" {
		return DefaultSecretName
	}
	return secretName
}

func getSecretNamespace() string {
	secretNamespace := os.Getenv("SECRET_NAMESPACE")
	if secretNamespace == "" {
		return DefaultSecretNamespace
	}
	return secretNamespace
}

// SetupWithManager sets up the controller with the Manager.
func (r *AdminerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("sealos-db-adminer-controller")
	r.adminerDomain = getDomain()
	r.tlsEnabled = getTLSEnabled()
	r.image = getImage()
	r.secretName = getSecretName()
	r.secretNamespace = getSecretNamespace()
	r.Config = mgr.GetConfig()

	// 初始化 Istio 支持
	ctx := context.Background()
	if err := r.SetupIstioSupport(ctx); err != nil {
		r.recorder.Eventf(&adminerv1.Adminer{}, corev1.EventTypeWarning, "IstioSetupFailed", "Failed to setup Istio support: %v", err)
		// 不返回错误，继续使用 Ingress 模式
	}

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&adminerv1.Adminer{}).
		Owns(&appsv1.Deployment{}).Owns(&corev1.Service{}).Owns(&corev1.Secret{}).Owns(&networkingv1.Ingress{})

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
