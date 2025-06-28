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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	adminerv1 "github.com/labring/sealos/controllers/db/adminer/api/v1"
)

func (r *AdminerReconciler) createIstioVirtualService(adminer *adminerv1.Adminer, host string) *unstructured.Unstructured {
	objectMeta := metav1.ObjectMeta{
		Name:      adminer.ObjectMeta.Name,
		Namespace: adminer.ObjectMeta.Namespace,
		Annotations: map[string]string{
			"higress.io/response-header-control-remove": "X-Frame-Options",
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
				"name": "adminer-route",
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
							"host": adminer.ObjectMeta.Name,
							"port": map[string]interface{}{
								"number": 8080,
							},
						},
						"weight": 100,
					},
				},
			},
		},
	}

	if r.tlsEnabled {
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
							"host": adminer.ObjectMeta.Name,
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

func (r *AdminerReconciler) syncIstioVirtualService(ctx context.Context, adminer *adminerv1.Adminer, host string, recLabels map[string]string) error {
	virtualService := &unstructured.Unstructured{}
	virtualService.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualService",
	})
	virtualService.SetName(adminer.ObjectMeta.Name)
	virtualService.SetNamespace(adminer.ObjectMeta.Namespace)
	virtualService.SetLabels(recLabels)

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, virtualService, func() error {
		expectVirtualService := r.createIstioVirtualService(adminer, host)
		virtualService.SetAnnotations(expectVirtualService.GetAnnotations())
		virtualService.Object["spec"] = expectVirtualService.Object["spec"]
		return controllerutil.SetControllerReference(adminer, virtualService, r.Scheme)
	}); err != nil {
		return err
	}

	protocol := protocolHTTPS
	if !r.tlsEnabled {
		protocol = protocolHTTP
	}

	domain := protocol + host
	if adminer.Status.Domain != domain {
		adminer.Status.Domain = domain
		return r.Client.Status().Update(ctx, adminer)
	}

	return nil
}
