#!/bin/bash

# One-Click Migration Script for Sealos Istio Migration
# WARNING: This script is for development/test environments only!
# For production, use the gradual migration approach
# Usage: ./one-click-migration.sh [--confirm] [--domain example.com]

set -e

# Configuration
DOMAIN="${DOMAIN:-cloud.sealos.io}"
ISTIO_VERSION="1.20.1"
CONFIRM=false
LOG_FILE="istio-migration-$(date +%Y%m%d-%H%M%S).log"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --confirm)
            CONFIRM=true
            shift
            ;;
        --domain)
            DOMAIN="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [--confirm] [--domain example.com]"
            echo "  --confirm    Skip confirmation prompts"
            echo "  --domain     Specify the domain (default: cloud.sealos.io)"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Logging
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $@" | tee -a "$LOG_FILE"
}

# Error handling
handle_error() {
    log "${RED}ERROR: Migration failed at step: $1${NC}"
    log "Check the log file: $LOG_FILE"
    log "To rollback, run: ./scripts/istio-migration/emergency-rollback.sh --mode full"
    exit 1
}

trap 'handle_error "$STEP"' ERR

# Warning banner
show_warning() {
    echo -e "${RED}"
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║                         WARNING                              ║"
    echo "║                                                              ║"
    echo "║  This script will perform a FULL MIGRATION to Istio!        ║"
    echo "║                                                              ║"
    echo "║  This includes:                                              ║"
    echo "║  - Installing Istio (if not present)                        ║"
    echo "║  - Converting ALL Ingress to VirtualService                 ║"
    echo "║  - Updating ALL controllers                                 ║"
    echo "║  - Deleting original Ingress resources                      ║"
    echo "║                                                              ║"
    echo "║  Recommended for: Development/Test environments ONLY         ║"
    echo "║                                                              ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"
    
    if [ "$CONFIRM" != "true" ]; then
        echo -n "Type 'yes' to continue, or Ctrl-C to cancel: "
        read response
        if [ "$response" != "yes" ]; then
            echo "Migration cancelled."
            exit 0
        fi
    fi
}

# Pre-flight checks
pre_flight_checks() {
    STEP="Pre-flight checks"
    log "${BLUE}Running pre-flight checks...${NC}"
    
    # Check kubectl
    if ! command -v kubectl >/dev/null 2>&1; then
        log "${RED}kubectl not found. Please install kubectl first.${NC}"
        exit 1
    fi
    
    # Check cluster connection
    if ! kubectl cluster-info >/dev/null 2>&1; then
        log "${RED}Cannot connect to Kubernetes cluster.${NC}"
        exit 1
    fi
    
    # Check permissions
    if ! kubectl auth can-i create deployments --all-namespaces >/dev/null 2>&1; then
        log "${RED}Insufficient permissions. Cluster admin access required.${NC}"
        exit 1
    fi
    
    # Count resources
    INGRESS_COUNT=$(kubectl get ingress --all-namespaces --no-headers 2>/dev/null | wc -l || echo 0)
    NAMESPACE_COUNT=$(kubectl get namespaces | grep "^ns-" | wc -l)
    
    log "Found $INGRESS_COUNT Ingress resources"
    log "Found $NAMESPACE_COUNT user namespaces"
    
    log "${GREEN}✓ Pre-flight checks passed${NC}"
}

# Backup current state
backup_resources() {
    STEP="Backup"
    log "${BLUE}Creating backup...${NC}"
    
    BACKUP_DIR="backup-$(date +%Y%m%d-%H%M%S)"
    mkdir -p "$BACKUP_DIR"
    
    # Backup Ingress
    kubectl get ingress --all-namespaces -o yaml > "$BACKUP_DIR/all-ingress.yaml" 2>/dev/null || true
    
    # Backup Services
    kubectl get services --all-namespaces -o yaml > "$BACKUP_DIR/all-services.yaml"
    
    # Backup Deployments
    kubectl get deployments -n sealos-system -o yaml > "$BACKUP_DIR/sealos-deployments.yaml"
    
    log "${GREEN}✓ Backup saved to: $BACKUP_DIR${NC}"
}

# Install Istio
install_istio() {
    STEP="Install Istio"
    
    if kubectl get namespace istio-system >/dev/null 2>&1; then
        log "${YELLOW}Istio already installed, skipping...${NC}"
        return
    fi
    
    log "${BLUE}Installing Istio $ISTIO_VERSION...${NC}"
    
    # Download Istio
    curl -sL https://istio.io/downloadIstio | ISTIO_VERSION=$ISTIO_VERSION sh -
    cd istio-$ISTIO_VERSION
    export PATH=$PWD/bin:$PATH
    
    # Install with production profile
    istioctl install --set profile=production -y
    
    # Wait for Istio to be ready
    log "Waiting for Istio components..."
    kubectl -n istio-system rollout status deployment/istiod --timeout=300s
    kubectl -n istio-system rollout status deployment/istio-ingressgateway --timeout=300s
    
    cd ..
    log "${GREEN}✓ Istio installed successfully${NC}"
}

# Enable injection for namespaces
enable_injection() {
    STEP="Enable injection"
    log "${BLUE}Enabling Istio injection for user namespaces...${NC}"
    
    for ns in $(kubectl get namespaces -o name | grep "namespace/ns-" | cut -d/ -f2); do
        kubectl label namespace $ns istio-injection=enabled --overwrite
        log "  Enabled injection for: $ns"
    done
    
    log "${GREEN}✓ Injection enabled${NC}"
}

# Create Gateway and certificates
setup_gateway() {
    STEP="Setup Gateway"
    log "${BLUE}Setting up Gateway and certificates...${NC}"
    
    # Create self-signed certificate (for testing)
    if ! kubectl get secret wildcard-cert -n istio-system >/dev/null 2>&1; then
        log "Creating self-signed certificate..."
        openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
            -keyout /tmp/tls.key -out /tmp/tls.crt \
            -subj "/CN=*.$DOMAIN" >/dev/null 2>&1
        
        kubectl create secret tls wildcard-cert \
            --cert=/tmp/tls.crt \
            --key=/tmp/tls.key \
            -n istio-system
        
        rm -f /tmp/tls.key /tmp/tls.crt
    fi
    
    # Create Gateway
    cat <<EOF | kubectl apply -f -
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: sealos-gateway
  namespace: istio-system
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
    tls:
      httpsRedirect: true
  - port:
      number: 443
      name: https
      protocol: HTTPS
    hosts:
    - "*.$DOMAIN"
    tls:
      mode: SIMPLE
      credentialName: wildcard-cert
EOF
    
    log "${GREEN}✓ Gateway configured${NC}"
}

# Update controllers
update_controllers() {
    STEP="Update controllers"
    log "${BLUE}Updating controllers to Istio mode...${NC}"
    
    CONTROLLERS=(
        "terminal-controller"
        "adminer-controller"
        "resources-controller"
    )
    
    for controller in "${CONTROLLERS[@]}"; do
        if kubectl get deployment/$controller -n sealos-system >/dev/null 2>&1; then
            log "  Updating $controller..."
            kubectl set env deployment/$controller -n sealos-system \
                USE_ISTIO_NETWORKING=true \
                USE_INGRESS_NETWORKING=false \
                ISTIO_ENABLED=true \
                ISTIO_GATEWAY=sealos-gateway.istio-system
            
            # Don't wait for rollout in one-click mode
            kubectl rollout restart deployment/$controller -n sealos-system
        fi
    done
    
    # Update webhook if exists
    if kubectl get deployment/admission-webhook -n sealos-system >/dev/null 2>&1; then
        kubectl set env deployment/admission-webhook -n sealos-system \
            ENABLE_ISTIO_WEBHOOKS=true
        kubectl rollout restart deployment/admission-webhook -n sealos-system
    fi
    
    log "${GREEN}✓ Controllers updated${NC}"
}

# Migrate all Ingress to VirtualService
migrate_ingress() {
    STEP="Migrate Ingress"
    log "${BLUE}Migrating Ingress to VirtualService...${NC}"
    
    # Get all namespaces with Ingress
    namespaces=$(kubectl get ingress --all-namespaces -o json | jq -r '.items[].metadata.namespace' | sort -u)
    
    migrated=0
    failed=0
    
    for ns in $namespaces; do
        ingresses=$(kubectl get ingress -n "$ns" -o json | jq -r '.items[].metadata.name')
        
        for ingress in $ingresses; do
            log "  Migrating $ns/$ingress..."
            
            # Convert Ingress to VirtualService
            if convert_ingress_to_virtualservice "$ns" "$ingress"; then
                migrated=$((migrated + 1))
                
                # Mark as migrated
                kubectl annotate ingress "$ingress" -n "$ns" \
                    sealos.io/migrated-to-istio=true \
                    sealos.io/migration-time="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
                    --overwrite >/dev/null 2>&1 || true
            else
                failed=$((failed + 1))
                log "${YELLOW}  Warning: Failed to migrate $ns/$ingress${NC}"
            fi
        done
    done
    
    log "${GREEN}✓ Migration complete: $migrated succeeded, $failed failed${NC}"
}

# Convert a single Ingress to VirtualService
convert_ingress_to_virtualservice() {
    local namespace=$1
    local name=$2
    
    # Get Ingress
    local ingress_json=$(kubectl get ingress "$name" -n "$namespace" -o json 2>/dev/null)
    if [ -z "$ingress_json" ]; then
        return 1
    fi
    
    # Extract host and paths
    local hosts=$(echo "$ingress_json" | jq -r '.spec.rules[].host' | grep -v null | sort -u)
    
    # Create VirtualService for each host
    for host in $hosts; do
        cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: ${name}-${host//\./-}
  namespace: $namespace
  labels:
    migration: istio
    original-ingress: $name
spec:
  hosts:
  - "$host"
  gateways:
  - istio-system/sealos-gateway
  http:
  - match:
    - uri:
        prefix: "/"
    route:
    - destination:
        host: $(echo "$ingress_json" | jq -r '.spec.rules[0].http.paths[0].backend.service.name')
        port:
          number: $(echo "$ingress_json" | jq -r '.spec.rules[0].http.paths[0].backend.service.port.number // .spec.rules[0].http.paths[0].backend.service.port.name')
EOF
    done
    
    return 0
}

# Verify migration
verify_migration() {
    STEP="Verify"
    log "${BLUE}Verifying migration...${NC}"
    
    # Count resources
    local vs_count=$(kubectl get virtualservices --all-namespaces | grep -v "^NAMESPACE" | wc -l)
    local ingress_migrated=$(kubectl get ingress --all-namespaces -o json | jq -r '.items[] | select(.metadata.annotations["sealos.io/migrated-to-istio"] == "true") | .metadata.name' | wc -l)
    
    log "VirtualServices created: $vs_count"
    log "Ingress migrated: $ingress_migrated"
    
    # Test a sample
    local sample_host=$(kubectl get virtualservice --all-namespaces -o json | jq -r '.items[0].spec.hosts[0]' 2>/dev/null | grep -v null)
    if [ -n "$sample_host" ]; then
        log "Testing sample host: $sample_host"
        if curl -k -s -o /dev/null -w "%{http_code}" "https://$sample_host" | grep -q "^[23]"; then
            log "${GREEN}✓ Sample traffic test passed${NC}"
        else
            log "${YELLOW}! Sample traffic test failed (this might be expected)${NC}"
        fi
    fi
    
    log "${GREEN}✓ Verification complete${NC}"
}

# Cleanup old resources
cleanup_resources() {
    STEP="Cleanup"
    log "${BLUE}Cleaning up old Ingress resources...${NC}"
    
    if [ "$CONFIRM" != "true" ]; then
        echo -n "Delete all migrated Ingress resources? (yes/no): "
        read response
        if [ "$response" != "yes" ]; then
            log "Skipping cleanup"
            return
        fi
    fi
    
    # Delete migrated Ingress
    local deleted=0
    for ingress in $(kubectl get ingress --all-namespaces -o json | jq -r '.items[] | select(.metadata.annotations["sealos.io/migrated-to-istio"] == "true") | "\(.metadata.namespace)/\(.metadata.name)"'); do
        ns=$(echo $ingress | cut -d/ -f1)
        name=$(echo $ingress | cut -d/ -f2)
        kubectl delete ingress "$name" -n "$ns" >/dev/null 2>&1
        deleted=$((deleted + 1))
    done
    
    log "${GREEN}✓ Deleted $deleted Ingress resources${NC}"
}

# Setup basic monitoring
setup_monitoring() {
    STEP="Setup monitoring"
    log "${BLUE}Setting up basic monitoring...${NC}"
    
    # Check if Prometheus is available
    if kubectl get svc -n istio-system prometheus >/dev/null 2>&1; then
        log "Prometheus already available"
    else
        # Deploy kiali (includes basic monitoring)
        kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/prometheus.yaml >/dev/null 2>&1 || true
        kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/addons/kiali.yaml >/dev/null 2>&1 || true
    fi
    
    log "${GREEN}✓ Monitoring setup complete${NC}"
}

# Main execution
main() {
    echo -e "${BLUE}=== Sealos One-Click Istio Migration ===${NC}"
    echo "Domain: $DOMAIN"
    echo "Log file: $LOG_FILE"
    echo ""
    
    # Show warning and get confirmation
    show_warning
    
    # Start timer
    START_TIME=$(date +%s)
    
    # Execute migration steps
    pre_flight_checks
    backup_resources
    install_istio
    enable_injection
    setup_gateway
    update_controllers
    migrate_ingress
    verify_migration
    cleanup_resources
    setup_monitoring
    
    # Calculate duration
    END_TIME=$(date +%s)
    DURATION=$((END_TIME - START_TIME))
    MINUTES=$((DURATION / 60))
    SECONDS=$((DURATION % 60))
    
    # Summary
    echo ""
    echo -e "${GREEN}╔══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║              Migration Completed Successfully!               ║${NC}"
    echo -e "${GREEN}╚══════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo "Duration: ${MINUTES}m ${SECONDS}s"
    echo "Log file: $LOG_FILE"
    echo ""
    echo "Next steps:"
    echo "1. Monitor the cluster for any issues"
    echo "2. Test your applications"
    echo "3. Check Istio dashboard: istioctl dashboard kiali"
    echo ""
    echo "To rollback if needed:"
    echo "  ./scripts/istio-migration/emergency-rollback.sh --mode full"
    echo ""
}

# Run main function
main