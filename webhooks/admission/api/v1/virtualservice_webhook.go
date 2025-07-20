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

package v1

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/labring/sealos/webhook/admission/pkg/code"

	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var vslog = logf.Log.WithName("virtualservice-webhook")

//+kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices,verbs=get;list;watch;create;update;patch;delete

//+kubebuilder:webhook:path=/mutate-networking-istio-io-v1beta1-virtualservice,mutating=true,failurePolicy=ignore,sideEffects=None,groups=networking.istio.io,resources=virtualservices,verbs=create;update,versions=v1beta1,name=mvirtualservice.sealos.io,admissionReviewVersions=v1
//+kubebuilder:object:generate=false

type VirtualServiceMutator struct {
	client.Client
	Domains            DomainList
	VsAnnotations      map[string]string
}

func (m *VirtualServiceMutator) SetupWithManager(mgr ctrl.Manager) error {
	m.Client = mgr.GetClient()
	
	// 创建 VirtualService 的 unstructured 对象
	virtualServiceType := &unstructured.Unstructured{}
	virtualServiceType.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualService",
	})
	
	return builder.WebhookManagedBy(mgr).
		For(virtualServiceType).
		WithDefaulter(m).
		Complete()
}

func (m *VirtualServiceMutator) Default(_ context.Context, obj runtime.Object) error {
	vs, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return errors.New("obj convert VirtualService is error")
	}

	for _, domain := range m.Domains {
		if isUserNamespace(vs.GetNamespace()) && m.hasSubDomain(vs, domain) {
			vslog.Info("mutating virtualservice in user ns", "virtualservice namespace", vs.GetNamespace(), "virtualservice name", vs.GetName())
			m.mutateUserVsAnnotations(vs)
		}
	}
	return nil
}

func (m *VirtualServiceMutator) mutateUserVsAnnotations(vs *unstructured.Unstructured) {
	annotations := vs.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	for k, v := range m.VsAnnotations {
		annotations[k] = v
	}
	vs.SetAnnotations(annotations)
}

func (m *VirtualServiceMutator) hasSubDomain(vs *unstructured.Unstructured, domain string) bool {
	// 获取 VirtualService 的 hosts
	spec, found, err := unstructured.NestedMap(vs.Object, "spec")
	if err != nil || !found {
		return false
	}
	
	hosts, found, err := unstructured.NestedStringSlice(spec, "hosts")
	if err != nil || !found {
		return false
	}
	
	for _, host := range hosts {
		if strings.HasSuffix(host, domain) {
			return true
		}
	}
	return false
}

//+kubebuilder:object:generate=false

type VirtualServiceValidator struct {
	client.Client
	Domains DomainList
	cache   cache.Cache

	IcpValidator *IcpValidator
}

const VirtualServiceHostIndex = "vs-host"

func (v *VirtualServiceValidator) SetupWithManager(mgr ctrl.Manager) error {
	vslog.Info("starting virtualservice webhook cache map")

	v.Client = mgr.GetClient()
	v.cache = mgr.GetCache()
	v.IcpValidator = NewIcpValidator(
		os.Getenv("ICP_ENABLED") == "true",
		os.Getenv("ICP_ENDPOINT"),
		os.Getenv("ICP_KEY"),
	)

	// 创建 VirtualService 的 unstructured 对象
	virtualServiceType := &unstructured.Unstructured{}
	virtualServiceType.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualService",
	})

	err := v.cache.IndexField(
		context.Background(),
		virtualServiceType,
		VirtualServiceHostIndex,
		func(obj client.Object) []string {
			vs := obj.(*unstructured.Unstructured)
			return v.extractHosts(vs)
		},
	)
	if err != nil {
		return err
	}

	return builder.WebhookManagedBy(mgr).
		For(virtualServiceType).
		WithValidator(v).
		Complete()
}

//+kubebuilder:webhook:path=/validate-networking-istio-io-v1beta1-virtualservice,mutating=false,failurePolicy=ignore,sideEffects=None,groups=networking.istio.io,resources=virtualservices,verbs=create;update;delete,versions=v1beta1,name=vvirtualservice.sealos.io,admissionReviewVersions=v1

func (v *VirtualServiceValidator) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	vs, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return errors.New("obj convert VirtualService is error")
	}

	vslog.Info("validating create", "virtualservice namespace", vs.GetNamespace(), "virtualservice name", vs.GetName())
	return v.validate(ctx, vs)
}

func (v *VirtualServiceValidator) ValidateUpdate(ctx context.Context, _, newObj runtime.Object) error {
	nvs, ok := newObj.(*unstructured.Unstructured)
	if !ok {
		return errors.New("obj convert VirtualService is error")
	}
	
	vslog.Info("validating update", "virtualservice namespace", nvs.GetNamespace(), "virtualservice name", nvs.GetName())
	return v.validate(ctx, nvs)
}

func (v *VirtualServiceValidator) ValidateDelete(_ context.Context, obj runtime.Object) error {
	vs, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return errors.New("obj convert VirtualService is error")
	}

	vslog.Info("validating delete", "virtualservice namespace", vs.GetNamespace(), "virtualservice name", vs.GetName())
	// delete virtualservice, pass validate
	return nil
}

func (v *VirtualServiceValidator) validate(ctx context.Context, vs *unstructured.Unstructured) error {
	// count validate cost time
	startTime := time.Now()
	defer func() {
		vslog.Info("finished validate", "virtualservice namespace", vs.GetNamespace(), "virtualservice name", vs.GetName(), "cost", time.Since(startTime))
	}()

	request, _ := admission.RequestFromContext(ctx)
	vslog.Info("validating", "virtualservice namespace", vs.GetNamespace(), "virtualservice name", vs.GetName(), "user", request.UserInfo.Username, "userGroups", request.UserInfo.Groups)
	
	if !isUserServiceAccount(request.UserInfo.Username) {
		vslog.Info("user is not user's serviceaccount, skip validate")
		return nil
	}

	if !isUserNamespace(vs.GetNamespace()) {
		vslog.Info("namespace is system namespace, skip validate")
		return nil
	}

	// 获取 VirtualService 的 hosts
	hosts := v.extractHosts(vs)
	if len(hosts) == 0 {
		vslog.Info("virtualservice has no hosts, skip validate")
		return nil
	}

	checks := []func(*unstructured.Unstructured, string) error{
		v.checkCname,
		v.checkOwner,
		v.checkIcp,
	}

	for _, host := range hosts {
		for _, check := range checks {
			if err := check(vs, host); err != nil {
				return err
			}
		}
	}

	return nil
}

func (v *VirtualServiceValidator) extractHosts(vs *unstructured.Unstructured) []string {
	spec, found, err := unstructured.NestedMap(vs.Object, "spec")
	if err != nil || !found {
		return nil
	}
	
	hosts, found, err := unstructured.NestedStringSlice(spec, "hosts")
	if err != nil || !found {
		return nil
	}
	
	// 过滤掉内部服务和通配符
	var validHosts []string
	for _, host := range hosts {
		if !strings.Contains(host, "*") && !strings.Contains(host, ".local") && !strings.Contains(host, ".svc.cluster.local") {
			validHosts = append(validHosts, host)
		}
	}
	
	return validHosts
}

func (v *VirtualServiceValidator) checkCname(vs *unstructured.Unstructured, host string) error {
	vslog.Info("checking cname", "virtualservice namespace", vs.GetNamespace(), "virtualservice name", vs.GetName(), "host", host)
	vslog.Info("domains:", "domains", strings.Join(v.Domains, ","))
	
	// get cname and check if it is cname to domain
	cname, err := net.LookupCNAME(host)
	if err != nil {
		vslog.Error(err, "can not verify virtualservice host "+host+", lookup cname error")
		return err
	}
	// remove last dot
	cname = strings.TrimSuffix(cname, ".")
	for _, domain := range v.Domains {
		// check if virtualservice host is end with domain
		if strings.HasSuffix(host, domain) {
			vslog.Info("virtualservice host is end with "+domain+", skip validate", "virtualservice namespace", vs.GetNamespace(), "virtualservice name", vs.GetName())
			return nil
		}
		// if cname is not end with domain, return error
		if strings.HasSuffix(cname, domain) {
			vslog.Info("virtualservice host "+host+" is cname to "+cname+", pass checkCname validate", "virtualservice namespace", vs.GetNamespace(), "virtualservice name", vs.GetName(), "cname", cname)
			return nil
		}
	}
	return fmt.Errorf(code.MessageFormat, code.IngressFailedCnameCheck, "can not verify virtualservice host "+host+", cname is not end with any domains in "+strings.Join(v.Domains, ","))
}

func (v *VirtualServiceValidator) checkOwner(vs *unstructured.Unstructured, host string) error {
	// 检查是否有其他 VirtualService 使用相同的 host
	virtualServiceType := &unstructured.Unstructured{}
	virtualServiceType.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualService",
	})
	
	vsList := &unstructured.UnstructuredList{}
	vsList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualServiceList",
	})
	
	if err := v.cache.List(context.Background(), vsList, client.MatchingFields{VirtualServiceHostIndex: host}); err != nil {
		vslog.Error(err, "can not verify virtualservice host "+host+", list virtualservice error")
		return fmt.Errorf(code.MessageFormat, code.IngressFailedOwnerCheck, err.Error())
	}

	for _, existingVs := range vsList.Items {
		if existingVs.GetNamespace() != vs.GetNamespace() {
			vslog.Info("virtualservice host "+host+" is owned by "+existingVs.GetNamespace()+", failed validate", "virtualservice namespace", vs.GetNamespace(), "virtualservice name", vs.GetName())
			return fmt.Errorf(code.MessageFormat, code.IngressFailedOwnerCheck, "virtualservice host "+host+" is owned by other user, you can not create virtualservice with same host.")
		}
	}
	// pass owner check
	vslog.Info("virtualservice host "+host+" pass checkOwner validate", "virtualservice namespace", vs.GetNamespace(), "virtualservice name", vs.GetName())
	return nil
}

func (v *VirtualServiceValidator) checkIcp(vs *unstructured.Unstructured, host string) error {
	if !v.IcpValidator.enabled {
		vslog.Info("icp is disabled, skip check icp", "virtualservice namespace", vs.GetNamespace(), "virtualservice name", vs.GetName(), "host", host)
		return nil
	}
	
	// 创建一个临时的 IngressRule 结构来复用现有的 ICP 验证逻辑
	rule := &netv1.IngressRule{Host: host}
	
	// check rule.host icp
	icpRep, err := v.IcpValidator.Query(rule)
	if err != nil {
		vslog.Error(err, "can not verify virtualservice host "+host+", icp query error")
		return fmt.Errorf(code.MessageFormat, code.IngressWebhookInternalError, "can not verify virtualservice host "+host+", icp query error")
	}
	if icpRep.ErrorCode != 0 {
		vslog.Error(err, "icp query error", "virtualservice namespace", vs.GetNamespace(), "virtualservice name", vs.GetName(), "host", host, "icp error code", icpRep.ErrorCode, "icp reason", icpRep.Reason)
		return fmt.Errorf(code.MessageFormat, code.IngressWebhookInternalError, icpRep.Reason)
	}
	// if icpRep.Result.SiteLicense is empty, return error, failed validate
	if icpRep.Result.SiteLicense == "" {
		vslog.Info("deny virtualservice host "+host+", icp query result is empty", "virtualservice namespace", vs.GetNamespace(), "virtualservice name", vs.GetName(), "host", host, "icp result", icpRep.Result)
		return fmt.Errorf(code.MessageFormat, code.IngressFailedIcpCheck, "icp query result is empty")
	}
	// pass icp check
	vslog.Info("virtualservice host "+host+" pass checkIcp validate", "virtualservice namespace", vs.GetNamespace(), "virtualservice name", vs.GetName(), "host", host, "icp result", icpRep.Result)
	return nil
}