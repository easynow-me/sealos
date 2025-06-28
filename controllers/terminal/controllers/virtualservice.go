/*
Copyright 2024 labring.

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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	terminalv1 "github.com/labring/sealos/controllers/terminal/api/v1"
)

func (r *TerminalReconciler) createIstioVirtualService(terminal *terminalv1.Terminal, host string) *unstructured.Unstructured {
	cors := fmt.Sprintf("https://%s,https://*.%s", r.CtrConfig.Global.CloudDomain+r.getPort(), r.CtrConfig.Global.CloudDomain+r.getPort())
	secretHeader := terminal.Status.SecretHeader

	objectMeta := metav1.ObjectMeta{
		Name:      terminal.Name,
		Namespace: terminal.Namespace,
		Annotations: map[string]string{
			"higress.io/request-header-control-update": fmt.Sprintf(`\nAuthorization \"\"\n%s \"1\"`, secretHeader),
		},
	}

	virtualService := &unstructured.Unstructured{}
	virtualService.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualService",
	})
	virtualService.SetName(objectMeta.Name)
	virtualService.SetNamespace(objectMeta.Namespace)
	virtualService.SetAnnotations(objectMeta.Annotations)

	// Set spec using unstructured
	spec := map[string]interface{}{
		"hosts": []string{host},
		"http": []map[string]interface{}{
			{
				"name": "terminal-route",
				"match": []map[string]interface{}{
					{
						"uri": map[string]interface{}{
							"prefix": "/",
						},
					},
				},
				"route": []map[string]interface{}{
					{
						"destination": map[string]interface{}{
							"host": terminal.Status.ServiceName,
							"port": map[string]interface{}{
								"number": 8080,
							},
						},
						"weight": 100,
					},
				},
				"corsPolicy": map[string]interface{}{
					"allowOrigins": []map[string]interface{}{
						{
							"exact": cors,
						},
					},
					"allowMethods": []string{"PUT", "GET", "POST", "PATCH", "OPTIONS"},
					"allowCredentials": map[string]interface{}{
						"value": false,
					},
				},
			},
		},
	}

	if r.CtrConfig.TerminalConfig.IngressTLSSecretName != "" {
		spec["tls"] = []map[string]interface{}{
			{
				"match": []map[string]interface{}{
					{
						"port":     443,
						"sniHosts": []string{host},
					},
				},
				"route": []map[string]interface{}{
					{
						"destination": map[string]interface{}{
							"host": terminal.Status.ServiceName,
							"port": map[string]interface{}{
								"number": 8080,
							},
						},
						"weight": 100,
					},
				},
			},
		}
	}

	virtualService.Object["spec"] = spec
	return virtualService
}

func (r *TerminalReconciler) syncIstioVirtualService(ctx context.Context, terminal *terminalv1.Terminal, host string, recLabels map[string]string) error {
	virtualService := &unstructured.Unstructured{}
	virtualService.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualService",
	})
	virtualService.SetName(terminal.Name)
	virtualService.SetNamespace(terminal.Namespace)
	virtualService.SetLabels(recLabels)

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, virtualService, func() error {
		expectVirtualService := r.createIstioVirtualService(terminal, host)
		virtualService.SetAnnotations(expectVirtualService.GetAnnotations())
		virtualService.Object["spec"] = expectVirtualService.Object["spec"]
		return controllerutil.SetControllerReference(terminal, virtualService, r.Scheme)
	}); err != nil {
		return err
	}

	domain := Protocol + host + r.getPort()
	if terminal.Status.Domain != domain {
		terminal.Status.Domain = domain
		return r.Status().Update(ctx, terminal)
	}

	return nil
}
