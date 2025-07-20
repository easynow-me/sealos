// Copyright © 2025 sealos.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1

import (
	"os"
	"strings"
)

// IsIstioWebhooksEnabled 检查是否应该启用 Istio webhooks
func IsIstioWebhooksEnabled() bool {
	enabled := os.Getenv("ENABLE_ISTIO_WEBHOOKS")
	return enabled == "true" || enabled == "1" || enabled == "on"
}

// GetVirtualServiceAnnotations 从环境变量获取 VirtualService 注解配置
func GetVirtualServiceAnnotations() map[string]string {
	annotationString := os.Getenv("VIRTUALSERVICE_MUTATING_ANNOTATIONS")
	annotations := make(map[string]string)
	
	if annotationString != "" {
		kvs := strings.Split(annotationString, ",")
		for _, kv := range kvs {
			parts := strings.Split(kv, "=")
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				annotations[key] = value
			}
		}
	}
	
	return annotations
}

// IsIstioAvailable 检查集群中是否安装了 Istio
// 这个函数可以用于运行时检查 Istio 是否可用
func IsIstioAvailable() bool {
	// 简单检查，可以根据需要扩展
	// 检查是否设置了 Istio 相关的环境变量
	return os.Getenv("ISTIO_PILOT_SERVICE") != "" || 
		   os.Getenv("PILOT_ENABLE_WORKLOAD_ENTRY_AUTOREGISTRATION") != "" ||
		   os.Getenv("ISTIO_META_CLUSTER_ID") != ""
}