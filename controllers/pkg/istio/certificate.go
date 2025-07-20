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
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// cert-manager Certificate GVK
	certificateGVK = schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	}
)

// certificateManager 证书管理器实现
type certificateManager struct {
	client client.Client
	config *NetworkConfig
}

// NewCertificateManager 创建新的证书管理器
func NewCertificateManager(client client.Client, config *NetworkConfig) CertificateManager {
	return &certificateManager{
		client: client,
		config: config,
	}
}

func (c *certificateManager) CreateOrUpdate(ctx context.Context, domain string, namespace string) error {
	// 如果没有启用自动 TLS，使用默认证书
	if !c.config.AutoTLS {
		return c.ensureDefaultCertificate(ctx, domain, namespace)
	}

	// 使用 cert-manager 自动生成证书
	return c.createCertManagerCertificate(ctx, domain, namespace)
}

func (c *certificateManager) CheckExpiration(ctx context.Context, secretName string, namespace string) (time.Time, error) {
	// 获取证书 Secret
	secret := &corev1.Secret{}
	key := types.NamespacedName{
		Name:      secretName,
		Namespace: namespace,
	}

	if err := c.client.Get(ctx, key, secret); err != nil {
		return time.Time{}, fmt.Errorf("failed to get certificate secret: %w", err)
	}

	// 解析证书
	certData, exists := secret.Data["tls.crt"]
	if !exists {
		return time.Time{}, fmt.Errorf("certificate data not found in secret")
	}

	return c.parseCertificateExpiration(certData)
}

func (c *certificateManager) Rotate(ctx context.Context, secretName string, namespace string) error {
	// 如果使用 cert-manager，删除 Certificate 资源让其重新生成
	if c.config.CertManager == "cert-manager" {
		return c.recreateCertManagerCertificate(ctx, secretName, namespace)
	}

	// 对于其他证书管理器，暂时不支持自动轮换
	return fmt.Errorf("certificate rotation not supported for cert manager: %s", c.config.CertManager)
}

func (c *certificateManager) Delete(ctx context.Context, secretName string, namespace string) error {
	// 删除 Certificate 资源（如果存在）
	if c.config.CertManager == "cert-manager" {
		cert := &unstructured.Unstructured{}
		cert.SetGroupVersionKind(certificateGVK)
		cert.SetName(secretName)
		cert.SetNamespace(namespace)

		if err := c.client.Delete(ctx, cert); !errors.IsNotFound(err) {
			return err
		}
	}

	// 删除 Secret
	secret := &corev1.Secret{}
	secret.SetName(secretName)
	secret.SetNamespace(namespace)

	return client.IgnoreNotFound(c.client.Delete(ctx, secret))
}

// ensureDefaultCertificate 确保默认证书存在
func (c *certificateManager) ensureDefaultCertificate(ctx context.Context, domain string, namespace string) error {
	secretName := c.getSecretName(domain)

	// 检查证书是否已存在
	secret := &corev1.Secret{}
	key := types.NamespacedName{
		Name:      secretName,
		Namespace: namespace,
	}

	if err := c.client.Get(ctx, key, secret); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		// 证书不存在，从默认证书复制
		return c.copyDefaultCertificate(ctx, secretName, namespace, domain)
	}

	// 检查证书是否即将过期
	expiration, err := c.CheckExpiration(ctx, secretName, namespace)
	if err != nil {
		return err
	}

	// 如果证书在 30 天内过期，更新证书
	if time.Until(expiration) < 30*24*time.Hour {
		return c.copyDefaultCertificate(ctx, secretName, namespace, domain)
	}

	return nil
}

// copyDefaultCertificate 从默认证书复制
func (c *certificateManager) copyDefaultCertificate(ctx context.Context, secretName string, namespace string, domain string) error {
	// 获取默认证书
	defaultSecret := &corev1.Secret{}
	defaultKey := types.NamespacedName{
		Name:      c.config.DefaultTLSSecret,
		Namespace: "sealos-system", // 默认证书通常在系统命名空间
	}

	if err := c.client.Get(ctx, defaultKey, defaultSecret); err != nil {
		return fmt.Errorf("failed to get default certificate: %w", err)
	}

	// 创建新的证书 Secret
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "sealos-istio",
				"sealos.io/cert-type":          "wildcard",
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": defaultSecret.Data["tls.crt"],
			"tls.key": defaultSecret.Data["tls.key"],
		},
	}

	return c.client.Create(ctx, newSecret)
}

// createCertManagerCertificate 创建 cert-manager Certificate
func (c *certificateManager) createCertManagerCertificate(ctx context.Context, domain string, namespace string) error {
	secretName := c.getSecretName(domain)

	// 检查 Certificate 是否已存在
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(certificateGVK)

	key := types.NamespacedName{
		Name:      secretName,
		Namespace: namespace,
	}

	if err := c.client.Get(ctx, key, cert); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		// 创建新的 Certificate
		return c.createNewCertificate(ctx, domain, secretName, namespace)
	}

	// Certificate 已存在，检查是否需要更新
	return c.updateCertificateIfNeeded(ctx, cert, domain)
}

// createNewCertificate 创建新的 Certificate 资源
func (c *certificateManager) createNewCertificate(ctx context.Context, domain string, secretName string, namespace string) error {
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(certificateGVK)
	cert.SetName(secretName)
	cert.SetNamespace(namespace)

	// 设置标签
	cert.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "sealos-istio",
		"sealos.io/domain":             domain,
	})

	// 构建 Certificate spec
	spec := map[string]interface{}{
		"secretName": secretName,
		"dnsNames":   []interface{}{domain},
		"issuerRef": map[string]interface{}{
			"name": "letsencrypt-prod",
			"kind": "ClusterIssuer",
		},
		"duration":    "2160h", // 90 天
		"renewBefore": "360h",  // 15 天前续期
	}

	// 如果是通配符域名，添加通配符支持
	if strings.HasPrefix(domain, "*.") {
		spec["dnsNames"] = []interface{}{domain, strings.TrimPrefix(domain, "*.")}
	}

	if err := unstructured.SetNestedMap(cert.Object, spec, "spec"); err != nil {
		return fmt.Errorf("failed to set certificate spec: %w", err)
	}

	return c.client.Create(ctx, cert)
}

// updateCertificateIfNeeded 如果需要则更新 Certificate
func (c *certificateManager) updateCertificateIfNeeded(ctx context.Context, cert *unstructured.Unstructured, domain string) error {
	// 检查 DNS 名称是否匹配
	dnsNames, found, err := unstructured.NestedStringSlice(cert.Object, "spec", "dnsNames")
	if err != nil || !found {
		return fmt.Errorf("failed to get dnsNames from certificate")
	}

	// 检查域名是否已包含在证书中
	for _, name := range dnsNames {
		if name == domain {
			return nil // 域名已存在，无需更新
		}
	}

	// 添加新的域名
	dnsNames = append(dnsNames, domain)
	if err := unstructured.SetNestedStringSlice(cert.Object, dnsNames, "spec", "dnsNames"); err != nil {
		return fmt.Errorf("failed to update dnsNames: %w", err)
	}

	return c.client.Update(ctx, cert)
}

// recreateCertManagerCertificate 重新创建 cert-manager Certificate
func (c *certificateManager) recreateCertManagerCertificate(ctx context.Context, secretName string, namespace string) error {
	// 获取现有的 Certificate
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(certificateGVK)

	key := types.NamespacedName{
		Name:      secretName,
		Namespace: namespace,
	}

	if err := c.client.Get(ctx, key, cert); err != nil {
		return fmt.Errorf("failed to get certificate: %w", err)
	}

	// 添加轮换注解来触发重新生成
	annotations := cert.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations["cert-manager.io/force-renewal"] = time.Now().Format(time.RFC3339)
	cert.SetAnnotations(annotations)

	return c.client.Update(ctx, cert)
}

// parseCertificateExpiration 解析证书过期时间
func (c *certificateManager) parseCertificateExpiration(certData []byte) (time.Time, error) {
	block, _ := pem.Decode(certData)
	if block == nil {
		return time.Time{}, fmt.Errorf("failed to decode PEM certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return cert.NotAfter, nil
}

// getSecretName 获取证书 Secret 名称
func (c *certificateManager) getSecretName(domain string) string {
	// 替换特殊字符
	secretName := strings.ReplaceAll(domain, "*", "wildcard")
	secretName = strings.ReplaceAll(secretName, ".", "-")
	return secretName + "-tls"
}

// IsCertificateReady 检查证书是否就绪
func (c *certificateManager) IsCertificateReady(ctx context.Context, secretName string, namespace string) (bool, error) {
	// 检查 Secret 是否存在
	secret := &corev1.Secret{}
	key := types.NamespacedName{
		Name:      secretName,
		Namespace: namespace,
	}

	if err := c.client.Get(ctx, key, secret); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	// 检查证书数据是否完整
	if _, exists := secret.Data["tls.crt"]; !exists {
		return false, nil
	}
	if _, exists := secret.Data["tls.key"]; !exists {
		return false, nil
	}

	// 检查证书是否未过期
	expiration, err := c.CheckExpiration(ctx, secretName, namespace)
	if err != nil {
		return false, err
	}

	return time.Now().Before(expiration), nil
}
