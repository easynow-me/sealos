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
	"crypto/md5"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// domainAllocator 域名分配器实现
type domainAllocator struct {
	config *NetworkConfig
}

// NewDomainAllocator 创建新的域名分配器
func NewDomainAllocator(config *NetworkConfig) DomainAllocator {
	return &domainAllocator{
		config: config,
	}
}

func (d *domainAllocator) GenerateAppDomain(tenantID, appName string) string {
	// 使用配置的模板生成域名
	template, exists := d.config.DomainTemplates["app"]
	if !exists {
		// 默认模板：{app-name}-{hash}.{tenant-id}.{base-domain}
		template = "{{.AppName}}-{{.Hash}}.{{.TenantID}}.{{.BaseDomain}}"
	}

	// 生成短哈希以确保唯一性
	hash := d.generateShortHash(tenantID + appName)

	// 替换模板变量
	domain := template
	domain = strings.ReplaceAll(domain, "{{.AppName}}", d.sanitizeDomainPart(appName))
	domain = strings.ReplaceAll(domain, "{{.TenantID}}", d.sanitizeDomainPart(tenantID))
	domain = strings.ReplaceAll(domain, "{{.Hash}}", hash)
	domain = strings.ReplaceAll(domain, "{{.BaseDomain}}", d.config.BaseDomain)

	return strings.ToLower(domain)
}

func (d *domainAllocator) ValidateCustomDomain(domain string) error {
	// 1. 基本格式验证
	if err := d.validateDomainFormat(domain); err != nil {
		return err
	}

	// 2. 检查是否为保留域名
	if d.isReservedDomain(domain) {
		return fmt.Errorf("domain %s is reserved", domain)
	}

	// 3. DNS 解析验证
	if err := d.validateDNSResolution(domain); err != nil {
		return fmt.Errorf("DNS validation failed for %s: %w", domain, err)
	}

	// 4. ICP 备案验证（中国域名）
	if d.isChinaDomain(domain) {
		if err := d.validateICPRecord(domain); err != nil {
			return fmt.Errorf("ICP validation failed for %s: %w", domain, err)
		}
	}

	return nil
}

func (d *domainAllocator) IsDomainAvailable(domain string) (bool, error) {
	// 检查基本格式
	if err := d.validateDomainFormat(domain); err != nil {
		return false, err
	}

	// 检查是否为保留域名
	if d.isReservedDomain(domain) {
		return false, nil
	}

	// 这里可以添加更多检查，比如：
	// - 检查域名是否已被其他租户使用
	// - 检查域名是否在黑名单中

	return true, nil
}

// validateDomainFormat 验证域名格式
func (d *domainAllocator) validateDomainFormat(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain cannot be empty")
	}

	if len(domain) > 253 {
		return fmt.Errorf("domain %s is too long (max 253 characters)", domain)
	}

	// 基本域名格式验证
	domainRegex := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)
	if !domainRegex.MatchString(domain) {
		return fmt.Errorf("invalid domain format: %s", domain)
	}

	// 检查是否以点开头或结尾
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return fmt.Errorf("domain cannot start or end with a dot: %s", domain)
	}

	// 检查连续的点
	if strings.Contains(domain, "..") {
		return fmt.Errorf("domain cannot contain consecutive dots: %s", domain)
	}

	return nil
}

// isReservedDomain 检查是否为保留域名
func (d *domainAllocator) isReservedDomain(domain string) bool {
	// 检查配置的保留域名
	for _, reserved := range d.config.ReservedDomains {
		if domain == reserved || strings.HasSuffix(domain, "."+reserved) {
			return true
		}
	}

	// 检查常见的保留子域名
	parts := strings.Split(domain, ".")
	if len(parts) > 0 {
		subdomain := parts[0]
		reservedSubdomains := []string{
			"api", "www", "mail", "ftp", "admin", "root", "system",
			"console", "dashboard", "management", "cluster", "istio",
			"kubernetes", "k8s", "sealos", "cloud",
		}

		for _, reserved := range reservedSubdomains {
			if subdomain == reserved {
				return true
			}
		}
	}

	return false
}

// validateDNSResolution 验证 DNS 解析
func (d *domainAllocator) validateDNSResolution(domain string) error {
	// 检查域名是否可以解析
	_, err := net.LookupHost(domain)
	if err != nil {
		// DNS 解析失败通常意味着域名不存在或配置错误
		return fmt.Errorf("DNS lookup failed: %w", err)
	}

	return nil
}

// isChinaDomain 检查是否为中国域名
func (d *domainAllocator) isChinaDomain(domain string) bool {
	chineseTLDs := []string{
		".cn", ".com.cn", ".net.cn", ".org.cn", ".gov.cn", ".edu.cn",
		".中国", ".公司", ".网络",
	}

	for _, tld := range chineseTLDs {
		if strings.HasSuffix(strings.ToLower(domain), tld) {
			return true
		}
	}

	return false
}

// validateICPRecord 验证 ICP 备案
func (d *domainAllocator) validateICPRecord(domain string) error {
	// 注意：实际的 ICP 验证需要调用相关的 API 服务
	// 这里只是一个示例实现

	// 对于中国的域名，通常需要有效的 ICP 备案号
	// 这里可以集成第三方 ICP 查询 API

	// 暂时返回成功，实际实现时需要调用真实的 ICP 验证服务
	return nil
}

// sanitizeDomainPart 清理域名部分
func (d *domainAllocator) sanitizeDomainPart(part string) string {
	// 移除非法字符
	reg := regexp.MustCompile(`[^a-zA-Z0-9\-]`)
	cleaned := reg.ReplaceAllString(part, "-")

	// 移除开头和结尾的连字符
	cleaned = strings.Trim(cleaned, "-")

	// 限制长度
	if len(cleaned) > 63 {
		cleaned = cleaned[:63]
	}

	// 如果为空，使用默认值
	if cleaned == "" {
		cleaned = "app"
	}

	return strings.ToLower(cleaned)
}

// generateShortHash 生成短哈希
func (d *domainAllocator) generateShortHash(input string) string {
	hash := md5.Sum([]byte(input))
	// 取前6个字符
	return fmt.Sprintf("%x", hash)[:6]
}

// GetDomainForTerminal 为 Terminal 生成域名
func (d *domainAllocator) GetDomainForTerminal(tenantID, terminalID string) string {
	template, exists := d.config.DomainTemplates["terminal"]
	if !exists {
		template = "terminal-{{.Hash}}.{{.TenantID}}.{{.BaseDomain}}"
	}

	hash := d.generateShortHash(tenantID + terminalID)

	domain := template
	domain = strings.ReplaceAll(domain, "{{.TenantID}}", d.sanitizeDomainPart(tenantID))
	domain = strings.ReplaceAll(domain, "{{.Hash}}", hash)
	domain = strings.ReplaceAll(domain, "{{.BaseDomain}}", d.config.BaseDomain)

	return strings.ToLower(domain)
}

// GetDomainForDatabase 为数据库管理器生成域名
func (d *domainAllocator) GetDomainForDatabase(tenantID, dbName string) string {
	template, exists := d.config.DomainTemplates["database"]
	if !exists {
		template = "db-{{.Hash}}.{{.TenantID}}.{{.BaseDomain}}"
	}

	hash := d.generateShortHash(tenantID + dbName)

	domain := template
	domain = strings.ReplaceAll(domain, "{{.TenantID}}", d.sanitizeDomainPart(tenantID))
	domain = strings.ReplaceAll(domain, "{{.Hash}}", hash)
	domain = strings.ReplaceAll(domain, "{{.BaseDomain}}", d.config.BaseDomain)

	return strings.ToLower(domain)
}
