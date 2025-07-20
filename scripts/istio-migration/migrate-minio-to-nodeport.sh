#!/bin/bash

# MinIO LoadBalancer 到 NodePort + Istio 迁移脚本
# 解决禁止租户创建 LoadBalancer 对 MinIO 对象存储的影响

set -euo pipefail

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 配置变量
NAMESPACE="objectstorage-system"
SERVICE_NAME="object-storage"
MINIO_DOMAIN="minio.objectstorage-system.sealos.io"
NODEPORT=30900
DRY_RUN=false
BACKUP=true

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 显示帮助信息
show_help() {
    cat << EOF
MinIO LoadBalancer 到 NodePort + Istio 迁移脚本

Usage: $0 [OPTIONS]

OPTIONS:
    --domain DOMAIN      MinIO 访问域名 (默认: minio.objectstorage-system.sealos.io)
    --nodeport PORT      NodePort 端口 (默认: 30900)
    --namespace NS       MinIO 命名空间 (默认: objectstorage-system)
    --dry-run            预览操作，不执行实际更改
    --no-backup          跳过配置备份
    -h, --help           显示此帮助信息

EXAMPLES:
    $0                                    # 使用默认配置迁移
    $0 --domain minio.sealos.io          # 使用自定义域名
    $0 --dry-run                         # 预览迁移操作
    $0 --nodeport 31900 --no-backup      # 自定义端口且不备份
EOF
}

# 检查前置条件
check_prerequisites() {
    log_info "检查前置条件..."
    
    # 检查 kubectl
    if ! command -v kubectl >/dev/null 2>&1; then
        log_error "kubectl 未安装或不在 PATH 中"
        exit 1
    fi
    
    # 检查集群连接
    if ! kubectl cluster-info >/dev/null 2>&1; then
        log_error "无法连接到 Kubernetes 集群"
        exit 1
    fi
    
    # 检查命名空间是否存在
    if ! kubectl get namespace "$NAMESPACE" >/dev/null 2>&1; then
        log_error "命名空间 $NAMESPACE 不存在"
        exit 1
    fi
    
    # 检查 MinIO 服务是否存在
    if ! kubectl get service "$SERVICE_NAME" -n "$NAMESPACE" >/dev/null 2>&1; then
        log_error "MinIO 服务 $SERVICE_NAME 在命名空间 $NAMESPACE 中不存在"
        exit 1
    fi
    
    # 检查 Istio 是否安装
    if ! kubectl get namespace istio-system >/dev/null 2>&1; then
        log_error "Istio 未安装 (istio-system 命名空间不存在)"
        exit 1
    fi
    
    log_success "前置条件检查通过"
}

# 备份当前配置
backup_current_config() {
    if [[ "$BACKUP" == "false" ]]; then
        log_info "跳过配置备份"
        return
    fi
    
    local backup_dir="/tmp/minio-migration-backup-$(date +%Y%m%d-%H%M%S)"
    mkdir -p "$backup_dir"
    
    log_info "备份当前 MinIO 配置到 $backup_dir..."
    
    # 备份 Service
    kubectl get service "$SERVICE_NAME" -n "$NAMESPACE" -o yaml > "$backup_dir/service.yaml"
    
    # 备份相关的 Gateway 和 VirtualService (如果存在)
    kubectl get gateway -n "$NAMESPACE" -o yaml > "$backup_dir/gateways.yaml" 2>/dev/null || true
    kubectl get virtualservice -n "$NAMESPACE" -o yaml > "$backup_dir/virtualservices.yaml" 2>/dev/null || true
    
    # 备份 MinIO 部署
    kubectl get deployment -l v1.min.io/tenant=object-storage -n "$NAMESPACE" -o yaml > "$backup_dir/deployments.yaml" 2>/dev/null || true
    
    log_success "配置备份完成: $backup_dir"
    echo "恢复命令: kubectl apply -f $backup_dir/service.yaml"
}

# 获取当前 MinIO 服务状态
get_current_status() {
    log_info "获取当前 MinIO 服务状态..."
    
    local current_type
    current_type=$(kubectl get service "$SERVICE_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.type}')
    
    echo "当前服务类型: $current_type"
    
    if [[ "$current_type" == "LoadBalancer" ]]; then
        log_warn "MinIO 当前使用 LoadBalancer，需要迁移"
        
        # 获取 LoadBalancer IP (如果有)
        local lb_ip
        lb_ip=$(kubectl get service "$SERVICE_NAME" -n "$NAMESPACE" -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "pending")
        echo "LoadBalancer IP: $lb_ip"
        
        return 1
    elif [[ "$current_type" == "NodePort" ]]; then
        log_success "MinIO 已经使用 NodePort"
        
        # 获取当前 NodePort
        local current_nodeport
        current_nodeport=$(kubectl get service "$SERVICE_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.ports[0].nodePort}')
        echo "当前 NodePort: $current_nodeport"
        
        return 0
    else
        log_error "未知的服务类型: $current_type"
        return 2
    fi
}

# 创建 MinIO NodePort 服务配置
create_nodeport_service() {
    log_info "创建 MinIO NodePort 服务配置..."
    
    cat > /tmp/minio-nodeport-service.yaml << EOF
apiVersion: v1
kind: Service
metadata:
  name: $SERVICE_NAME
  namespace: $NAMESPACE
  labels:
    app: minio
    component: object-storage
spec:
  type: NodePort
  ports:
    - name: http-minio
      protocol: TCP
      port: 9000
      targetPort: 9000
      nodePort: $NODEPORT
  selector:
    v1.min.io/tenant: object-storage
  sessionAffinity: None
  externalTrafficPolicy: Cluster
EOF

    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "DRY RUN: MinIO NodePort 服务配置:"
        cat /tmp/minio-nodeport-service.yaml
        return
    fi
    
    kubectl apply -f /tmp/minio-nodeport-service.yaml
    log_success "MinIO NodePort 服务配置已应用"
}

# 创建 Istio Gateway 配置
create_istio_gateway() {
    log_info "创建 MinIO Istio Gateway 配置..."
    
    cat > /tmp/minio-istio-config.yaml << EOF
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: minio-gateway
  namespace: $NAMESPACE
  labels:
    app: minio
    component: gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "$MINIO_DOMAIN"
    # HTTP 重定向到 HTTPS
    tls:
      httpsRedirect: true
  - port:
      number: 443
      name: https
      protocol: HTTPS
    hosts:
    - "$MINIO_DOMAIN"
    tls:
      mode: SIMPLE
      credentialName: minio-tls-cert
---
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: minio-vs
  namespace: $NAMESPACE
  labels:
    app: minio
    component: virtual-service
spec:
  hosts:
  - "$MINIO_DOMAIN"
  gateways:
  - minio-gateway
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: $SERVICE_NAME
        port:
          number: 9000
    timeout: 300s
    corsPolicy:
      allowOrigins:
      - regex: ".*"
      allowMethods:
      - GET
      - POST
      - PUT
      - DELETE
      - HEAD
      - OPTIONS
      allowHeaders:
      - "*"
      allowCredentials: false
    headers:
      response:
        set:
          X-Frame-Options: "SAMEORIGIN"
          X-Content-Type-Options: "nosniff"
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: minio-tls-cert
  namespace: $NAMESPACE
spec:
  secretName: minio-tls-cert
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
  - "$MINIO_DOMAIN"
EOF

    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "DRY RUN: MinIO Istio 配置:"
        cat /tmp/minio-istio-config.yaml
        return
    fi
    
    kubectl apply -f /tmp/minio-istio-config.yaml
    log_success "MinIO Istio Gateway 配置已应用"
}

# 验证迁移结果
verify_migration() {
    log_info "验证迁移结果..."
    
    # 检查服务类型
    local service_type
    service_type=$(kubectl get service "$SERVICE_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.type}')
    
    if [[ "$service_type" == "NodePort" ]]; then
        log_success "✓ 服务类型已更改为 NodePort"
    else
        log_error "✗ 服务类型仍为: $service_type"
        return 1
    fi
    
    # 检查 NodePort
    local nodeport
    nodeport=$(kubectl get service "$SERVICE_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.ports[0].nodePort}')
    
    if [[ "$nodeport" == "$NODEPORT" ]]; then
        log_success "✓ NodePort 配置正确: $nodeport"
    else
        log_error "✗ NodePort 配置错误，期望: $NODEPORT，实际: $nodeport"
        return 1
    fi
    
    # 检查 Gateway
    if kubectl get gateway minio-gateway -n "$NAMESPACE" >/dev/null 2>&1; then
        log_success "✓ Istio Gateway 创建成功"
    else
        log_error "✗ Istio Gateway 创建失败"
        return 1
    fi
    
    # 检查 VirtualService
    if kubectl get virtualservice minio-vs -n "$NAMESPACE" >/dev/null 2>&1; then
        log_success "✓ Istio VirtualService 创建成功"
    else
        log_error "✗ Istio VirtualService 创建失败"
        return 1
    fi
    
    # 检查 Pod 状态
    local ready_pods
    ready_pods=$(kubectl get pods -l v1.min.io/tenant=object-storage -n "$NAMESPACE" --no-headers | grep Running | wc -l)
    
    if [[ "$ready_pods" -gt 0 ]]; then
        log_success "✓ MinIO Pod 运行正常 ($ready_pods 个)"
    else
        log_warn "⚠ 没有发现运行中的 MinIO Pod"
    fi
    
    log_success "迁移验证完成"
}

# 测试连通性
test_connectivity() {
    log_info "测试 MinIO 连通性..."
    
    # 获取 Istio Gateway 外部 IP
    local gateway_ip
    gateway_ip=$(kubectl get service istio-ingressgateway -n istio-system -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "pending")
    
    if [[ "$gateway_ip" == "pending" || -z "$gateway_ip" ]]; then
        log_warn "Istio Gateway 外部 IP 尚未就绪，跳过连通性测试"
        log_info "请配置 DNS: $MINIO_DOMAIN -> Istio Gateway IP"
        return
    fi
    
    log_info "使用 Istio Gateway IP: $gateway_ip"
    
    # 测试 HTTP 健康检查
    if curl -s -f -H "Host: $MINIO_DOMAIN" "http://$gateway_ip/minio/health/live" >/dev/null; then
        log_success "✓ MinIO HTTP 健康检查通过"
    else
        log_warn "⚠ MinIO HTTP 健康检查失败"
    fi
    
    # 测试 NodePort 直连
    local node_ip
    node_ip=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="ExternalIP")].address}' 2>/dev/null || kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
    
    if [[ -n "$node_ip" ]]; then
        log_info "测试 NodePort 直连: $node_ip:$NODEPORT"
        if curl -s -f "http://$node_ip:$NODEPORT/minio/health/live" >/dev/null; then
            log_success "✓ MinIO NodePort 直连测试通过"
        else
            log_warn "⚠ MinIO NodePort 直连测试失败"
        fi
    fi
}

# 显示迁移后的配置信息
show_configuration() {
    log_info "迁移后的配置信息:"
    echo ""
    echo "MinIO 服务信息:"
    echo "  类型: NodePort"
    echo "  端口: 9000"
    echo "  NodePort: $NODEPORT"
    echo "  域名: $MINIO_DOMAIN"
    echo ""
    echo "访问方式:"
    echo "  1. 通过 Istio Gateway (推荐):"
    echo "     https://$MINIO_DOMAIN"
    echo ""
    echo "  2. 通过 NodePort 直连:"
    echo "     http://<NODE_IP>:$NODEPORT"
    echo ""
    echo "客户端配置更新:"
    echo "  - API Endpoint: https://$MINIO_DOMAIN"
    echo "  - Port: 443 (HTTPS) 或 80 (HTTP)"
    echo "  - SSL: true (推荐)"
    echo ""
    echo "故障排查命令:"
    echo "  kubectl get svc $SERVICE_NAME -n $NAMESPACE"
    echo "  kubectl get gateway minio-gateway -n $NAMESPACE"
    echo "  kubectl get virtualservice minio-vs -n $NAMESPACE"
    echo "  kubectl logs -l v1.min.io/tenant=object-storage -n $NAMESPACE"
}

# 主函数
main() {
    # 解析命令行参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            --domain)
                MINIO_DOMAIN="$2"
                shift 2
                ;;
            --nodeport)
                NODEPORT="$2"
                shift 2
                ;;
            --namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            --no-backup)
                BACKUP=false
                shift
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                log_error "未知选项: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    echo "=== MinIO LoadBalancer 到 NodePort + Istio 迁移 ==="
    echo "命名空间: $NAMESPACE"
    echo "服务名称: $SERVICE_NAME"
    echo "域名: $MINIO_DOMAIN"
    echo "NodePort: $NODEPORT"
    echo "Dry Run: $DRY_RUN"
    echo ""
    
    # 检查前置条件
    check_prerequisites
    
    # 备份当前配置
    backup_current_config
    
    # 检查当前状态
    if get_current_status; then
        log_info "MinIO 已经使用 NodePort，检查 Istio 配置..."
    else
        log_info "开始迁移 MinIO 到 NodePort..."
        
        if [[ "$DRY_RUN" == "false" ]]; then
            read -p "确认继续迁移？(y/N): " -n 1 -r
            echo
            if [[ ! $REPLY =~ ^[Yy]$ ]]; then
                log_info "迁移已取消"
                exit 0
            fi
        fi
        
        # 创建 NodePort 服务
        create_nodeport_service
        
        # 等待服务更新
        if [[ "$DRY_RUN" == "false" ]]; then
            sleep 5
        fi
    fi
    
    # 创建 Istio Gateway 配置
    create_istio_gateway
    
    if [[ "$DRY_RUN" == "false" ]]; then
        # 等待配置生效
        sleep 10
        
        # 验证迁移结果
        verify_migration
        
        # 测试连通性
        test_connectivity
        
        # 显示配置信息
        show_configuration
        
        log_success "MinIO 迁移完成！"
    else
        log_info "DRY RUN 完成，使用 --no-dry-run 执行实际迁移"
    fi
}

# 执行主函数
main "$@"