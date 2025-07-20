/*
Copyright 2023.

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
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v2"
	
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	objectstoragev1 "github/labring/sealos/controllers/objectstorage/api/v1"

	//kbv1alpha1 "github.com/apecloud/kubeblocks/apis/apps/v1alpha1"
	"github.com/go-logr/logr"
	v1 "github.com/labring/sealos/controllers/account/api/v1"
	"github.com/minio/madmin-go/v3"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// NamespaceReconciler reconciles a Namespace object
type NamespaceReconciler struct {
	Client           client.WithWatch
	dynamicClient    dynamic.Interface
	Log              logr.Logger
	Scheme           *runtime.Scheme
	OSAdminClient    *madmin.AdminClient
	OSNamespace      string
	OSAdminSecret    string
	InternalEndpoint string
	
	// 优化相关字段
	resourceCache    *ResourceCache
	suspensionConfig *SuspensionConfig
	strategies       []SuspensionStrategy
	metrics          *SuspensionMetrics
}

// SuspensionStrategy 暂停策略接口
type SuspensionStrategy interface {
	Suspend(ctx context.Context, namespace string) error
	Resume(ctx context.Context, namespace string) error
	IsSupported(resourceType string) bool
	GetName() string
}

// CertManagerStrategy cert-manager资源暂停策略
type CertManagerStrategy struct {
	client        client.Client
	dynamicClient dynamic.Interface
	cache         *ResourceCache
}

// NetworkStrategy 网络资源暂停策略
type NetworkStrategy struct {
	client        client.Client
	dynamicClient dynamic.Interface
	cache         *ResourceCache
}

// RBACStrategy RBAC权限暂停策略
type RBACStrategy struct {
	client client.Client
	cache  *ResourceCache
}

// ResourceCache 资源状态缓存
type ResourceCache struct {
	suspended map[string]map[string]bool // namespace -> resourceType -> suspended
	mutex     sync.RWMutex
	ttl       time.Duration
	lastClean time.Time
}

// SuspensionTransaction 暂停事务记录
type SuspensionTransaction struct {
	Namespace string    `json:"namespace"`
	Status    string    `json:"status"`
	Steps     []string  `json:"steps"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SuspensionConfig 暂停配置
type SuspensionConfig struct {
	Resources map[string]ResourceConfig `yaml:"resources"`
}

// ResourceConfig 资源配置
type ResourceConfig struct {
	GVR              string `yaml:"gvr"`
	Strategy         string `yaml:"strategy"`
	BackupRequired   bool   `yaml:"backup_required"`
	BackupSizeLimit  string `yaml:"backup_size_limit"`
}

// SuspensionMetrics 暂停操作指标
type SuspensionMetrics struct {
	suspensionDuration *prometheus.HistogramVec
	resourceCount      *prometheus.GaugeVec
	operationTotal     *prometheus.CounterVec
	errorTotal         *prometheus.CounterVec
}

const (
	DebtLimit0Name        = "debt-limit0"
	OSAccessKey           = "CONSOLE_ACCESS_KEY"
	OSSecretKey           = "CONSOLE_SECRET_KEY"
	Disabled              = "disabled"
	Enabled               = "enabled"
	OSInternalEndpointEnv = "OSInternalEndpoint"
	OSNamespace           = "OSNamespace"
	OSAdminSecret         = "OSAdminSecret"
	
	// 新增的优化相关常量
	SuspensionConfigMapName = "suspension-config"
	SuspensionConfigMapKey  = "config.yaml"
	TransactionInProgress   = "IN_PROGRESS"
	TransactionCompleted    = "COMPLETED"
	TransactionFailed       = "FAILED"
	CacheCleanupInterval    = 10 * time.Minute
	DefaultCacheTTL         = 5 * time.Minute
	LockTimeout             = 30 * time.Second
	
	// 策略名称
	StrategyCertManager = "cert-manager"
	StrategyNetwork     = "network"
	StrategyRBAC        = "rbac"
)

// 全局Prometheus指标
var (
	suspensionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "debt_suspension_duration_seconds",
			Help: "暂停操作耗时",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"namespace", "operation", "result", "strategy"},
	)
	
	resourceCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "debt_suspended_resources_total",
			Help: "暂停的资源数量",
		},
		[]string{"namespace", "resource_type", "strategy"},
	)
	
	operationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "debt_operations_total",
			Help: "暂停/恢复操作总数",
		},
		[]string{"operation", "result", "strategy"},
	)
	
	errorTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "debt_errors_total",
			Help: "暂停/恢复操作错误总数",
		},
		[]string{"operation", "error_type", "strategy"},
	)
	
	// 默认暂停配置
	defaultSuspensionConfig = &SuspensionConfig{
		Resources: map[string]ResourceConfig{
			"certificates": {
				GVR:             "cert-manager.io/v1/Certificate",
				Strategy:        "mark_suspended",
				BackupRequired:  false,
				BackupSizeLimit: "",
			},
			"challenges": {
				GVR:             "acme.cert-manager.io/v1/Challenge",
				Strategy:        "delete",
				BackupRequired:  false,
				BackupSizeLimit: "",
			},
			"ingresses": {
				GVR:             "networking.k8s.io/v1/Ingress",
				Strategy:        "backup_and_clear",
				BackupRequired:  true,
				BackupSizeLimit: "200KB",
			},
			"services": {
				GVR:             "v1/Service",
				Strategy:        "backup_and_clear",
				BackupRequired:  true,
				BackupSizeLimit: "200KB",
			},
			"gateways": {
				GVR:             "networking.istio.io/v1beta1/Gateway",
				Strategy:        "backup_and_clear",
				BackupRequired:  true,
				BackupSizeLimit: "200KB",
			},
			"virtualservices": {
				GVR:             "networking.istio.io/v1beta1/VirtualService",
				Strategy:        "backup_and_clear",
				BackupRequired:  true,
				BackupSizeLimit: "200KB",
			},
		},
	}
)

//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=namespaces/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=namespaces/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=gateways,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=destinationrules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=acme.cert-manager.io,resources=challenges,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.kubeblocks.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.kubeblocks.io,resources=clusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps.kubeblocks.io,resources=opsrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.kubeblocks.io,resources=opsrequests/status,verbs=get;update;watch
//+kubebuilder:rbac:groups=app.sealos.io,resources=apps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=app.sealos.io,resources=instances,verbs=get;list;watch;create;update;patch;delete

func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("Namespace", req.Namespace, "Name", req.NamespacedName)

	ns := corev1.Namespace{}
	if err := r.Client.Get(ctx, req.NamespacedName, &ns); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if ns.Status.Phase == corev1.NamespaceTerminating {
		logger.V(1).Info("namespace is terminating")
		return ctrl.Result{}, nil
	}

	debtStatus, ok := ns.Annotations[v1.DebtNamespaceAnnoStatusKey]
	if !ok {
		logger.Error(fmt.Errorf("no debt status"), "no debt status")
		return ctrl.Result{}, nil
	}
	logger.V(1).Info("debt status", "status", debtStatus)
	// Skip if namespace is in any completed state
	if debtStatus == v1.SuspendCompletedDebtNamespaceAnnoStatus ||
		debtStatus == v1.FinalDeletionCompletedDebtNamespaceAnnoStatus ||
		debtStatus == v1.ResumeCompletedDebtNamespaceAnnoStatus ||
		debtStatus == v1.TerminateSuspendCompletedDebtNamespaceAnnoStatus {
		logger.V(1).Info("Skipping completed namespace")
		return ctrl.Result{}, nil
	}

	switch debtStatus {
	case v1.SuspendDebtNamespaceAnnoStatus, v1.TerminateSuspendDebtNamespaceAnnoStatus:
		if err := r.SuspendUserResource(ctx, req.NamespacedName.Name); err != nil {
			logger.Error(err, "suspend namespace resources failed")
			return ctrl.Result{}, err
		}
		// Update to corresponding completed state
		newStatus := v1.SuspendCompletedDebtNamespaceAnnoStatus
		if debtStatus == v1.TerminateSuspendDebtNamespaceAnnoStatus {
			newStatus = v1.TerminateSuspendCompletedDebtNamespaceAnnoStatus
		}
		ns.Annotations[v1.DebtNamespaceAnnoStatusKey] = newStatus
		if err := r.Client.Update(ctx, &ns); err != nil {
			logger.Error(err, "update namespace status to completed failed")
			return ctrl.Result{}, err
		}
	case v1.FinalDeletionDebtNamespaceAnnoStatus:
		if err := r.DeleteUserResource(ctx, req.NamespacedName.Name); err != nil {
			logger.Error(err, "delete namespace resources failed")
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: 10 * time.Minute,
			}, err
		}
		ns.Annotations[v1.DebtNamespaceAnnoStatusKey] = v1.FinalDeletionCompletedDebtNamespaceAnnoStatus
		if err := r.Client.Update(ctx, &ns); err != nil {
			logger.Error(err, "update namespace status to FinalDeletionCompleted failed")
			return ctrl.Result{}, err
		}
	case v1.ResumeDebtNamespaceAnnoStatus:
		if err := r.ResumeUserResource(ctx, req.NamespacedName.Name); err != nil {
			logger.Error(err, "resume namespace resources failed")
			return ctrl.Result{}, err
		}
		ns.Annotations[v1.DebtNamespaceAnnoStatusKey] = v1.ResumeCompletedDebtNamespaceAnnoStatus
		if err := r.Client.Update(ctx, &ns); err != nil {
			logger.Error(err, "update namespace status to ResumeCompleted failed")
			return ctrl.Result{}, err
		}
	case v1.NormalDebtNamespaceAnnoStatus:
		// No action needed for Normal state
	default:
		logger.Error(fmt.Errorf("unknown namespace debt status, change to normal"), "", "debt status", ns.Annotations[v1.DebtNamespaceAnnoStatusKey])
		ns.Annotations[v1.DebtNamespaceAnnoStatusKey] = v1.NormalDebtNamespaceAnnoStatus
		if err := r.Client.Update(ctx, &ns); err != nil {
			logger.Error(err, "update namespace status failed")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *NamespaceReconciler) SuspendUserResource(ctx context.Context, namespace string) error {
	return r.suspendWithLockAndMetrics(ctx, namespace, "suspend")
}

// suspendWithLockAndMetrics 带锁和指标的暂停操作
func (r *NamespaceReconciler) suspendWithLockAndMetrics(ctx context.Context, namespace string, operation string) error {
	logger := r.Log.WithValues(
		"operation", operation,
		"namespace", namespace,
		"timestamp", time.Now(),
	)
	
	logger.Info("开始资源暂停操作")
	
	// 记录开始时间用于指标
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		logger.Info("资源暂停操作完成", "duration", duration)
	}()
	
	// 检查幂等性
	if suspended, err := r.isSuspended(ctx, namespace); err != nil {
		logger.Error(err, "检查暂停状态失败")
		errorTotal.WithLabelValues(operation, "idempotency_check", "").Inc()
		return err
	} else if suspended {
		logger.Info("资源已经暂停，跳过操作")
		return nil
	}
	
	// 使用分布式锁
	return r.suspendWithLock(ctx, namespace, operation, func(ctx context.Context) error {
		return r.suspendResourcesWithTransaction(ctx, namespace)
	})
}

// suspendWithLock 使用简化的锁机制执行暂停操作
func (r *NamespaceReconciler) suspendWithLock(ctx context.Context, namespace string, operation string, fn func(context.Context) error) error {
	lockName := fmt.Sprintf("debt-%s-%s", operation, namespace)
	
	// 使用ConfigMap作为分布式锁
	lockConfigMap := &corev1.ConfigMap{
		ObjectMeta: v12.ObjectMeta{
			Name:      lockName,
			Namespace: "sealos-system",
			Labels: map[string]string{
				"debt.sealos.io/lock": "true",
				"debt.sealos.io/operation": operation,
			},
		},
		Data: map[string]string{
			"holder":    fmt.Sprintf("namespace-controller-%s", os.Getenv("HOSTNAME")),
			"timestamp": time.Now().Format(time.RFC3339),
		},
	}
	
	// 尝试获取锁
	err := r.Client.Create(ctx, lockConfigMap)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// 锁已被其他实例持有
			r.Log.Info("操作正在被其他实例执行", "namespace", namespace, "operation", operation)
			return fmt.Errorf("操作正在被其他实例执行")
		}
		return fmt.Errorf("创建分布式锁失败: %w", err)
	}
	
	// 确保释放锁
	defer func() {
		if deleteErr := r.Client.Delete(context.Background(), lockConfigMap); deleteErr != nil {
			r.Log.Error(deleteErr, "释放分布式锁失败", "lockName", lockName)
		}
	}()
	
	// 设置超时
	ctx, cancel := context.WithTimeout(ctx, LockTimeout)
	defer cancel()
	
	// 执行操作
	return fn(ctx)
}

// suspendResourcesWithTransaction 事务性暂停资源
func (r *NamespaceReconciler) suspendResourcesWithTransaction(ctx context.Context, namespace string) error {
	txn := &SuspensionTransaction{
		Namespace: namespace,
		Status:    TransactionInProgress,
		Steps:     []string{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	
	defer func() {
		if txn.Status != TransactionCompleted {
			r.rollbackTransaction(ctx, txn)
		}
	}()
	
	// 使用策略模式并行执行
	return r.executeSuspensionStrategies(ctx, namespace, txn)
}

// executeSuspensionStrategies 执行暂停策略
func (r *NamespaceReconciler) executeSuspensionStrategies(ctx context.Context, namespace string, txn *SuspensionTransaction) error {
	// 初始化策略
	if len(r.strategies) == 0 {
		r.initializeStrategies()
	}
	
	// 并行执行策略 - 第一阶段：cert-manager和网络资源（可并行）
	g1, ctx1 := errgroup.WithContext(ctx)
	
	for _, strategy := range r.strategies {
		strategy := strategy // 避免闭包变量问题
		if strategy.GetName() == StrategyCertManager || strategy.GetName() == StrategyNetwork {
			g1.Go(func() error {
				timer := prometheus.NewTimer(suspensionDuration.WithLabelValues(namespace, "suspend", "", strategy.GetName()))
				defer timer.ObserveDuration()
				
				err := strategy.Suspend(ctx1, namespace)
				
				result := "success"
				if err != nil {
					result = "error"
					errorTotal.WithLabelValues("suspend", "strategy_execution", strategy.GetName()).Inc()
				}
				operationTotal.WithLabelValues("suspend", result, strategy.GetName()).Inc()
				
				if err == nil {
					txn.Steps = append(txn.Steps, fmt.Sprintf("%s_suspended", strategy.GetName()))
				}
				
				return err
			})
		}
	}
	
	if err := g1.Wait(); err != nil {
		txn.Status = TransactionFailed
		txn.Error = err.Error()
		return err
	}
	
	// 第二阶段：RBAC权限（必须在网络资源暂停后执行）
	for _, strategy := range r.strategies {
		if strategy.GetName() == StrategyRBAC {
			timer := prometheus.NewTimer(suspensionDuration.WithLabelValues(namespace, "suspend", "", strategy.GetName()))
			err := strategy.Suspend(ctx, namespace)
			timer.ObserveDuration()
			
			if err != nil {
				txn.Status = TransactionFailed
				txn.Error = err.Error()
				errorTotal.WithLabelValues("suspend", "strategy_execution", strategy.GetName()).Inc()
				return err
			}
			
			txn.Steps = append(txn.Steps, fmt.Sprintf("%s_suspended", strategy.GetName()))
			operationTotal.WithLabelValues("suspend", "success", strategy.GetName()).Inc()
			break
		}
	}
	
	// 第三阶段：其他原有功能（保持向后兼容）
	g2, ctx2 := errgroup.WithContext(ctx)
	
	legacyFunctions := []func(context.Context, string) error{
		r.suspendKBCluster,
		r.suspendOrphanPod,
		r.limitResourceQuotaCreate,
		r.deleteControlledPod,
		r.suspendCronJob,
		r.suspendObjectStorage,
	}
	
	for _, fn := range legacyFunctions {
		fn := fn
		g2.Go(func() error {
			return fn(ctx2, namespace)
		})
	}
	
	if err := g2.Wait(); err != nil {
		txn.Status = TransactionFailed
		txn.Error = err.Error()
		return err
	}
	
	txn.Status = TransactionCompleted
	txn.UpdatedAt = time.Now()
	return nil
}

func (r *NamespaceReconciler) DeleteUserResource(_ context.Context, namespace string) error {
	deleteResources := []string{
		"backup", "cluster.apps.kubeblocks.io", "backupschedules", "devboxes", "devboxreleases", "cronjob",
		"objectstorageuser", "deploy", "sts", "pvc", "Service", "Ingress",
		"Issuer", "Certificate", "HorizontalPodAutoscaler", "instance",
		"job", "app",
	}
	errChan := make(chan error, len(deleteResources))
	for _, rs := range deleteResources {
		go func(resource string) {
			errChan <- deleteResource(r.dynamicClient, resource, namespace)
		}(rs)
	}
	for range deleteResources {
		if err := <-errChan; err != nil {
			return err
		}
	}
	return nil
}

func (r *NamespaceReconciler) ResumeUserResource(ctx context.Context, namespace string) error {
	return r.resumeWithLockAndMetrics(ctx, namespace, "resume")
}

// resumeWithLockAndMetrics 带锁和指标的恢复操作
func (r *NamespaceReconciler) resumeWithLockAndMetrics(ctx context.Context, namespace string, operation string) error {
	logger := r.Log.WithValues(
		"operation", operation,
		"namespace", namespace,
		"timestamp", time.Now(),
	)
	
	logger.Info("开始资源恢复操作")
	
	// 记录开始时间用于指标
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		logger.Info("资源恢复操作完成", "duration", duration)
	}()
	
	// 检查幂等性
	if suspended, err := r.isSuspended(ctx, namespace); err != nil {
		logger.Error(err, "检查暂停状态失败")
		errorTotal.WithLabelValues(operation, "idempotency_check", "").Inc()
		return err
	} else if !suspended {
		logger.Info("资源未被暂停，跳过恢复操作")
		return nil
	}
	
	// 使用分布式锁
	return r.suspendWithLock(ctx, namespace, operation, func(ctx context.Context) error {
		return r.resumeResourcesWithTransaction(ctx, namespace)
	})
}

// resumeResourcesWithTransaction 事务性恢复资源
func (r *NamespaceReconciler) resumeResourcesWithTransaction(ctx context.Context, namespace string) error {
	txn := &SuspensionTransaction{
		Namespace: namespace,
		Status:    TransactionInProgress,
		Steps:     []string{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	
	defer func() {
		if txn.Status != TransactionCompleted {
			// 恢复操作失败时不需要回滚，因为已经是恢复状态
			r.Log.Error(fmt.Errorf("恢复操作失败"), "事务失败", "namespace", txn.Namespace, "steps", txn.Steps)
		}
	}()
	
	// 使用策略模式执行恢复
	return r.executeResumeStrategies(ctx, namespace, txn)
}

// executeResumeStrategies 执行恢复策略
func (r *NamespaceReconciler) executeResumeStrategies(ctx context.Context, namespace string, txn *SuspensionTransaction) error {
	// 初始化策略
	if len(r.strategies) == 0 {
		r.initializeStrategies()
	}
	
	// 第一阶段：RBAC权限恢复（必须首先执行）
	for _, strategy := range r.strategies {
		if strategy.GetName() == StrategyRBAC {
			timer := prometheus.NewTimer(suspensionDuration.WithLabelValues(namespace, "resume", "", strategy.GetName()))
			err := strategy.Resume(ctx, namespace)
			timer.ObserveDuration()
			
			if err != nil {
				txn.Status = TransactionFailed
				txn.Error = err.Error()
				errorTotal.WithLabelValues("resume", "strategy_execution", strategy.GetName()).Inc()
				return err
			}
			
			txn.Steps = append(txn.Steps, fmt.Sprintf("%s_resumed", strategy.GetName()))
			operationTotal.WithLabelValues("resume", "success", strategy.GetName()).Inc()
			break
		}
	}
	
	// 第二阶段：cert-manager和网络资源并行恢复
	g, ctx := errgroup.WithContext(ctx)
	
	for _, strategy := range r.strategies {
		strategy := strategy // 避免闭包变量问题
		if strategy.GetName() == StrategyCertManager || strategy.GetName() == StrategyNetwork {
			g.Go(func() error {
				timer := prometheus.NewTimer(suspensionDuration.WithLabelValues(namespace, "resume", "", strategy.GetName()))
				defer timer.ObserveDuration()
				
				err := strategy.Resume(ctx, namespace)
				
				result := "success"
				if err != nil {
					result = "error"
					errorTotal.WithLabelValues("resume", "strategy_execution", strategy.GetName()).Inc()
				}
				operationTotal.WithLabelValues("resume", result, strategy.GetName()).Inc()
				
				if err == nil {
					txn.Steps = append(txn.Steps, fmt.Sprintf("%s_resumed", strategy.GetName()))
				}
				
				return err
			})
		}
	}
	
	if err := g.Wait(); err != nil {
		txn.Status = TransactionFailed
		txn.Error = err.Error()
		return err
	}
	
	// 第三阶段：其他原有功能（保持向后兼容）
	g2, ctx2 := errgroup.WithContext(ctx)
	
	legacyFunctions := []func(context.Context, string) error{
		r.limitResourceQuotaDelete,
		r.resumePod,
		r.resumeObjectStorage,
	}
	
	for _, fn := range legacyFunctions {
		fn := fn
		g2.Go(func() error {
			return fn(ctx2, namespace)
		})
	}
	
	if err := g2.Wait(); err != nil {
		txn.Status = TransactionFailed
		txn.Error = err.Error()
		return err
	}
	
	txn.Status = TransactionCompleted
	txn.UpdatedAt = time.Now()
	return nil
}

func (r *NamespaceReconciler) limitResourceQuotaCreate(ctx context.Context, namespace string) error {
	limitQuota := GetLimit0ResourceQuota(namespace)
	_, err := ctrl.CreateOrUpdate(ctx, r.Client, limitQuota, func() error {
		return nil
	})
	return err
}

func (r *NamespaceReconciler) limitResourceQuotaDelete(ctx context.Context, namespace string) error {
	limitQuota := GetLimit0ResourceQuota(namespace)
	err := r.Client.Delete(ctx, limitQuota)
	return client.IgnoreNotFound(err)
}

func GetLimit0ResourceQuota(namespace string) *corev1.ResourceQuota {
	quota := corev1.ResourceQuota{}
	quota.Name = "debt-limit0"
	quota.Namespace = namespace
	quota.Spec.Hard = corev1.ResourceList{
		corev1.ResourceLimitsCPU:        resource.MustParse("0"),
		corev1.ResourceLimitsMemory:     resource.MustParse("0"),
		corev1.ResourceRequestsStorage:  resource.MustParse("0"),
		corev1.ResourceEphemeralStorage: resource.MustParse("0"),
		"services.loadbalancers":        resource.MustParse("0"), // 强制禁止 LoadBalancer 服务
	}
	return &quota
}

func (r *NamespaceReconciler) suspendKBCluster(ctx context.Context, namespace string) error {
	logger := r.Log.WithValues("Namespace", namespace, "Function", "suspendKBCluster")

	// Define the GroupVersionResource for KubeBlocks clusters
	clusterGVR := schema.GroupVersionResource{
		Group:    "apps.kubeblocks.io",
		Version:  "v1alpha1",
		Resource: "clusters",
	}

	// List all clusters in the namespace
	clusterList, err := r.dynamicClient.Resource(clusterGVR).Namespace(namespace).List(ctx, v12.ListOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to list clusters in namespace %s: %w", namespace, err)
	}

	// Define the GroupVersionResource for OpsRequests
	opsGVR := schema.GroupVersionResource{
		Group:    "apps.kubeblocks.io",
		Version:  "v1alpha1",
		Resource: "opsrequests",
	}

	// Iterate through each cluster
	for _, cluster := range clusterList.Items {
		clusterName := cluster.GetName()
		logger.V(1).Info("Processing cluster", "Cluster", clusterName)

		// Check if the cluster is already stopped or stopping
		status, exists := cluster.Object["status"]
		if exists && status != nil {
			phase, _ := status.(map[string]interface{})["phase"].(string)
			if phase == "Stopped" || phase == "Stopping" {
				logger.V(1).Info("Cluster already stopped or stopping, skipping", "Cluster", clusterName)
				continue
			}
		}

		// Create OpsRequest resource
		opsName := fmt.Sprintf("stop-%s-%s", clusterName, time.Now().Format("2006-01-02-15"))
		opsRequest := &unstructured.Unstructured{}
		opsRequest.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "apps.kubeblocks.io",
			Version: "v1alpha1",
			Kind:    "OpsRequest",
		})
		opsRequest.SetNamespace(namespace)
		opsRequest.SetName(opsName)

		// Set OpsRequest spec
		opsSpec := map[string]interface{}{
			"clusterRef":             clusterName,
			"type":                   "Stop",
			"ttlSecondsAfterSucceed": int64(1),
			"ttlSecondsBeforeAbort":  int64(60 * 60),
		}
		if err := unstructured.SetNestedField(opsRequest.Object, opsSpec, "spec"); err != nil {
			return fmt.Errorf("failed to set spec for OpsRequest %s in namespace %s: %w", opsName, namespace, err)
		}

		_, err = r.dynamicClient.Resource(opsGVR).Namespace(namespace).Create(ctx, opsRequest, v12.CreateOptions{})
		if err != nil && !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create OpsRequest %s in namespace %s: %w", opsName, namespace, err)
		}
		if errors.IsAlreadyExists(err) {
			logger.V(1).Info("OpsRequest already exists, skipping creation", "OpsRequest", opsName)
		}
	}
	return nil
}

//func (r *NamespaceReconciler) suspendKBCluster(ctx context.Context, namespace string) error {
//	kbClusterList := kbv1alpha1.ClusterList{}
//	if err := r.Client.List(ctx, &kbClusterList, client.InNamespace(namespace)); err != nil {
//		return err
//	}
//	for _, kbCluster := range kbClusterList.Items {
//		if kbCluster.Status.Phase == kbv1alpha1.StoppedClusterPhase || kbCluster.Status.Phase == kbv1alpha1.StoppingClusterPhase {
//			continue
//		}
//		ops := kbv1alpha1.OpsRequest{}
//		ops.Namespace = kbCluster.Namespace
//		ops.ObjectMeta.Name = "stop-" + kbCluster.Name + "-" + time.Now().Format("2006-01-02-15")
//		ops.Spec.TTLSecondsAfterSucceed = 1
//		abort := int32(60 * 60)
//		ops.Spec.TTLSecondsBeforeAbort = &abort
//		ops.Spec.ClusterRef = kbCluster.Name
//		ops.Spec.Type = "Stop"
//		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, &ops, func() error {
//			return nil
//		})
//		if err != nil {
//			r.Log.Error(err, "create ops request failed", "ops", ops.Name, "namespace", ops.Namespace)
//		}
//	}
//	return nil
//}

func (r *NamespaceReconciler) suspendOrphanPod(ctx context.Context, namespace string) error {
	podList := corev1.PodList{}
	if err := r.Client.List(ctx, &podList, client.InNamespace(namespace)); err != nil {
		return err
	}
	for _, pod := range podList.Items {
		if pod.Spec.SchedulerName == v1.DebtSchedulerName || len(pod.ObjectMeta.OwnerReferences) > 0 {
			continue
		}
		clone := pod.DeepCopy()
		clone.ObjectMeta.ResourceVersion = ""
		clone.Spec.NodeName = ""
		clone.Status = corev1.PodStatus{}
		clone.Spec.SchedulerName = v1.DebtSchedulerName
		if clone.Annotations == nil {
			clone.Annotations = make(map[string]string)
		}
		clone.Annotations[v1.PreviousSchedulerName] = pod.Spec.SchedulerName
		err := r.recreatePod(ctx, pod, clone)
		if err != nil {
			return fmt.Errorf("recreate unowned pod `%s` failed: %w", pod.Name, err)
		}
	}
	return nil
}

func (r *NamespaceReconciler) deleteControlledPod(ctx context.Context, namespace string) error {
	podList := corev1.PodList{}
	if err := r.Client.List(ctx, &podList, client.InNamespace(namespace)); err != nil {
		return err
	}
	for _, pod := range podList.Items {
		if pod.Spec.SchedulerName == v1.DebtSchedulerName || len(pod.ObjectMeta.OwnerReferences) == 0 {
			r.Log.Info("skip pod", "pod", pod.Name)
			continue
		}
		r.Log.Info("delete pod", "pod", pod.Name)
		err := r.Client.Delete(ctx, &pod)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *NamespaceReconciler) resumePod(ctx context.Context, namespace string) error {
	var list corev1.PodList
	if err := r.Client.List(ctx, &list, client.InNamespace(namespace)); err != nil {
		return err
	}
	deleteCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for _, pod := range list.Items {
		if pod.Status.Phase != v1.PodPhaseSuspended || pod.Spec.SchedulerName != v1.DebtSchedulerName {
			continue
		}
		if len(pod.ObjectMeta.OwnerReferences) > 0 {
			err := r.Client.Delete(deleteCtx, &pod)
			if err != nil {
				return fmt.Errorf("delete pod %s failed: %v", pod.Name, err)
			}
		} else {
			clone := pod.DeepCopy()
			clone.ObjectMeta.ResourceVersion = ""
			clone.Spec.NodeName = ""
			clone.Status = corev1.PodStatus{}
			if scheduler, ok := clone.Annotations[v1.PreviousSchedulerName]; ok {
				clone.Spec.SchedulerName = scheduler
				delete(clone.Annotations, v1.PreviousSchedulerName)
			} else {
				clone.Spec.SchedulerName = ""
			}
			err := r.recreatePod(deleteCtx, pod, clone)
			if err != nil {
				return fmt.Errorf("recreate unowned pod %s failed: %v", pod.Name, err)
			}
		}
	}
	return nil
}

func (r *NamespaceReconciler) recreatePod(ctx context.Context, oldPod corev1.Pod, newPod *corev1.Pod) error {
	list := corev1.PodList{}
	watcher, err := r.Client.Watch(ctx, &list, client.InNamespace(oldPod.Namespace))
	if err != nil {
		return fmt.Errorf("failed to start watch stream for pod %s: %w", oldPod.Name, err)
	}
	ch := watcher.ResultChan()
	err = r.Client.Delete(ctx, &oldPod)
	if err != nil {
		return fmt.Errorf("failed to delete pod %s: %w", oldPod.Name, err)
	}
	for event := range ch {
		if event.Type == watch.Deleted {
			if val, ok := event.Object.(*corev1.Pod); ok && val.Name == oldPod.Name {
				err = r.Client.Create(ctx, newPod)
				if err != nil {
					return fmt.Errorf("failed to recreate pod %s: %w", newPod.Name, err)
				}
				watcher.Stop()
				break
			}
		}
	}
	return nil
}

func (r *NamespaceReconciler) suspendObjectStorage(ctx context.Context, namespace string) error {
	split := strings.Split(namespace, "-")
	user := split[1]
	err := r.setOSUserStatus(ctx, user, Disabled)
	if err != nil {
		r.Log.Error(err, "failed to suspend object storage", "user", user)
		return err
	}
	return nil
}

func (r *NamespaceReconciler) resumeObjectStorage(ctx context.Context, namespace string) error {
	split := strings.Split(namespace, "-")
	user := split[1]
	err := r.setOSUserStatus(ctx, user, Enabled)
	if err != nil {
		r.Log.Error(err, "failed to resume object storage", "user", user)
		return err
	}
	return nil
}

func (r *NamespaceReconciler) setOSUserStatus(ctx context.Context, user string, status string) error {
	if r.InternalEndpoint == "" || r.OSNamespace == "" || r.OSAdminSecret == "" {
		r.Log.V(1).Info("the endpoint or namespace or admin secret env of object storage is nil")
		return nil
	}
	if r.OSAdminClient == nil {
		secret := &corev1.Secret{}
		if err := r.Client.Get(ctx, client.ObjectKey{Name: r.OSAdminSecret, Namespace: r.OSNamespace}, secret); err != nil {
			r.Log.Error(err, "failed to get secret", "name", r.OSAdminSecret, "namespace", r.OSNamespace)
			return err
		}
		accessKey := string(secret.Data[OSAccessKey])
		secretKey := string(secret.Data[OSSecretKey])
		oSAdminClient, err := objectstoragev1.NewOSAdminClient(r.InternalEndpoint, accessKey, secretKey)
		if err != nil {
			r.Log.Error(err, "failed to new object storage admin client")
			return err
		}
		r.OSAdminClient = oSAdminClient
	}
	users, err := r.OSAdminClient.ListUsers(ctx)
	if err != nil {
		r.Log.Error(err, "failed to list minio user", "user", user)
		return err
	}
	if _, ok := users[user]; !ok {
		return nil
	}
	err = r.OSAdminClient.SetUserStatus(ctx, user, madmin.AccountStatus(status))
	if err != nil {
		r.Log.Error(err, "failed to set user status", "user", user, "status", status)
		return err
	}
	return nil
}

func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager, limitOps controller.Options) error {
	r.Log = ctrl.Log.WithName("controllers").WithName("Namespace")
	r.OSAdminSecret = os.Getenv(OSAdminSecret)
	r.InternalEndpoint = os.Getenv(OSInternalEndpointEnv)
	r.OSNamespace = os.Getenv(OSNamespace)
	config, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("failed to load in-cluster config: %v", err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %v", err)
	}
	r.dynamicClient = dynamicClient
	if r.OSAdminSecret == "" || r.InternalEndpoint == "" || r.OSNamespace == "" {
		r.Log.V(1).Info("failed to get the endpoint or namespace or admin secret env of object storage")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}, builder.WithPredicates(AnnotationChangedPredicate{})).
		WithEventFilter(&AnnotationChangedPredicate{}).
		WithOptions(limitOps).
		Complete(r)
}

type AnnotationChangedPredicate struct {
	predicate.Funcs
}

func (AnnotationChangedPredicate) Update(e event.UpdateEvent) bool {
	oldObj, ok1 := e.ObjectOld.(*corev1.Namespace)
	newObj, ok2 := e.ObjectNew.(*corev1.Namespace)
	if !ok1 || !ok2 || newObj.Annotations == nil {
		return false
	}
	oldStatus := oldObj.Annotations[v1.DebtNamespaceAnnoStatusKey]
	newStatus := newObj.Annotations[v1.DebtNamespaceAnnoStatusKey]
	return oldStatus != newStatus && newStatus != v1.SuspendCompletedDebtNamespaceAnnoStatus &&
		newStatus != v1.FinalDeletionCompletedDebtNamespaceAnnoStatus &&
		newStatus != v1.ResumeCompletedDebtNamespaceAnnoStatus &&
		newStatus != v1.TerminateSuspendCompletedDebtNamespaceAnnoStatus
}

func (AnnotationChangedPredicate) Create(e event.CreateEvent) bool {
	status, ok := e.Object.GetAnnotations()[v1.DebtNamespaceAnnoStatusKey]
	return ok && status != v1.NormalDebtNamespaceAnnoStatus &&
		status != v1.SuspendCompletedDebtNamespaceAnnoStatus &&
		status != v1.FinalDeletionCompletedDebtNamespaceAnnoStatus &&
		status != v1.ResumeCompletedDebtNamespaceAnnoStatus &&
		status != v1.TerminateSuspendCompletedDebtNamespaceAnnoStatus
}

func (r *NamespaceReconciler) suspendCronJob(ctx context.Context, namespace string) error {
	cronJobList := batchv1.CronJobList{}
	if err := r.Client.List(ctx, &cronJobList, client.InNamespace(namespace)); err != nil {
		return err
	}
	for _, cronJob := range cronJobList.Items {
		if cronJob.Spec.Suspend != nil && *cronJob.Spec.Suspend {
			continue
		}
		cronJob.Spec.Suspend = ptr.To(true)
		if err := r.Client.Update(ctx, &cronJob); err != nil {
			return fmt.Errorf("failed to suspend cronjob %s: %w", cronJob.Name, err)
		}
	}
	return nil
}

func (r *NamespaceReconciler) suspendUserPermissions(ctx context.Context, namespace string) error {
	logger := r.Log.WithValues("Namespace", namespace, "Function", "suspendUserPermissions")
	
	// 列出该命名空间中的所有RoleBinding
	roleBindingList := &rbacv1.RoleBindingList{}
	if err := r.Client.List(ctx, roleBindingList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("列出RoleBinding失败: %w", err)
	}
	
	// 需要限制的资源权限
	restrictedResources := []string{
		"services", "ingresses", "gateways", "virtualservices", 
		"destinationrules", "certificates", "challenges",
	}
	
	var processedCount int
	for _, rb := range roleBindingList.Items {
		// 跳过系统级的RoleBinding
		if r.isSystemRoleBinding(&rb) {
			continue
		}
		
		logger.V(1).Info("处理用户RoleBinding", "RoleBinding", rb.Name)
		
		// 检查是否已被暂停
		if rb.Annotations != nil && rb.Annotations["sealos.io/debt-suspended"] == "true" {
			logger.V(1).Info("RoleBinding已被暂停，跳过", "RoleBinding", rb.Name)
			continue
		}
		
		// 备份原始RoleBinding配置
		if err := r.backupRoleBinding(ctx, &rb); err != nil {
			logger.Error(err, "备份RoleBinding失败", "RoleBinding", rb.Name)
			continue
		}
		
		// 创建限制性权限
		if err := r.createRestrictedPermissions(ctx, &rb, restrictedResources); err != nil {
			logger.Error(err, "创建限制性权限失败", "RoleBinding", rb.Name)
			continue
		}
		
		processedCount++
		logger.V(1).Info("已限制用户权限", "RoleBinding", rb.Name)
	}
	
	if processedCount > 0 {
		logger.V(1).Info("用户权限限制完成", "ProcessedCount", processedCount)
	}
	
	return nil
}

func (r *NamespaceReconciler) resumeUserPermissions(ctx context.Context, namespace string) error {
	logger := r.Log.WithValues("Namespace", namespace, "Function", "resumeUserPermissions")
	
	// 列出该命名空间中被暂停的RoleBinding
	roleBindingList := &rbacv1.RoleBindingList{}
	if err := r.Client.List(ctx, roleBindingList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("列出RoleBinding失败: %w", err)
	}
	
	var restoredCount int
	for _, rb := range roleBindingList.Items {
		// 只处理被暂停的RoleBinding
		if rb.Annotations == nil || rb.Annotations["sealos.io/debt-suspended"] != "true" {
			continue
		}
		
		logger.V(1).Info("恢复用户RoleBinding", "RoleBinding", rb.Name)
		
		// 从备份恢复原始权限
		if err := r.restoreRoleBinding(ctx, &rb); err != nil {
			logger.Error(err, "恢复RoleBinding失败", "RoleBinding", rb.Name)
			continue
		}
		
		// 清理限制性Role
		if err := r.cleanupRestrictedRole(ctx, &rb); err != nil {
			logger.Error(err, "清理限制性Role失败", "RoleBinding", rb.Name)
			// 不影响主要恢复流程
		}
		
		restoredCount++
		logger.V(1).Info("已恢复用户权限", "RoleBinding", rb.Name)
	}
	
	if restoredCount > 0 {
		logger.V(1).Info("用户权限恢复完成", "RestoredCount", restoredCount)
	}
	
	return nil
}

func (r *NamespaceReconciler) isSystemRoleBinding(rb *rbacv1.RoleBinding) bool {
	// 跳过系统级和Sealos管理的RoleBinding
	systemPrefixes := []string{
		"system:",
		"cluster-",
		"kubeadm:",
		"sealos-system-",
	}
	
	for _, prefix := range systemPrefixes {
		if strings.HasPrefix(rb.Name, prefix) {
			return true
		}
	}
	
	// 检查是否为Sealos系统Role
	if strings.Contains(rb.RoleRef.Name, "system") || 
	   strings.Contains(rb.RoleRef.Name, "admin") ||
	   strings.Contains(rb.RoleRef.Name, "cluster") {
		return true
	}
	
	return false
}

func (r *NamespaceReconciler) backupRoleBinding(ctx context.Context, rb *rbacv1.RoleBinding) error {
	// 备份原始RoleRef到ConfigMap
	backupData := map[string]interface{}{
		"originalRoleRef": rb.RoleRef,
		"subjects":       rb.Subjects,
		"backupTime":     time.Now().Format(time.RFC3339),
	}
	
	backupJSON, err := json.Marshal(backupData)
	if err != nil {
		return fmt.Errorf("序列化备份数据失败: %w", err)
	}
	
	configMapName := fmt.Sprintf("debt-rbac-backup-%s", rb.Name)
	configMap := &corev1.ConfigMap{
		ObjectMeta: v12.ObjectMeta{
			Name:      configMapName,
			Namespace: rb.Namespace,
			Labels: map[string]string{
				"sealos.io/debt-backup":  "true",
				"sealos.io/backup-type":  "rbac",
				"sealos.io/source-rb":    rb.Name,
			},
		},
		Data: map[string]string{
			"backup": string(backupJSON),
		},
	}
	
	if err := r.Client.Create(ctx, configMap); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("创建备份ConfigMap失败: %w", err)
	}
	
	// 在RoleBinding上添加备份标记
	if rb.Annotations == nil {
		rb.Annotations = make(map[string]string)
	}
	rb.Annotations["sealos.io/debt-suspended"] = "true"
	rb.Annotations["sealos.io/debt-backup-configmap"] = configMapName
	rb.Annotations["sealos.io/debt-suspended-time"] = time.Now().Format(time.RFC3339)
	
	return nil
}

func (r *NamespaceReconciler) createRestrictedPermissions(ctx context.Context, rb *rbacv1.RoleBinding, restrictedResources []string) error {
	// 创建限制性Role名称
	restrictedRoleName := fmt.Sprintf("debt-restricted-%s", rb.RoleRef.Name)
	
	// 定义限制性权限：只允许查看，禁止修改网络资源
	restrictedRole := &rbacv1.Role{
		ObjectMeta: v12.ObjectMeta{
			Name:      restrictedRoleName,
			Namespace: rb.Namespace,
			Labels: map[string]string{
				"sealos.io/debt-restricted": "true",
				"sealos.io/source-role":     rb.RoleRef.Name,
			},
			Annotations: map[string]string{
				"sealos.io/restriction-reason": "Account debt suspension",
				"sealos.io/created-time":       time.Now().Format(time.RFC3339),
			},
		},
	}
	
	// 构建策略规则
	rules := []rbacv1.PolicyRule{
		// 允许查看基本资源
		{
			APIGroups: []string{""},
			Resources: []string{"pods", "configmaps", "secrets", "persistentvolumeclaims"},
			Verbs:     []string{"get", "list", "watch"},
		},
		// 允许查看应用资源
		{
			APIGroups: []string{"apps"},
			Resources: []string{"deployments", "statefulsets", "daemonsets", "replicasets"},
			Verbs:     []string{"get", "list", "watch"},
		},
		// 禁止修改网络资源，只允许查看
		{
			APIGroups: []string{"", "networking.k8s.io", "networking.istio.io", "cert-manager.io"},
			Resources: restrictedResources,
			Verbs:     []string{"get", "list", "watch"},
		},
		// 允许基本的资源操作（非网络相关）
		{
			APIGroups: []string{""},
			Resources: []string{"pods/exec", "pods/log", "pods/portforward"},
			Verbs:     []string{"create"},
		},
	}
	
	restrictedRole.Rules = rules
	
	// 创建限制性Role
	if err := r.Client.Create(ctx, restrictedRole); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("创建限制性Role失败: %w", err)
	}
	
	// 更新RoleBinding指向限制性Role
	rb.RoleRef = rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "Role",
		Name:     restrictedRoleName,
	}
	
	if err := r.Client.Update(ctx, rb); err != nil {
		return fmt.Errorf("更新RoleBinding失败: %w", err)
	}
	
	return nil
}

func (r *NamespaceReconciler) restoreRoleBinding(ctx context.Context, rb *rbacv1.RoleBinding) error {
	// 从ConfigMap恢复原始配置
	configMapName := rb.Annotations["sealos.io/debt-backup-configmap"]
	if configMapName == "" {
		return fmt.Errorf("未找到备份ConfigMap引用")
	}
	
	configMap := &corev1.ConfigMap{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: rb.Namespace}, configMap); err != nil {
		return fmt.Errorf("获取备份ConfigMap失败: %w", err)
	}
	
	backupData := configMap.Data["backup"]
	if backupData == "" {
		return fmt.Errorf("备份数据为空")
	}
	
	var backup map[string]interface{}
	if err := json.Unmarshal([]byte(backupData), &backup); err != nil {
		return fmt.Errorf("解析备份数据失败: %w", err)
	}
	
	// 恢复原始RoleRef
	originalRoleRefJSON, _ := json.Marshal(backup["originalRoleRef"])
	var originalRoleRef rbacv1.RoleRef
	if err := json.Unmarshal(originalRoleRefJSON, &originalRoleRef); err != nil {
		return fmt.Errorf("恢复RoleRef失败: %w", err)
	}
	
	// 恢复原始subjects
	subjectsJSON, _ := json.Marshal(backup["subjects"])
	var subjects []rbacv1.Subject
	if err := json.Unmarshal(subjectsJSON, &subjects); err != nil {
		return fmt.Errorf("恢复Subjects失败: %w", err)
	}
	
	// 更新RoleBinding
	rb.RoleRef = originalRoleRef
	rb.Subjects = subjects
	
	// 移除暂停标记
	delete(rb.Annotations, "sealos.io/debt-suspended")
	delete(rb.Annotations, "sealos.io/debt-backup-configmap")
	delete(rb.Annotations, "sealos.io/debt-suspended-time")
	
	if err := r.Client.Update(ctx, rb); err != nil {
		return fmt.Errorf("更新RoleBinding失败: %w", err)
	}
	
	// 删除备份ConfigMap
	if err := r.Client.Delete(ctx, configMap); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("删除备份ConfigMap失败: %w", err)
	}
	
	return nil
}

func (r *NamespaceReconciler) cleanupRestrictedRole(ctx context.Context, rb *rbacv1.RoleBinding) error {
	// 删除限制性Role
	restrictedRoleName := fmt.Sprintf("debt-restricted-%s", rb.RoleRef.Name)
	
	role := &rbacv1.Role{}
	role.Name = restrictedRoleName
	role.Namespace = rb.Namespace
	
	if err := r.Client.Delete(ctx, role); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("删除限制性Role失败: %w", err)
	}
	
	return nil
}

func (r *NamespaceReconciler) suspendNetworkResources(ctx context.Context, namespace string) error {
	logger := r.Log.WithValues("Namespace", namespace, "Function", "suspendNetworkResources")
	
	// 定义需要暂停的网络资源类型
	networkResources := map[string]schema.GroupVersionResource{
		"Ingress": {
			Group:    "networking.k8s.io",
			Version:  "v1",
			Resource: "ingresses",
		},
		"Service": {
			Group:    "",
			Version:  "v1", 
			Resource: "services",
		},
		"Gateway": {
			Group:    "networking.istio.io",
			Version:  "v1beta1",
			Resource: "gateways",
		},
		"VirtualService": {
			Group:    "networking.istio.io",
			Version:  "v1beta1",
			Resource: "virtualservices",
		},
		"DestinationRule": {
			Group:    "networking.istio.io",
			Version:  "v1beta1",
			Resource: "destinationrules",
		},
	}
	
	// 暂停各类网络资源
	for resourceType, gvr := range networkResources {
		if err := r.suspendNetworkResourceByType(ctx, namespace, resourceType, gvr); err != nil {
			logger.Error(err, "暂停网络资源失败", "ResourceType", resourceType)
			// 继续处理其他资源类型，不因一个失败而停止
		}
	}
	
	logger.V(1).Info("成功暂停网络资源")
	return nil
}

func (r *NamespaceReconciler) suspendNetworkResourceByType(ctx context.Context, namespace string, resourceType string, gvr schema.GroupVersionResource) error {
	logger := r.Log.WithValues("Namespace", namespace, "ResourceType", resourceType)
	
	// 列出该类型的所有资源
	resourceList, err := r.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, v12.ListOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("列出 %s 资源失败: %w", resourceType, err)
	}
	
	if resourceList == nil {
		return nil
	}
	
	var suspendedCount int
	var failedResources []string
	
	// 为每个资源添加暂停注解并备份原始配置
	for _, resource := range resourceList.Items {
		resourceName := resource.GetName()
		
		// 跳过系统级Service（如kube-dns等）
		if resourceType == "Service" && r.isSystemService(resourceName) {
			continue
		}
		
		logger.V(1).Info("暂停网络资源", "Resource", resourceName, "Type", resourceType)
		
		// 检查是否已被暂停
		annotations := resource.GetAnnotations()
		if annotations != nil && annotations["sealos.io/debt-suspended"] == "true" {
			logger.V(1).Info("资源已被暂停，跳过", "Resource", resourceName, "Type", resourceType)
			continue
		}
		
		// 添加暂停注解
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations["sealos.io/debt-suspended"] = "true"
		annotations["sealos.io/debt-suspended-time"] = time.Now().Format(time.RFC3339)
		annotations["sealos.io/debt-resource-type"] = resourceType
		
		resource.SetAnnotations(annotations)
		
		// 根据资源类型进行特定的暂停处理
		if err := r.processNetworkResourceSuspension(ctx, &resource, resourceType); err != nil {
			logger.Error(err, "处理资源暂停失败", "Resource", resourceName, "Type", resourceType)
			failedResources = append(failedResources, resourceName)
			
			// 暂停处理失败时，移除暂停标记以避免状态不一致
			if annotations != nil {
				delete(annotations, "sealos.io/debt-suspended")
				delete(annotations, "sealos.io/debt-suspended-time")
				delete(annotations, "sealos.io/debt-resource-type")
				resource.SetAnnotations(annotations)
			}
			continue
		}
		
		// 更新资源
		if _, err := r.dynamicClient.Resource(gvr).Namespace(namespace).Update(ctx, &resource, v12.UpdateOptions{}); err != nil {
			logger.Error(err, "更新资源暂停状态失败", "Resource", resourceName, "Type", resourceType)
			failedResources = append(failedResources, resourceName)
			continue
		}
		
		suspendedCount++
		logger.V(1).Info("已暂停网络资源", "Resource", resourceName, "Type", resourceType)
	}
	
	// 记录暂停结果
	if len(failedResources) > 0 {
		logger.Error(fmt.Errorf("部分资源暂停失败"), "资源暂停统计", 
			"ResourceType", resourceType, 
			"Suspended", suspendedCount, 
			"Failed", len(failedResources), 
			"FailedResources", failedResources)
		
		// 如果失败的资源超过一定比例，返回错误
		totalResources := suspendedCount + len(failedResources)
		if float64(len(failedResources))/float64(totalResources) > 0.5 {
			return fmt.Errorf("暂停 %s 资源失败率过高: %d/%d", resourceType, len(failedResources), totalResources)
		}
	} else if suspendedCount > 0 {
		logger.V(1).Info("成功暂停所有网络资源", "ResourceType", resourceType, "Count", suspendedCount)
	}
	
	return nil
}

func (r *NamespaceReconciler) processNetworkResourceSuspension(ctx context.Context, resource *unstructured.Unstructured, resourceType string) error {
	logger := r.Log.WithValues("Resource", resource.GetName(), "Type", resourceType)
	
	switch resourceType {
	case "Ingress":
		return r.suspendIngressResource(ctx, resource, logger)
	case "Service":
		return r.suspendServiceResource(ctx, resource, logger)
	case "Gateway":
		return r.suspendGatewayResource(ctx, resource, logger)
	case "VirtualService":
		return r.suspendVirtualServiceResource(ctx, resource, logger)
	case "DestinationRule":
		return r.suspendDestinationRuleResource(ctx, resource, logger)
	}
	
	return nil
}

func (r *NamespaceReconciler) suspendIngressResource(ctx context.Context, resource *unstructured.Unstructured, logger logr.Logger) error {
	// 获取并备份原始rules配置
	if rules, found, err := unstructured.NestedSlice(resource.Object, "spec", "rules"); err == nil && found && len(rules) > 0 {
		if err := r.backupResourceConfig(ctx, resource, "sealos.io/debt-original-hosts", rules, logger); err != nil {
			logger.Error(err, "备份Ingress规则失败，将跳过配置清空")
			return err
		}
		
		// 备份成功后清空Ingress规则以阻止流量
		if err := unstructured.SetNestedSlice(resource.Object, []interface{}{}, "spec", "rules"); err != nil {
			logger.Error(err, "清空Ingress规则失败")
			return fmt.Errorf("清空Ingress规则失败: %w", err)
		}
		
		logger.V(1).Info("已备份并清空Ingress规则")
	} else {
		logger.V(1).Info("Ingress无规则配置或获取失败，跳过处理", "found", found, "error", err)
	}
	
	return nil
}

func (r *NamespaceReconciler) suspendServiceResource(ctx context.Context, resource *unstructured.Unstructured, logger logr.Logger) error {
	// 获取并备份原始ports配置
	if ports, found, err := unstructured.NestedSlice(resource.Object, "spec", "ports"); err == nil && found && len(ports) > 0 {
		if err := r.backupResourceConfig(ctx, resource, "sealos.io/debt-original-ports", ports, logger); err != nil {
			logger.Error(err, "备份Service端口失败，将跳过配置清空")
			return err
		}
		
		// 备份成功后清空Service端口以阻止流量
		if err := unstructured.SetNestedSlice(resource.Object, []interface{}{}, "spec", "ports"); err != nil {
			logger.Error(err, "清空Service端口失败")
			return fmt.Errorf("清空Service端口失败: %w", err)
		}
		
		logger.V(1).Info("已备份并清空Service端口")
	} else {
		logger.V(1).Info("Service无端口配置或获取失败，跳过处理", "found", found, "error", err)
	}
	
	return nil
}

func (r *NamespaceReconciler) suspendGatewayResource(ctx context.Context, resource *unstructured.Unstructured, logger logr.Logger) error {
	// 获取并备份原始servers配置
	if servers, found, err := unstructured.NestedSlice(resource.Object, "spec", "servers"); err == nil && found && len(servers) > 0 {
		if err := r.backupResourceConfig(ctx, resource, "sealos.io/debt-original-servers", servers, logger); err != nil {
			logger.Error(err, "备份Gateway服务器失败，将跳过配置清空")
			return err
		}
		
		// 备份成功后清空Gateway servers以阻止流量
		if err := unstructured.SetNestedSlice(resource.Object, []interface{}{}, "spec", "servers"); err != nil {
			logger.Error(err, "清空Gateway服务器失败")
			return fmt.Errorf("清空Gateway服务器失败: %w", err)
		}
		
		logger.V(1).Info("已备份并清空Gateway服务器")
	} else {
		logger.V(1).Info("Gateway无服务器配置或获取失败，跳过处理", "found", found, "error", err)
	}
	
	return nil
}

func (r *NamespaceReconciler) suspendVirtualServiceResource(ctx context.Context, resource *unstructured.Unstructured, logger logr.Logger) error {
	// 获取并备份原始http配置
	if http, found, err := unstructured.NestedSlice(resource.Object, "spec", "http"); err == nil && found && len(http) > 0 {
		if err := r.backupResourceConfig(ctx, resource, "sealos.io/debt-original-http", http, logger); err != nil {
			logger.Error(err, "备份VirtualService HTTP规则失败，将跳过配置清空")
			return err
		}
		
		// 备份成功后清空VirtualService路由规则以阻止流量
		if err := unstructured.SetNestedSlice(resource.Object, []interface{}{}, "spec", "http"); err != nil {
			logger.Error(err, "清空VirtualService HTTP规则失败")
			return fmt.Errorf("清空VirtualService HTTP规则失败: %w", err)
		}
		
		logger.V(1).Info("已备份并清空VirtualService HTTP规则")
	} else {
		logger.V(1).Info("VirtualService无HTTP规则或获取失败，跳过处理", "found", found, "error", err)
	}
	
	return nil
}

func (r *NamespaceReconciler) suspendDestinationRuleResource(ctx context.Context, resource *unstructured.Unstructured, logger logr.Logger) error {
	// DestinationRule主要影响负载均衡和连接池，暂时只标记暂停
	logger.V(1).Info("DestinationRule已标记为暂停")
	return nil
}

// backupResourceConfig 智能备份资源配置，支持大配置的ConfigMap存储
func (r *NamespaceReconciler) backupResourceConfig(ctx context.Context, resource *unstructured.Unstructured, annotationKey string, config interface{}, logger logr.Logger) error {
	// 序列化配置
	configJSON, err := json.Marshal(config)
	if err != nil {
		logger.Error(err, "序列化资源配置失败")
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	
	// 验证配置数据的有效性
	if err := r.validateBackupData(configJSON); err != nil {
		logger.Error(err, "备份数据验证失败")
		return fmt.Errorf("配置数据无效: %w", err)
	}
	
	configStr := string(configJSON)
	
	// 检查配置大小，annotation限制约为262KB
	const maxAnnotationSize = 200 * 1024 // 200KB，留一些余量
	if len(configStr) > maxAnnotationSize {
		logger.V(1).Info("配置过大，将使用ConfigMap备份", "size", len(configStr))
		return r.backupLargeConfigToConfigMap(ctx, resource, annotationKey, configStr, logger)
	}
	
	// 小配置直接存储到annotation
	annotations := resource.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[annotationKey] = configStr
	resource.SetAnnotations(annotations)
	
	logger.V(1).Info("已将配置备份到annotation", "size", len(configStr))
	return nil
}

// validateBackupData 验证备份数据的有效性
func (r *NamespaceReconciler) validateBackupData(data []byte) error {
	var config interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("JSON格式无效: %w", err)
	}
	
	// 检查是否为空配置
	if config == nil {
		return fmt.Errorf("配置数据为空")
	}
	
	return nil
}

// backupLargeConfigToConfigMap 将大配置备份到ConfigMap
func (r *NamespaceReconciler) backupLargeConfigToConfigMap(ctx context.Context, resource *unstructured.Unstructured, annotationKey string, configData string, logger logr.Logger) error {
	resourceName := resource.GetName()
	namespace := resource.GetNamespace()
	
	// 生成ConfigMap名称
	configMapName := fmt.Sprintf("debt-backup-%s-%s", strings.ToLower(resource.GetKind()), resourceName)
	if len(configMapName) > 63 { // Kubernetes名称长度限制
		configMapName = configMapName[:60] + "..."
	}
	
	// 创建ConfigMap备份配置
	configMap := &corev1.ConfigMap{
		ObjectMeta: v12.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
			Labels: map[string]string{
				"sealos.io/debt-backup":     "true",
				"sealos.io/backup-type":     "network-config",
				"sealos.io/source-resource": resourceName,
			},
			Annotations: map[string]string{
				"sealos.io/backup-time":   time.Now().Format(time.RFC3339),
				"sealos.io/backup-source": fmt.Sprintf("%s/%s", resource.GetKind(), resourceName),
			},
		},
		Data: map[string]string{
			"config": configData,
		},
	}
	
	// 创建或更新ConfigMap
	if err := r.Client.Create(ctx, configMap); err != nil {
		if errors.IsAlreadyExists(err) {
			// ConfigMap已存在，更新它
			existingConfigMap := &corev1.ConfigMap{}
			if err := r.Client.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: namespace}, existingConfigMap); err != nil {
				logger.Error(err, "获取现有ConfigMap失败")
				return fmt.Errorf("获取ConfigMap失败: %w", err)
			}
			
			existingConfigMap.Data["config"] = configData
			existingConfigMap.Annotations["sealos.io/backup-time"] = time.Now().Format(time.RFC3339)
			
			if err := r.Client.Update(ctx, existingConfigMap); err != nil {
				logger.Error(err, "更新ConfigMap失败")
				return fmt.Errorf("更新ConfigMap失败: %w", err)
			}
		} else {
			logger.Error(err, "创建ConfigMap失败")
			return fmt.Errorf("创建ConfigMap失败: %w", err)
		}
	}
	
	// 在resource的annotation中记录ConfigMap引用
	annotations := resource.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[annotationKey+"-configmap"] = configMapName
	resource.SetAnnotations(annotations)
	
	logger.V(1).Info("已将大配置备份到ConfigMap", "configMap", configMapName, "size", len(configData))
	return nil
}

func (r *NamespaceReconciler) isSystemService(serviceName string) bool {
	systemServices := []string{"kubernetes", "kube-dns", "kube-proxy"}
	for _, sysService := range systemServices {
		if serviceName == sysService {
			return true
		}
	}
	return false
}

func (r *NamespaceReconciler) resumeNetworkResources(ctx context.Context, namespace string) error {
	logger := r.Log.WithValues("Namespace", namespace, "Function", "resumeNetworkResources")
	
	// 定义需要恢复的网络资源类型
	networkResources := map[string]schema.GroupVersionResource{
		"Ingress": {
			Group:    "networking.k8s.io",
			Version:  "v1",
			Resource: "ingresses",
		},
		"Service": {
			Group:    "",
			Version:  "v1",
			Resource: "services",
		},
		"Gateway": {
			Group:    "networking.istio.io",
			Version:  "v1beta1",
			Resource: "gateways",
		},
		"VirtualService": {
			Group:    "networking.istio.io",
			Version:  "v1beta1",
			Resource: "virtualservices",
		},
		"DestinationRule": {
			Group:    "networking.istio.io",
			Version:  "v1beta1",
			Resource: "destinationrules",
		},
	}
	
	// 恢复各类网络资源
	for resourceType, gvr := range networkResources {
		if err := r.resumeNetworkResourceByType(ctx, namespace, resourceType, gvr); err != nil {
			logger.Error(err, "恢复网络资源失败", "ResourceType", resourceType)
			// 继续处理其他资源类型，不因一个失败而停止
		}
	}
	
	logger.V(1).Info("成功恢复网络资源")
	return nil
}

func (r *NamespaceReconciler) resumeNetworkResourceByType(ctx context.Context, namespace string, resourceType string, gvr schema.GroupVersionResource) error {
	logger := r.Log.WithValues("Namespace", namespace, "ResourceType", resourceType)
	
	// 列出该类型的所有资源
	resourceList, err := r.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, v12.ListOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("列出 %s 资源失败: %w", resourceType, err)
	}
	
	if resourceList == nil {
		return nil
	}
	
	var resumedCount int
	var failedResources []string
	
	// 恢复每个被暂停的资源
	for _, resource := range resourceList.Items {
		resourceName := resource.GetName()
		
		// 检查是否被暂停
		annotations := resource.GetAnnotations()
		if annotations == nil || annotations["sealos.io/debt-suspended"] != "true" {
			continue
		}
		
		logger.V(1).Info("恢复网络资源", "Resource", resourceName, "Type", resourceType)
		
		// 根据资源类型恢复原始配置
		if err := r.processNetworkResourceResumption(ctx, &resource, resourceType); err != nil {
			logger.Error(err, "处理资源恢复失败", "Resource", resourceName, "Type", resourceType)
			failedResources = append(failedResources, resourceName)
			continue
		}
		
		// 清理备份资源（ConfigMap等）
		if err := r.cleanupBackupResources(ctx, &resource, logger); err != nil {
			logger.Error(err, "清理备份资源失败", "Resource", resourceName, "Type", resourceType)
			// 清理失败不影响主要恢复流程
		}
		
		// 移除暂停相关的注解
		delete(annotations, "sealos.io/debt-suspended")
		delete(annotations, "sealos.io/debt-suspended-time")
		delete(annotations, "sealos.io/debt-resource-type")
		delete(annotations, "sealos.io/debt-original-hosts")
		delete(annotations, "sealos.io/debt-original-ports")
		delete(annotations, "sealos.io/debt-original-servers")
		delete(annotations, "sealos.io/debt-original-http")
		
		// 清理ConfigMap引用注解
		delete(annotations, "sealos.io/debt-original-hosts-configmap")
		delete(annotations, "sealos.io/debt-original-ports-configmap")
		delete(annotations, "sealos.io/debt-original-servers-configmap")
		delete(annotations, "sealos.io/debt-original-http-configmap")
		
		resource.SetAnnotations(annotations)
		
		// 更新资源
		if _, err := r.dynamicClient.Resource(gvr).Namespace(namespace).Update(ctx, &resource, v12.UpdateOptions{}); err != nil {
			logger.Error(err, "更新资源恢复状态失败", "Resource", resourceName, "Type", resourceType)
			failedResources = append(failedResources, resourceName)
			continue
		}
		
		resumedCount++
		logger.V(1).Info("已恢复网络资源", "Resource", resourceName, "Type", resourceType)
	}
	
	// 记录恢复结果
	if len(failedResources) > 0 {
		logger.Error(fmt.Errorf("部分资源恢复失败"), "资源恢复统计", 
			"ResourceType", resourceType, 
			"Resumed", resumedCount, 
			"Failed", len(failedResources), 
			"FailedResources", failedResources)
		
		// 如果失败的资源超过一定比例，返回错误
		totalResources := resumedCount + len(failedResources)
		if float64(len(failedResources))/float64(totalResources) > 0.5 {
			return fmt.Errorf("恢复 %s 资源失败率过高: %d/%d", resourceType, len(failedResources), totalResources)
		}
	} else if resumedCount > 0 {
		logger.V(1).Info("成功恢复所有网络资源", "ResourceType", resourceType, "Count", resumedCount)
	}
	
	return nil
}

func (r *NamespaceReconciler) processNetworkResourceResumption(ctx context.Context, resource *unstructured.Unstructured, resourceType string) error {
	logger := r.Log.WithValues("Resource", resource.GetName(), "Type", resourceType)
	
	switch resourceType {
	case "Ingress":
		return r.resumeIngressResource(ctx, resource, logger)
	case "Service":
		return r.resumeServiceResource(ctx, resource, logger)
	case "Gateway":
		return r.resumeGatewayResource(ctx, resource, logger)
	case "VirtualService":
		return r.resumeVirtualServiceResource(ctx, resource, logger)
	case "DestinationRule":
		return r.resumeDestinationRuleResource(ctx, resource, logger)
	}
	
	return nil
}

func (r *NamespaceReconciler) resumeIngressResource(ctx context.Context, resource *unstructured.Unstructured, logger logr.Logger) error {
	// 尝试从annotation或ConfigMap恢复原始hosts配置
	config, err := r.restoreResourceConfig(ctx, resource, "sealos.io/debt-original-hosts", logger)
	if err != nil {
		logger.Error(err, "恢复Ingress配置失败")
		return err
	}
	
	if config != nil {
		if err := unstructured.SetNestedSlice(resource.Object, config, "spec", "rules"); err != nil {
			logger.Error(err, "设置Ingress规则失败")
			return fmt.Errorf("设置Ingress规则失败: %w", err)
		}
		logger.V(1).Info("已恢复Ingress规则")
	} else {
		logger.V(1).Info("Ingress无备份配置，跳过恢复")
	}
	
	return nil
}

func (r *NamespaceReconciler) resumeServiceResource(ctx context.Context, resource *unstructured.Unstructured, logger logr.Logger) error {
	// 尝试从annotation或ConfigMap恢复原始ports配置
	config, err := r.restoreResourceConfig(ctx, resource, "sealos.io/debt-original-ports", logger)
	if err != nil {
		logger.Error(err, "恢复Service配置失败")
		return err
	}
	
	if config != nil {
		if err := unstructured.SetNestedSlice(resource.Object, config, "spec", "ports"); err != nil {
			logger.Error(err, "设置Service端口失败")
			return fmt.Errorf("设置Service端口失败: %w", err)
		}
		logger.V(1).Info("已恢复Service端口")
	} else {
		logger.V(1).Info("Service无备份配置，跳过恢复")
	}
	
	return nil
}

func (r *NamespaceReconciler) resumeGatewayResource(ctx context.Context, resource *unstructured.Unstructured, logger logr.Logger) error {
	// 尝试从annotation或ConfigMap恢复原始servers配置
	config, err := r.restoreResourceConfig(ctx, resource, "sealos.io/debt-original-servers", logger)
	if err != nil {
		logger.Error(err, "恢复Gateway配置失败")
		return err
	}
	
	if config != nil {
		if err := unstructured.SetNestedSlice(resource.Object, config, "spec", "servers"); err != nil {
			logger.Error(err, "设置Gateway服务器失败")
			return fmt.Errorf("设置Gateway服务器失败: %w", err)
		}
		logger.V(1).Info("已恢复Gateway服务器")
	} else {
		logger.V(1).Info("Gateway无备份配置，跳过恢复")
	}
	
	return nil
}

func (r *NamespaceReconciler) resumeVirtualServiceResource(ctx context.Context, resource *unstructured.Unstructured, logger logr.Logger) error {
	// 尝试从annotation或ConfigMap恢复原始http配置
	config, err := r.restoreResourceConfig(ctx, resource, "sealos.io/debt-original-http", logger)
	if err != nil {
		logger.Error(err, "恢复VirtualService配置失败")
		return err
	}
	
	if config != nil {
		if err := unstructured.SetNestedSlice(resource.Object, config, "spec", "http"); err != nil {
			logger.Error(err, "设置VirtualService HTTP规则失败")
			return fmt.Errorf("设置VirtualService HTTP规则失败: %w", err)
		}
		logger.V(1).Info("已恢复VirtualService HTTP规则")
	} else {
		logger.V(1).Info("VirtualService无备份配置，跳过恢复")
	}
	
	return nil
}

func (r *NamespaceReconciler) resumeDestinationRuleResource(ctx context.Context, resource *unstructured.Unstructured, logger logr.Logger) error {
	// DestinationRule恢复时只需要移除暂停标记
	logger.V(1).Info("DestinationRule已恢复")
	return nil
}

// restoreResourceConfig 智能恢复资源配置，支持从annotation或ConfigMap恢复
func (r *NamespaceReconciler) restoreResourceConfig(ctx context.Context, resource *unstructured.Unstructured, annotationKey string, logger logr.Logger) ([]interface{}, error) {
	annotations := resource.GetAnnotations()
	if annotations == nil {
		return nil, nil
	}
	
	// 首先尝试从annotation恢复
	if configStr, exists := annotations[annotationKey]; exists {
		var config []interface{}
		if err := json.Unmarshal([]byte(configStr), &config); err != nil {
			logger.Error(err, "解析annotation配置失败", "annotationKey", annotationKey)
			return nil, fmt.Errorf("解析annotation配置失败: %w", err)
		}
		
		// 验证恢复的配置
		if err := r.validateRestoredConfig(config); err != nil {
			logger.Error(err, "恢复的配置验证失败")
			return nil, fmt.Errorf("配置验证失败: %w", err)
		}
		
		logger.V(1).Info("从annotation恢复配置", "configSize", len(config))
		return config, nil
	}
	
	// 尝试从ConfigMap恢复
	configMapKey := annotationKey + "-configmap"
	if configMapName, exists := annotations[configMapKey]; exists {
		config, err := r.restoreConfigFromConfigMap(ctx, resource.GetNamespace(), configMapName, logger)
		if err != nil {
			logger.Error(err, "从ConfigMap恢复配置失败", "configMap", configMapName)
			return nil, fmt.Errorf("从ConfigMap恢复失败: %w", err)
		}
		
		// 验证恢复的配置
		if err := r.validateRestoredConfig(config); err != nil {
			logger.Error(err, "从ConfigMap恢复的配置验证失败")
			return nil, fmt.Errorf("配置验证失败: %w", err)
		}
		
		logger.V(1).Info("从ConfigMap恢复配置", "configMap", configMapName, "configSize", len(config))
		return config, nil
	}
	
	// 没有找到备份配置
	return nil, nil
}

// restoreConfigFromConfigMap 从ConfigMap恢复配置
func (r *NamespaceReconciler) restoreConfigFromConfigMap(ctx context.Context, namespace, configMapName string, logger logr.Logger) ([]interface{}, error) {
	configMap := &corev1.ConfigMap{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: namespace}, configMap); err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info("ConfigMap不存在，可能已被清理", "configMap", configMapName)
			return nil, nil
		}
		return nil, fmt.Errorf("获取ConfigMap失败: %w", err)
	}
	
	configData, exists := configMap.Data["config"]
	if !exists {
		return nil, fmt.Errorf("ConfigMap中没有config数据")
	}
	
	var config []interface{}
	if err := json.Unmarshal([]byte(configData), &config); err != nil {
		return nil, fmt.Errorf("解析ConfigMap配置失败: %w", err)
	}
	
	return config, nil
}

// validateRestoredConfig 验证恢复的配置
func (r *NamespaceReconciler) validateRestoredConfig(config []interface{}) error {
	if config == nil {
		return fmt.Errorf("配置为空")
	}
	
	// 基本的配置结构验证
	for i, item := range config {
		if item == nil {
			return fmt.Errorf("配置项 %d 为空", i)
		}
		
		// 检查是否为有效的map结构
		if _, ok := item.(map[string]interface{}); !ok {
			return fmt.Errorf("配置项 %d 不是有效的对象结构", i)
		}
	}
	
	return nil
}

// cleanupBackupResources 清理备份资源（在恢复完成后调用）
func (r *NamespaceReconciler) cleanupBackupResources(ctx context.Context, resource *unstructured.Unstructured, logger logr.Logger) error {
	annotations := resource.GetAnnotations()
	if annotations == nil {
		return nil
	}
	
	// 清理ConfigMap备份
	configMapKeys := []string{
		"sealos.io/debt-original-hosts-configmap",
		"sealos.io/debt-original-ports-configmap",
		"sealos.io/debt-original-servers-configmap",
		"sealos.io/debt-original-http-configmap",
	}
	
	for _, key := range configMapKeys {
		if configMapName, exists := annotations[key]; exists {
			configMap := &corev1.ConfigMap{}
			configMap.Name = configMapName
			configMap.Namespace = resource.GetNamespace()
			
			if err := r.Client.Delete(ctx, configMap); err != nil && !errors.IsNotFound(err) {
				logger.Error(err, "清理备份ConfigMap失败", "configMap", configMapName)
				// 不返回错误，继续清理其他资源
			} else {
				logger.V(1).Info("已清理备份ConfigMap", "configMap", configMapName)
			}
		}
	}
	
	return nil
}

func (r *NamespaceReconciler) suspendCertManagerResources(ctx context.Context, namespace string) error {
	logger := r.Log.WithValues("Namespace", namespace, "Function", "suspendCertManagerResources")
	
	// 定义 cert-manager Certificate 的 GroupVersionResource
	certificateGVR := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}
	
	// 定义 cert-manager Challenge 的 GroupVersionResource
	challengeGVR := schema.GroupVersionResource{
		Group:    "acme.cert-manager.io",
		Version:  "v1",
		Resource: "challenges",
	}
	
	// 列出命名空间中的所有证书
	certificateList, err := r.dynamicClient.Resource(certificateGVR).Namespace(namespace).List(ctx, v12.ListOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("列出命名空间 %s 中的证书失败: %w", namespace, err)
	}
	
	// 暂停证书但保留TLS Secret，避免ACME重新申请的问题
	if certificateList != nil {
		for _, cert := range certificateList.Items {
			certName := cert.GetName()
			logger.V(1).Info("处理证书", "Certificate", certName)
			
			// 检查证书是否已经被暂停
			annotations := cert.GetAnnotations()
			if annotations != nil && annotations["sealos.io/debt-suspended"] == "true" {
				logger.V(1).Info("证书已被暂停，跳过", "Certificate", certName)
				continue
			}
			
			// 添加暂停注解
			if annotations == nil {
				annotations = make(map[string]string)
			}
			annotations["sealos.io/debt-suspended"] = "true"
			annotations["sealos.io/debt-suspended-time"] = time.Now().Format(time.RFC3339)
			
			// 记录原始状态以便恢复
			secretName, found, err := unstructured.NestedString(cert.Object, "spec", "secretName")
			if err == nil && found {
				annotations["sealos.io/debt-original-secret"] = secretName
			}
			
			cert.SetAnnotations(annotations)
			
			// 更新证书（仅添加注解，不删除Secret）
			if _, err := r.dynamicClient.Resource(certificateGVR).Namespace(namespace).Update(ctx, &cert, v12.UpdateOptions{}); err != nil {
				logger.Error(err, "更新证书暂停注解失败", "Certificate", certName)
				continue
			}
			
			logger.V(1).Info("已标记证书为暂停状态（保留TLS Secret）", "Certificate", certName)
		}
	}
	
	// 列出并删除所有活跃的挑战
	challengeList, err := r.dynamicClient.Resource(challengeGVR).Namespace(namespace).List(ctx, v12.ListOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("列出命名空间 %s 中的挑战失败: %w", namespace, err)
	}
	
	if challengeList != nil {
		for _, challenge := range challengeList.Items {
			challengeName := challenge.GetName()
			logger.V(1).Info("删除挑战", "Challenge", challengeName)
			
			if err := r.dynamicClient.Resource(challengeGVR).Namespace(namespace).Delete(ctx, challengeName, v12.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
				logger.Error(err, "删除挑战失败", "Challenge", challengeName)
			}
		}
	}
	
	logger.V(1).Info("成功暂停 cert-manager 资源（保留证书数据）")
	return nil
}

func (r *NamespaceReconciler) resumeCertManagerResources(ctx context.Context, namespace string) error {
	logger := r.Log.WithValues("Namespace", namespace, "Function", "resumeCertManagerResources")
	
	// 定义 cert-manager Certificate 的 GroupVersionResource
	certificateGVR := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}
	
	// 列出命名空间中的所有证书
	certificateList, err := r.dynamicClient.Resource(certificateGVR).Namespace(namespace).List(ctx, v12.ListOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("列出命名空间 %s 中的证书失败: %w", namespace, err)
	}
	
	// 恢复被暂停的证书
	if certificateList != nil {
		for _, cert := range certificateList.Items {
			certName := cert.GetName()
			logger.V(1).Info("处理证书", "Certificate", certName)
			
			// 检查证书是否被暂停
			annotations := cert.GetAnnotations()
			if annotations == nil || annotations["sealos.io/debt-suspended"] != "true" {
				logger.V(1).Info("证书未被暂停，跳过", "Certificate", certName)
				continue
			}
			
			// 移除暂停相关的注解（保留TLS Secret，不强制续期）
			delete(annotations, "sealos.io/debt-suspended")
			delete(annotations, "sealos.io/debt-suspended-time")
			delete(annotations, "sealos.io/debt-original-secret")
			
			cert.SetAnnotations(annotations)
			
			// 更新证书
			if _, err := r.dynamicClient.Resource(certificateGVR).Namespace(namespace).Update(ctx, &cert, v12.UpdateOptions{}); err != nil {
				logger.Error(err, "更新证书恢复注解失败", "Certificate", certName)
				continue
			}
			
			logger.V(1).Info("已恢复证书（保留原有TLS Secret）", "Certificate", certName)
		}
	}
	
	logger.V(1).Info("成功恢复 cert-manager 资源")
	return nil
}

// cleanupOrphanedBackupResources 清理孤立的备份资源（定期维护用）
func (r *NamespaceReconciler) cleanupOrphanedBackupResources(ctx context.Context, namespace string) error {
	logger := r.Log.WithValues("Namespace", namespace, "Function", "cleanupOrphanedBackupResources")
	
	// 列出所有债务备份相关的ConfigMap
	configMapList := &corev1.ConfigMapList{}
	if err := r.Client.List(ctx, configMapList, 
		client.InNamespace(namespace),
		client.MatchingLabels{"sealos.io/debt-backup": "true"}); err != nil {
		return fmt.Errorf("列出备份ConfigMap失败: %w", err)
	}
	
	var cleanedCount int
	for _, cm := range configMapList.Items {
		// 检查对应的资源是否还存在
		sourceResource := cm.Annotations["sealos.io/backup-source"]
		if sourceResource == "" {
			continue
		}
		
		// 解析资源类型和名称
		parts := strings.Split(sourceResource, "/")
		if len(parts) != 2 {
			continue
		}
		
		resourceKind := parts[0]
		resourceName := parts[1]
		
		// 根据资源类型检查是否还存在
		exists, err := r.checkResourceExists(ctx, namespace, resourceKind, resourceName)
		if err != nil {
			logger.Error(err, "检查资源存在性失败", "Resource", sourceResource)
			continue
		}
		
		if !exists {
			// 源资源已删除，清理备份ConfigMap
			if err := r.Client.Delete(ctx, &cm); err != nil && !errors.IsNotFound(err) {
				logger.Error(err, "删除孤立的备份ConfigMap失败", "ConfigMap", cm.Name)
			} else {
				cleanedCount++
				logger.V(1).Info("已清理孤立的备份ConfigMap", "ConfigMap", cm.Name, "SourceResource", sourceResource)
			}
		}
	}
	
	if cleanedCount > 0 {
		logger.V(1).Info("孤立备份资源清理完成", "CleanedCount", cleanedCount)
	}
	
	return nil
}

// checkResourceExists 检查指定资源是否存在
func (r *NamespaceReconciler) checkResourceExists(ctx context.Context, namespace, kind, name string) (bool, error) {
	var gvr schema.GroupVersionResource
	
	switch kind {
	case "Ingress":
		gvr = schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}
	case "Service":
		gvr = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}
	case "Gateway":
		gvr = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1beta1", Resource: "gateways"}
	case "VirtualService":
		gvr = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1beta1", Resource: "virtualservices"}
	default:
		return false, fmt.Errorf("不支持的资源类型: %s", kind)
	}
	
	_, err := r.dynamicClient.Resource(gvr).Namespace(namespace).Get(ctx, name, v12.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	
	return true, nil
}

// migrateBackupDataFormat 迁移备份数据格式（用于版本升级时的数据迁移）
func (r *NamespaceReconciler) migrateBackupDataFormat(ctx context.Context, namespace string) error {
	logger := r.Log.WithValues("Namespace", namespace, "Function", "migrateBackupDataFormat")
	
	// 这个函数可以在将来用于处理备份数据格式的升级迁移
	// 例如：从旧的annotation格式迁移到新的ConfigMap格式
	
	// 示例：查找使用旧格式的资源并迁移到新格式
	// 目前作为预留功能，如果将来需要可以实现具体的迁移逻辑
	
	logger.V(1).Info("备份数据格式迁移检查完成（当前版本无需迁移）")
	return nil
}

func deleteResource(dynamicClient dynamic.Interface, resource, namespace string) error {
	ctx := context.Background()
	deletePolicy := v12.DeletePropagationForeground
	var gvr schema.GroupVersionResource
	switch resource {
	case "backup":
		gvr = schema.GroupVersionResource{Group: "dataprotection.kubeblocks.io", Version: "v1alpha1", Resource: "backups"}
	case "cluster.apps.kubeblocks.io":
		gvr = schema.GroupVersionResource{Group: "apps.kubeblocks.io", Version: "v1alpha1", Resource: "clusters"}
	case "backupschedules":
		gvr = schema.GroupVersionResource{Group: "dataprotection.kubeblocks.io", Version: "v1alpha1", Resource: "backupschedules"}
	case "cronjob":
		gvr = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}
	case "objectstorageuser":
		gvr = schema.GroupVersionResource{Group: "objectstorage.sealos.io", Version: "v1", Resource: "objectstorageusers"}
	case "deploy":
		gvr = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	case "sts":
		gvr = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
	case "pvc":
		gvr = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"}
	case "Service":
		gvr = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}
	case "Ingress":
		gvr = schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}
	case "Issuer":
		gvr = schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "issuers"}
	case "Certificate":
		gvr = schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}
	case "HorizontalPodAutoscaler":
		gvr = schema.GroupVersionResource{Group: "autoscaling", Version: "v1", Resource: "horizontalpodautoscalers"}
	case "instance":
		gvr = schema.GroupVersionResource{Group: "app.sealos.io", Version: "v1", Resource: "instances"}
	case "job":
		gvr = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
	case "app":
		gvr = schema.GroupVersionResource{Group: "app.sealos.io", Version: "v1", Resource: "apps"}
	case "devboxes":
		gvr = schema.GroupVersionResource{Group: "devbox.sealos.io", Version: "v1alpha1", Resource: "devboxes"}
	case "devboxreleases":
		gvr = schema.GroupVersionResource{Group: "devbox.sealos.io", Version: "v1alpha1", Resource: "devboxreleases"}
	default:
		return fmt.Errorf("unknown resource: %s", resource)
	}
	err := dynamicClient.Resource(gvr).Namespace(namespace).DeleteCollection(ctx, v12.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}, v12.ListOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete %s: %v", resource, err)
	}
	return nil
}

// ====================== 优化功能实现 ======================

// initializeStrategies 初始化暂停策略
func (r *NamespaceReconciler) initializeStrategies() {
	if r.resourceCache == nil {
		r.resourceCache = NewResourceCache(DefaultCacheTTL)
	}
	
	if r.suspensionConfig == nil {
		r.suspensionConfig = r.loadSuspensionConfig()
	}
	
	r.strategies = []SuspensionStrategy{
		&CertManagerStrategy{
			client:        r.Client,
			dynamicClient: r.dynamicClient,
			cache:         r.resourceCache,
		},
		&NetworkStrategy{
			client:        r.Client,
			dynamicClient: r.dynamicClient,
			cache:         r.resourceCache,
		},
		&RBACStrategy{
			client: r.Client,
			cache:  r.resourceCache,
		},
	}
}

// loadSuspensionConfig 加载暂停配置
func (r *NamespaceReconciler) loadSuspensionConfig() *SuspensionConfig {
	configMap := &corev1.ConfigMap{}
	err := r.Client.Get(context.Background(), 
		client.ObjectKey{
			Name:      SuspensionConfigMapName,
			Namespace: "sealos-system",
		}, configMap)
	
	if err != nil {
		r.Log.Info("使用默认暂停配置", "error", err.Error())
		return defaultSuspensionConfig
	}
	
	configData, exists := configMap.Data[SuspensionConfigMapKey]
	if !exists {
		r.Log.Info("配置文件不存在，使用默认配置")
		return defaultSuspensionConfig
	}
	
	config := &SuspensionConfig{}
	if err := yaml.Unmarshal([]byte(configData), config); err != nil {
		r.Log.Error(err, "解析配置文件失败，使用默认配置")
		return defaultSuspensionConfig
	}
	
	return config
}

// isSuspended 检查幂等性
func (r *NamespaceReconciler) isSuspended(ctx context.Context, namespace string) (bool, error) {
	if r.resourceCache == nil {
		r.resourceCache = NewResourceCache(DefaultCacheTTL)
	}
	
	// 先检查缓存
	if suspended, found := r.resourceCache.IsSuspended(namespace, "all"); found {
		return suspended, nil
	}
	
	// 检查namespace注解
	ns := &corev1.Namespace{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: namespace}, ns)
	if err != nil {
		return false, err
	}
	
	suspended := ns.Annotations[v1.DebtNamespaceAnnoStatusKey] == v1.SuspendCompletedDebtNamespaceAnnoStatus ||
		ns.Annotations[v1.DebtNamespaceAnnoStatusKey] == v1.TerminateSuspendCompletedDebtNamespaceAnnoStatus
	
	// 更新缓存
	r.resourceCache.SetSuspended(namespace, "all", suspended)
	
	return suspended, nil
}

// rollbackTransaction 事务回滚
func (r *NamespaceReconciler) rollbackTransaction(ctx context.Context, txn *SuspensionTransaction) {
	logger := r.Log.WithValues("namespace", txn.Namespace, "transaction", txn.Status)
	logger.Info("开始事务回滚", "steps", txn.Steps)
	
	// 按相反顺序回滚已完成的步骤
	for i := len(txn.Steps) - 1; i >= 0; i-- {
		step := txn.Steps[i]
		if err := r.rollbackStep(ctx, txn.Namespace, step); err != nil {
			logger.Error(err, "回滚步骤失败", "step", step)
			errorTotal.WithLabelValues("rollback", "step_failure", "").Inc()
		}
	}
	
	// 清理缓存
	if r.resourceCache != nil {
		r.resourceCache.ClearNamespace(txn.Namespace)
	}
}

// rollbackStep 回滚单个步骤
func (r *NamespaceReconciler) rollbackStep(ctx context.Context, namespace, step string) error {
	parts := strings.Split(step, "_")
	if len(parts) != 2 || parts[1] != "suspended" {
		return nil
	}
	
	strategyName := parts[0]
	for _, strategy := range r.strategies {
		if strategy.GetName() == strategyName {
			return strategy.Resume(ctx, namespace)
		}
	}
	
	return nil
}

// ====================== 缓存实现 ======================

// NewResourceCache 创建新的资源缓存
func NewResourceCache(ttl time.Duration) *ResourceCache {
	return &ResourceCache{
		suspended: make(map[string]map[string]bool),
		ttl:       ttl,
		lastClean: time.Now(),
	}
}

// IsSuspended 检查资源是否已暂停
func (rc *ResourceCache) IsSuspended(namespace, resourceType string) (bool, bool) {
	rc.mutex.RLock()
	defer rc.mutex.RUnlock()
	
	if nsCache, exists := rc.suspended[namespace]; exists {
		if suspended, exists := nsCache[resourceType]; exists {
			return suspended, true
		}
	}
	
	return false, false
}

// SetSuspended 设置资源暂停状态
func (rc *ResourceCache) SetSuspended(namespace, resourceType string, suspended bool) {
	rc.mutex.Lock()
	defer rc.mutex.Unlock()
	
	if rc.suspended[namespace] == nil {
		rc.suspended[namespace] = make(map[string]bool)
	}
	
	rc.suspended[namespace][resourceType] = suspended
	
	// 定期清理缓存
	if time.Since(rc.lastClean) > CacheCleanupInterval {
		go rc.cleanup()
	}
}

// ClearNamespace 清理命名空间缓存
func (rc *ResourceCache) ClearNamespace(namespace string) {
	rc.mutex.Lock()
	defer rc.mutex.Unlock()
	
	delete(rc.suspended, namespace)
}

// cleanup 清理过期缓存
func (rc *ResourceCache) cleanup() {
	rc.mutex.Lock()
	defer rc.mutex.Unlock()
	
	// 这里可以添加更复杂的缓存清理逻辑
	// 目前简单清理所有缓存
	rc.suspended = make(map[string]map[string]bool)
	rc.lastClean = time.Now()
}

// ====================== CertManagerStrategy 实现 ======================

// GetName 获取策略名称
func (s *CertManagerStrategy) GetName() string {
	return StrategyCertManager
}

// IsSupported 检查是否支持指定资源类型
func (s *CertManagerStrategy) IsSupported(resourceType string) bool {
	return resourceType == "Certificate" || resourceType == "Challenge"
}

// Suspend 暂停cert-manager资源
func (s *CertManagerStrategy) Suspend(ctx context.Context, namespace string) error {
	// 检查缓存
	if suspended, found := s.cache.IsSuspended(namespace, StrategyCertManager); found && suspended {
		return nil
	}
	
	g, ctx := errgroup.WithContext(ctx)
	
	// 暂停Certificate资源
	g.Go(func() error {
		return s.suspendCertificates(ctx, namespace)
	})
	
	// 删除Challenge资源
	g.Go(func() error {
		return s.deleteChallenges(ctx, namespace)
	})
	
	if err := g.Wait(); err != nil {
		return err
	}
	
	// 更新缓存
	s.cache.SetSuspended(namespace, StrategyCertManager, true)
	resourceCount.WithLabelValues(namespace, "Certificate", StrategyCertManager).Inc()
	
	return nil
}

// Resume 恢复cert-manager资源
func (s *CertManagerStrategy) Resume(ctx context.Context, namespace string) error {
	// 恢复Certificate资源
	if err := s.resumeCertificates(ctx, namespace); err != nil {
		return err
	}
	
	// 更新缓存
	s.cache.SetSuspended(namespace, StrategyCertManager, false)
	resourceCount.WithLabelValues(namespace, "Certificate", StrategyCertManager).Dec()
	
	return nil
}

// suspendCertificates 暂停证书资源
func (s *CertManagerStrategy) suspendCertificates(ctx context.Context, namespace string) error {
	gvr := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}
	
	resources, err := s.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, v12.ListOptions{})
	if err != nil {
		return err
	}
	
	for _, cert := range resources.Items {
		// 标记为暂停状态而不是删除
		annotations := cert.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations["debt.sealos.io/suspended"] = "true"
		annotations["debt.sealos.io/suspended-at"] = time.Now().Format(time.RFC3339)
		
		cert.SetAnnotations(annotations)
		
		if _, err := s.dynamicClient.Resource(gvr).Namespace(namespace).Update(ctx, &cert, v12.UpdateOptions{}); err != nil {
			return err
		}
	}
	
	return nil
}

// deleteChallenges 删除Challenge资源
func (s *CertManagerStrategy) deleteChallenges(ctx context.Context, namespace string) error {
	gvr := schema.GroupVersionResource{
		Group:    "acme.cert-manager.io",
		Version:  "v1",
		Resource: "challenges",
	}
	
	deletePolicy := v12.DeletePropagationForeground
	return s.dynamicClient.Resource(gvr).Namespace(namespace).DeleteCollection(ctx, v12.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}, v12.ListOptions{})
}

// resumeCertificates 恢复证书资源
func (s *CertManagerStrategy) resumeCertificates(ctx context.Context, namespace string) error {
	gvr := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}
	
	resources, err := s.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, v12.ListOptions{})
	if err != nil {
		return err
	}
	
	for _, cert := range resources.Items {
		annotations := cert.GetAnnotations()
		if annotations != nil && annotations["debt.sealos.io/suspended"] == "true" {
			// 移除暂停标记
			delete(annotations, "debt.sealos.io/suspended")
			delete(annotations, "debt.sealos.io/suspended-at")
			cert.SetAnnotations(annotations)
			
			if _, err := s.dynamicClient.Resource(gvr).Namespace(namespace).Update(ctx, &cert, v12.UpdateOptions{}); err != nil {
				return err
			}
		}
	}
	
	return nil
}

// ====================== NetworkStrategy 实现 ======================

// GetName 获取策略名称
func (s *NetworkStrategy) GetName() string {
	return StrategyNetwork
}

// IsSupported 检查是否支持指定资源类型
func (s *NetworkStrategy) IsSupported(resourceType string) bool {
	supportedTypes := []string{"Ingress", "Service", "Gateway", "VirtualService"}
	for _, t := range supportedTypes {
		if t == resourceType {
			return true
		}
	}
	return false
}

// Suspend 暂停网络资源
func (s *NetworkStrategy) Suspend(ctx context.Context, namespace string) error {
	// 检查缓存
	if suspended, found := s.cache.IsSuspended(namespace, StrategyNetwork); found && suspended {
		return nil
	}
	
	networkGVRs := []schema.GroupVersionResource{
		{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
		{Group: "", Version: "v1", Resource: "services"},
		{Group: "networking.istio.io", Version: "v1beta1", Resource: "gateways"},
		{Group: "networking.istio.io", Version: "v1beta1", Resource: "virtualservices"},
	}
	
	g, ctx := errgroup.WithContext(ctx)
	
	for _, gvr := range networkGVRs {
		gvr := gvr
		g.Go(func() error {
			return s.suspendResourcesByGVR(ctx, namespace, gvr)
		})
	}
	
	if err := g.Wait(); err != nil {
		return err
	}
	
	// 更新缓存
	s.cache.SetSuspended(namespace, StrategyNetwork, true)
	resourceCount.WithLabelValues(namespace, "Network", StrategyNetwork).Inc()
	
	return nil
}

// Resume 恢复网络资源
func (s *NetworkStrategy) Resume(ctx context.Context, namespace string) error {
	networkGVRs := []schema.GroupVersionResource{
		{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
		{Group: "", Version: "v1", Resource: "services"},
		{Group: "networking.istio.io", Version: "v1beta1", Resource: "gateways"},
		{Group: "networking.istio.io", Version: "v1beta1", Resource: "virtualservices"},
	}
	
	g, ctx := errgroup.WithContext(ctx)
	
	for _, gvr := range networkGVRs {
		gvr := gvr
		g.Go(func() error {
			return s.resumeResourcesByGVR(ctx, namespace, gvr)
		})
	}
	
	if err := g.Wait(); err != nil {
		return err
	}
	
	// 更新缓存
	s.cache.SetSuspended(namespace, StrategyNetwork, false)
	resourceCount.WithLabelValues(namespace, "Network", StrategyNetwork).Dec()
	
	return nil
}

// suspendResourcesByGVR 按GVR暂停资源
func (s *NetworkStrategy) suspendResourcesByGVR(ctx context.Context, namespace string, gvr schema.GroupVersionResource) error {
	resources, err := s.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, v12.ListOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	
	for _, resource := range resources.Items {
		if err := s.backupAndClearResource(ctx, namespace, &resource, gvr); err != nil {
			return err
		}
	}
	
	return nil
}

// resumeResourcesByGVR 按GVR恢复资源
func (s *NetworkStrategy) resumeResourcesByGVR(ctx context.Context, namespace string, gvr schema.GroupVersionResource) error {
	resources, err := s.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, v12.ListOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	
	for _, resource := range resources.Items {
		if err := s.restoreResource(ctx, namespace, &resource, gvr); err != nil {
			return err
		}
	}
	
	return nil
}

// backupAndClearResource 备份并清空资源配置
func (s *NetworkStrategy) backupAndClearResource(ctx context.Context, namespace string, resource *unstructured.Unstructured, gvr schema.GroupVersionResource) error {
	// 获取需要备份的字段
	spec, found, err := unstructured.NestedMap(resource.Object, "spec")
	if err != nil || !found {
		return nil
	}
	
	// 创建备份数据
	backup := map[string]interface{}{
		"spec": spec,
		"metadata": map[string]interface{}{
			"labels":      resource.GetLabels(),
			"annotations": resource.GetAnnotations(),
		},
	}
	
	backupJSON, err := json.Marshal(backup)
	if err != nil {
		return err
	}
	
	// 检查备份大小
	const maxAnnotationSize = 200 * 1024 // 200KB
	
	annotations := resource.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	
	if len(backupJSON) > maxAnnotationSize {
		// 使用ConfigMap存储大的备份数据
		if err := s.storeBackupInConfigMap(ctx, namespace, resource.GetName(), gvr.Resource, backupJSON); err != nil {
			return err
		}
		annotations["debt.sealos.io/backup-location"] = "configmap"
		annotations["debt.sealos.io/backup-configmap"] = fmt.Sprintf("%s-%s-backup", resource.GetName(), gvr.Resource)
	} else {
		// 使用注解存储小的备份数据
		annotations["debt.sealos.io/backup-data"] = string(backupJSON)
		annotations["debt.sealos.io/backup-location"] = "annotation"
	}
	
	annotations["debt.sealos.io/suspended"] = "true"
	annotations["debt.sealos.io/suspended-at"] = time.Now().Format(time.RFC3339)
	
	// 清空spec但保留备份信息
	resource.SetAnnotations(annotations)
	
	// 根据资源类型清空相关配置
	switch gvr.Resource {
	case "ingresses":
		unstructured.SetNestedMap(resource.Object, map[string]interface{}{}, "spec")
	case "services":
		// 保留ClusterIP但清空其他配置
		clusterIP, _, _ := unstructured.NestedString(resource.Object, "spec", "clusterIP")
		newSpec := map[string]interface{}{
			"ports": []interface{}{},
		}
		if clusterIP != "" && clusterIP != "None" {
			newSpec["clusterIP"] = clusterIP
		}
		unstructured.SetNestedMap(resource.Object, newSpec, "spec")
	case "gateways", "virtualservices":
		unstructured.SetNestedMap(resource.Object, map[string]interface{}{}, "spec")
	}
	
	_, err = s.dynamicClient.Resource(gvr).Namespace(namespace).Update(ctx, resource, v12.UpdateOptions{})
	return err
}

// restoreResource 恢复资源配置
func (s *NetworkStrategy) restoreResource(ctx context.Context, namespace string, resource *unstructured.Unstructured, gvr schema.GroupVersionResource) error {
	annotations := resource.GetAnnotations()
	if annotations == nil || annotations["debt.sealos.io/suspended"] != "true" {
		return nil // 资源未被暂停
	}
	
	var backupData []byte
	var err error
	
	backupLocation := annotations["debt.sealos.io/backup-location"]
	if backupLocation == "configmap" {
		configMapName := annotations["debt.sealos.io/backup-configmap"]
		backupData, err = s.loadBackupFromConfigMap(ctx, namespace, configMapName)
		if err != nil {
			return err
		}
	} else {
		backupData = []byte(annotations["debt.sealos.io/backup-data"])
	}
	
	// 恢复备份数据
	var backup map[string]interface{}
	if err := json.Unmarshal(backupData, &backup); err != nil {
		return err
	}
	
	// 恢复spec
	if spec, exists := backup["spec"]; exists {
		unstructured.SetNestedMap(resource.Object, spec.(map[string]interface{}), "spec")
	}
	
	// 恢复metadata（除了暂停相关的注解）
	if metadata, exists := backup["metadata"]; exists {
		metadataMap := metadata.(map[string]interface{})
		if labels, exists := metadataMap["labels"]; exists && labels != nil {
			resource.SetLabels(labels.(map[string]string))
		}
		if backupAnnotations, exists := metadataMap["annotations"]; exists && backupAnnotations != nil {
			for k, v := range backupAnnotations.(map[string]string) {
				annotations[k] = v
			}
		}
	}
	
	// 清理暂停相关的注解
	delete(annotations, "debt.sealos.io/suspended")
	delete(annotations, "debt.sealos.io/suspended-at")
	delete(annotations, "debt.sealos.io/backup-data")
	delete(annotations, "debt.sealos.io/backup-location")
	delete(annotations, "debt.sealos.io/backup-configmap")
	
	resource.SetAnnotations(annotations)
	
	// 更新资源
	_, err = s.dynamicClient.Resource(gvr).Namespace(namespace).Update(ctx, resource, v12.UpdateOptions{})
	if err != nil {
		return err
	}
	
	// 清理ConfigMap备份
	if backupLocation == "configmap" {
		configMapName := fmt.Sprintf("%s-%s-backup", resource.GetName(), gvr.Resource)
		s.deleteBackupConfigMap(ctx, namespace, configMapName)
	}
	
	return nil
}

// storeBackupInConfigMap 在ConfigMap中存储备份数据
func (s *NetworkStrategy) storeBackupInConfigMap(ctx context.Context, namespace, resourceName, resourceType string, backup []byte) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: v12.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-backup", resourceName, resourceType),
			Namespace: namespace,
			Labels: map[string]string{
				"debt.sealos.io/backup":       "true",
				"debt.sealos.io/resource":     resourceName,
				"debt.sealos.io/resource-type": resourceType,
			},
		},
		Data: map[string]string{
			"backup.json": string(backup),
		},
	}
	
	return s.client.Create(ctx, configMap)
}

// loadBackupFromConfigMap 从ConfigMap加载备份数据
func (s *NetworkStrategy) loadBackupFromConfigMap(ctx context.Context, namespace, configMapName string) ([]byte, error) {
	configMap := &corev1.ConfigMap{}
	err := s.client.Get(ctx, client.ObjectKey{
		Name:      configMapName,
		Namespace: namespace,
	}, configMap)
	
	if err != nil {
		return nil, err
	}
	
	return []byte(configMap.Data["backup.json"]), nil
}

// deleteBackupConfigMap 删除备份ConfigMap
func (s *NetworkStrategy) deleteBackupConfigMap(ctx context.Context, namespace, configMapName string) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: v12.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
	}
	// 忽略删除错误
	s.client.Delete(ctx, configMap)
}

// ====================== RBACStrategy 实现 ======================

// GetName 获取策略名称
func (s *RBACStrategy) GetName() string {
	return StrategyRBAC
}

// IsSupported 检查是否支持指定资源类型
func (s *RBACStrategy) IsSupported(resourceType string) bool {
	return resourceType == "Role" || resourceType == "RoleBinding"
}

// Suspend 暂停RBAC权限
func (s *RBACStrategy) Suspend(ctx context.Context, namespace string) error {
	// 检查缓存
	if suspended, found := s.cache.IsSuspended(namespace, StrategyRBAC); found && suspended {
		return nil
	}
	
	if err := s.createRestrictedRole(ctx, namespace); err != nil {
		return err
	}
	
	if err := s.backupAndModifyRoleBindings(ctx, namespace); err != nil {
		return err
	}
	
	// 更新缓存
	s.cache.SetSuspended(namespace, StrategyRBAC, true)
	resourceCount.WithLabelValues(namespace, "RBAC", StrategyRBAC).Inc()
	
	return nil
}

// Resume 恢复RBAC权限
func (s *RBACStrategy) Resume(ctx context.Context, namespace string) error {
	if err := s.restoreRoleBindings(ctx, namespace); err != nil {
		return err
	}
	
	if err := s.deleteRestrictedRole(ctx, namespace); err != nil {
		return err
	}
	
	// 更新缓存
	s.cache.SetSuspended(namespace, StrategyRBAC, false)
	resourceCount.WithLabelValues(namespace, "RBAC", StrategyRBAC).Dec()
	
	return nil
}

// createRestrictedRole 创建受限角色
func (s *RBACStrategy) createRestrictedRole(ctx context.Context, namespace string) error {
	restrictedRole := &rbacv1.Role{
		ObjectMeta: v12.ObjectMeta{
			Name:      "debt-restricted-role",
			Namespace: namespace,
			Labels: map[string]string{
				"debt.sealos.io/restricted": "true",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods", "services", "configmaps", "secrets"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"networking.k8s.io"},
				Resources: []string{"ingresses"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"networking.istio.io"},
				Resources: []string{"gateways", "virtualservices"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
	
	return s.client.Create(ctx, restrictedRole)
}

// deleteRestrictedRole 删除受限角色
func (s *RBACStrategy) deleteRestrictedRole(ctx context.Context, namespace string) error {
	restrictedRole := &rbacv1.Role{
		ObjectMeta: v12.ObjectMeta{
			Name:      "debt-restricted-role",
			Namespace: namespace,
		},
	}
	
	return client.IgnoreNotFound(s.client.Delete(ctx, restrictedRole))
}

// backupAndModifyRoleBindings 备份并修改RoleBinding
func (s *RBACStrategy) backupAndModifyRoleBindings(ctx context.Context, namespace string) error {
	roleBindings := &rbacv1.RoleBindingList{}
	err := s.client.List(ctx, roleBindings, client.InNamespace(namespace))
	if err != nil {
		return err
	}
	
	for _, rb := range roleBindings.Items {
		// 跳过系统角色绑定
		if strings.HasPrefix(rb.Name, "system:") {
			continue
		}
		
		// 备份原始角色引用
		annotations := rb.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		
		originalRoleRef, _ := json.Marshal(rb.RoleRef)
		annotations["debt.sealos.io/original-role-ref"] = string(originalRoleRef)
		annotations["debt.sealos.io/suspended"] = "true"
		
		// 修改为受限角色
		rb.RoleRef = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "debt-restricted-role",
		}
		
		rb.SetAnnotations(annotations)
		
		if err := s.client.Update(ctx, &rb); err != nil {
			return err
		}
	}
	
	return nil
}

// restoreRoleBindings 恢复RoleBinding
func (s *RBACStrategy) restoreRoleBindings(ctx context.Context, namespace string) error {
	roleBindings := &rbacv1.RoleBindingList{}
	err := s.client.List(ctx, roleBindings, client.InNamespace(namespace))
	if err != nil {
		return err
	}
	
	for _, rb := range roleBindings.Items {
		annotations := rb.GetAnnotations()
		if annotations == nil || annotations["debt.sealos.io/suspended"] != "true" {
			continue
		}
		
		// 恢复原始角色引用
		originalRoleRefStr := annotations["debt.sealos.io/original-role-ref"]
		if originalRoleRefStr != "" {
			var originalRoleRef rbacv1.RoleRef
			if err := json.Unmarshal([]byte(originalRoleRefStr), &originalRoleRef); err == nil {
				rb.RoleRef = originalRoleRef
			}
		}
		
		// 清理注解
		delete(annotations, "debt.sealos.io/original-role-ref")
		delete(annotations, "debt.sealos.io/suspended")
		rb.SetAnnotations(annotations)
		
		if err := s.client.Update(ctx, &rb); err != nil {
			return err
		}
	}
	
	return nil
}
