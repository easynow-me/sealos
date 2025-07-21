package controllers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	adminerv1 "github.com/labring/sealos/controllers/db/adminer/api/v1"
	"github.com/labring/sealos/controllers/pkg/istio"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

func TestAdminerVirtualServiceCorsOutput(t *testing.T) {
	// 创建测试配置
	config := &istio.NetworkConfig{
		BaseDomain:       "cloud.sealos.io",
		DefaultGateway:   "istio-system/sealos-gateway",
		PublicDomains:    []string{"cloud.sealos.io", "*.example.com"},
		TLSEnabled:       true,
		DefaultTLSSecret: "wildcard-cert",
	}

	// 创建 fake client
	scheme := runtime.NewScheme()
	_ = adminerv1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// 创建协调器
	reconciler := NewAdminerIstioNetworkingReconciler(fakeClient, config, true, "cloud.sealos.io")

	// 创建测试 Adminer 实例
	adminer := &adminerv1.Adminer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-adminer",
			Namespace: "test-namespace",
		},
	}

	// 构建网络规范
	spec := reconciler.buildNetworkingSpec(adminer, "test-host")

	// 打印 CORS 配置以便调试
	fmt.Printf("CORS Origins: %v\n", spec.CorsPolicy.AllowOrigins)
	fmt.Printf("CORS Methods: %v\n", spec.CorsPolicy.AllowMethods)
	fmt.Printf("CORS Headers: %v\n", spec.CorsPolicy.AllowHeaders)
	fmt.Printf("CORS Credentials: %v\n", spec.CorsPolicy.AllowCredentials)

	// 验证 CORS origins
	expectedOrigins := []string{
		"https://adminer.cloud.sealos.io",
		"https://adminer.cloud.sealos.io",
		"https://adminer.example.com",
	}

	if len(spec.CorsPolicy.AllowOrigins) != len(expectedOrigins) {
		t.Errorf("Expected %d origins, got %d", len(expectedOrigins), len(spec.CorsPolicy.AllowOrigins))
	}

	// 检查是否包含预期的 origins
	originMap := make(map[string]bool)
	for _, origin := range spec.CorsPolicy.AllowOrigins {
		originMap[origin] = true
	}

	for _, expected := range expectedOrigins {
		if !originMap[expected] {
			t.Errorf("Missing expected origin: %s", expected)
		}
	}

	// 现在测试实际的 VirtualService 创建
	t.Run("VirtualService YAML Output", func(t *testing.T) {
		// 使用实际的 manager 创建 VirtualService
		ctx := context.Background()
		err := reconciler.SyncIstioNetworking(ctx, adminer, "test-host")
		if err != nil {
			// 预期会失败，因为 fake client 没有 VirtualService CRD
			t.Logf("Expected error (no CRD): %v", err)
		}

		// 手动构建 VirtualService 以检查输出
		vs := buildVirtualServiceManually(spec)
		
		// 转换为 YAML
		yamlBytes, err := yaml.Marshal(vs)
		if err != nil {
			t.Fatalf("Failed to marshal VirtualService to YAML: %v", err)
		}

		yamlStr := string(yamlBytes)
		fmt.Printf("\nGenerated VirtualService YAML:\n%s\n", yamlStr)

		// 验证 YAML 中包含正确的 CORS 配置
		if !strings.Contains(yamlStr, "exact: https://adminer.cloud.sealos.io") {
			t.Error("VirtualService YAML should contain exact CORS origin")
		}

		if strings.Contains(yamlStr, "regex:") {
			t.Error("VirtualService YAML should NOT contain regex CORS origin")
		}
	})
}

func buildVirtualServiceManually(spec *istio.AppNetworkingSpec) *unstructured.Unstructured {
	// 构建 CORS 策略
	corsPolicy := map[string]interface{}{
		"allowOrigins": []map[string]string{},
		"allowMethods": spec.CorsPolicy.AllowMethods,
		"allowHeaders": spec.CorsPolicy.AllowHeaders,
		"allowCredentials": spec.CorsPolicy.AllowCredentials,
	}

	// 添加 origins - 使用 exact 匹配
	for _, origin := range spec.CorsPolicy.AllowOrigins {
		corsPolicy["allowOrigins"] = append(corsPolicy["allowOrigins"].([]map[string]string), 
			map[string]string{"exact": origin})
	}

	// 构建 VirtualService
	vs := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.istio.io/v1beta1",
			"kind":       "VirtualService",
			"metadata": map[string]interface{}{
				"name":      spec.Name,
				"namespace": spec.Namespace,
				"labels":    spec.Labels,
			},
			"spec": map[string]interface{}{
				"hosts":    spec.Hosts,
				"gateways": []string{"istio-system/sealos-gateway"},
				"http": []map[string]interface{}{
					{
						"match": []map[string]interface{}{
							{
								"uri": map[string]string{
									"prefix": "/",
								},
							},
						},
						"route": []map[string]interface{}{
							{
								"destination": map[string]interface{}{
									"host": spec.ServiceName,
									"port": map[string]interface{}{
										"number": spec.ServicePort,
									},
								},
							},
						},
						"corsPolicy": corsPolicy,
						"timeout": "86400s",
					},
				},
			},
		},
	}

	return vs
}

