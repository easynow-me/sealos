#!/bin/bash

# Phase 6: Full Production Cutover Script
# This script manages the complete migration from Ingress to Istio
# Usage: ./phase6-full-cutover.sh [--step STEP] [--dry-run] [--force]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/utils.sh"

# Configuration
DRY_RUN=false
FORCE=false
STEP="all"
BACKUP_DIR="/tmp/sealos-istio-backup-$(date +%Y%m%d-%H%M%S)"
LOG_FILE="/tmp/phase6-cutover-$(date +%Y%m%d-%H%M%S).log"

# Help function
show_help() {
    cat << EOF
Phase 6: Full Production Cutover Script

Usage: $0 [OPTIONS]

Options:
    --step STEP     Execute specific step (all, disable-ingress, migrate-existing, validate, cleanup)
    --dry-run       Show what would be done without making changes
    --force         Skip confirmation prompts
    --help          Show this help message

Steps:
    disable-ingress   Disable new Ingress creation
    migrate-existing  Migrate all existing Ingress to Istio
    validate         Validate all functionality
    cleanup          Clean up old resources

Example:
    $0 --step disable-ingress --dry-run
    $0 --step all --force
EOF
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --force)
            FORCE=true
            shift
            ;;
        --step)
            STEP="$2"
            shift 2
            ;;
        --help)
            show_help
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

# Logging function
log() {
    local level=$1
    shift
    local message="$@"
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] [$level] $message" | tee -a "$LOG_FILE"
}

# Step 1: Disable new Ingress creation
disable_ingress_creation() {
    log "INFO" "Step 1: Disabling new Ingress creation..."
    
    # Update all admission webhooks to reject Ingress creation
    if [[ "$DRY_RUN" == "true" ]]; then
        log "DRY-RUN" "Would update admission webhook to reject new Ingress resources"
        return
    fi
    
    # Create a new webhook configuration that blocks Ingress
    cat > /tmp/block-ingress-webhook.yaml << EOF
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionWebhookConfiguration
metadata:
  name: block-ingress-creation
webhooks:
- name: block-ingress.sealos.io
  clientConfig:
    service:
      name: admission-webhook-service
      namespace: sealos-system
      path: /block-ingress
  rules:
  - operations: ["CREATE"]
    apiGroups: ["networking.k8s.io"]
    apiVersions: ["v1"]
    resources: ["ingresses"]
  failurePolicy: Fail
  sideEffects: None
  admissionReviewVersions: ["v1"]
  namespaceSelector:
    matchExpressions:
    - key: name
      operator: NotIn
      values: ["kube-system", "istio-system", "sealos-system"]
EOF

    kubectl apply -f /tmp/block-ingress-webhook.yaml
    log "SUCCESS" "Ingress creation has been disabled"
    
    # Update all controllers to use Istio mode
    log "INFO" "Updating controllers to Istio-only mode..."
    
    # Update Terminal controller
    kubectl set env deployment/terminal-controller -n sealos-system \
        USE_ISTIO_NETWORKING=true \
        USE_INGRESS_NETWORKING=false
    
    # Update DB Adminer controller
    kubectl set env deployment/adminer-controller -n sealos-system \
        USE_ISTIO_NETWORKING=true \
        USE_INGRESS_NETWORKING=false
    
    # Update Resources controller
    kubectl set env deployment/resources-controller -n sealos-system \
        USE_ISTIO_NETWORKING=true \
        USE_INGRESS_NETWORKING=false
    
    log "SUCCESS" "All controllers updated to Istio-only mode"
}

# Step 2: Migrate existing Ingress resources
migrate_existing_ingress() {
    log "INFO" "Step 2: Migrating existing Ingress resources to Istio..."
    
    # Create backup
    mkdir -p "$BACKUP_DIR"
    kubectl get ingress --all-namespaces -o yaml > "$BACKUP_DIR/all-ingress-backup.yaml"
    log "INFO" "Backup created at: $BACKUP_DIR/all-ingress-backup.yaml"
    
    # Get all user namespaces
    namespaces=$(kubectl get namespaces -o json | jq -r '.items[] | select(.metadata.name | startswith("ns-")) | .metadata.name')
    
    total_ingress=0
    migrated_ingress=0
    failed_ingress=0
    
    for ns in $namespaces; do
        log "INFO" "Processing namespace: $ns"
        
        # Get all Ingress in namespace
        ingresses=$(kubectl get ingress -n "$ns" -o json | jq -r '.items[].metadata.name')
        
        for ingress in $ingresses; do
            total_ingress=$((total_ingress + 1))
            log "INFO" "Migrating Ingress: $ns/$ingress"
            
            if [[ "$DRY_RUN" == "true" ]]; then
                log "DRY-RUN" "Would migrate Ingress $ns/$ingress to Istio"
                migrated_ingress=$((migrated_ingress + 1))
                continue
            fi
            
            # Use the converter tool
            if "${SCRIPT_DIR}/../../tools/istio-migration/converter/sealos-ingress-converter" \
                -namespace "$ns" \
                -ingress-name "$ingress" \
                -output-dir "/tmp/istio-resources" \
                -gateway-name "sealos-gateway" \
                -apply; then
                
                migrated_ingress=$((migrated_ingress + 1))
                log "SUCCESS" "Migrated Ingress $ns/$ingress"
                
                # Add migration annotation
                kubectl annotate ingress "$ingress" -n "$ns" \
                    "sealos.io/migrated-to-istio=true" \
                    "sealos.io/migration-time=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
                    --overwrite
            else
                failed_ingress=$((failed_ingress + 1))
                log "ERROR" "Failed to migrate Ingress $ns/$ingress"
            fi
            
            # Small delay to avoid overwhelming the API server
            sleep 0.5
        done
    done
    
    log "INFO" "Migration summary: Total=$total_ingress, Migrated=$migrated_ingress, Failed=$failed_ingress"
    
    if [[ $failed_ingress -gt 0 ]]; then
        log "WARNING" "Some Ingress resources failed to migrate. Check the log for details."
        return 1
    fi
}

# Step 3: Validate all functionality
validate_functionality() {
    log "INFO" "Step 3: Validating all functionality..."
    
    validation_failed=false
    
    # Run comprehensive tests
    test_suites=(
        "multi-tenant"
        "protocols"
        "ssl-certificates"
        "cors"
        "performance"
    )
    
    for suite in "${test_suites[@]}"; do
        log "INFO" "Running $suite validation tests..."
        
        if [[ "$DRY_RUN" == "true" ]]; then
            log "DRY-RUN" "Would run $suite validation tests"
            continue
        fi
        
        if "${SCRIPT_DIR}/../../tests/istio-migration/scripts/run-test-suite.sh" "$suite"; then
            log "SUCCESS" "$suite tests passed"
        else
            log "ERROR" "$suite tests failed"
            validation_failed=true
        fi
    done
    
    # Check if all Istio resources are healthy
    log "INFO" "Checking Istio resource health..."
    
    # Check Gateways
    unhealthy_gateways=$(kubectl get gateways --all-namespaces -o json | \
        jq -r '.items[] | select(.status.conditions[]? | select(.type=="Ready" and .status!="True")) | "\(.metadata.namespace)/\(.metadata.name)"')
    
    if [[ -n "$unhealthy_gateways" ]]; then
        log "ERROR" "Unhealthy Gateways found: $unhealthy_gateways"
        validation_failed=true
    else
        log "SUCCESS" "All Gateways are healthy"
    fi
    
    # Check VirtualServices
    virtualservices=$(kubectl get virtualservices --all-namespaces -o json | jq -r '.items | length')
    log "INFO" "Total VirtualServices: $virtualservices"
    
    # Verify traffic flow
    log "INFO" "Verifying traffic flow..."
    sample_apps=$(kubectl get virtualservices --all-namespaces -o json | \
        jq -r '.items[0:5] | .[] | "\(.metadata.namespace)/\(.metadata.name)/\(.spec.hosts[0])"')
    
    for app_info in $sample_apps; do
        IFS='/' read -r ns name host <<< "$app_info"
        if [[ -n "$host" ]] && [[ "$host" != "null" ]]; then
            log "INFO" "Testing traffic to $host"
            
            if curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 "http://$host" | grep -q "^[23]"; then
                log "SUCCESS" "Traffic test passed for $host"
            else
                log "WARNING" "Traffic test failed for $host (this might be expected for some services)"
            fi
        fi
    done
    
    if [[ "$validation_failed" == "true" ]]; then
        log "ERROR" "Validation failed. Please check the errors above."
        return 1
    else
        log "SUCCESS" "All validations passed"
    fi
}

# Step 4: Clean up old resources
cleanup_old_resources() {
    log "INFO" "Step 4: Cleaning up old Ingress resources..."
    
    if [[ "$FORCE" != "true" ]] && [[ "$DRY_RUN" != "true" ]]; then
        echo -n "WARNING: This will delete all Ingress resources. Are you sure? (yes/no): "
        read -r response
        if [[ "$response" != "yes" ]]; then
            log "INFO" "Cleanup cancelled by user"
            return
        fi
    fi
    
    # Get all migrated Ingress resources
    migrated_ingresses=$(kubectl get ingress --all-namespaces \
        -l "sealos.io/migrated-to-istio=true" \
        -o json | jq -r '.items[] | "\(.metadata.namespace)/\(.metadata.name)"')
    
    total_cleaned=0
    
    for ingress_ref in $migrated_ingresses; do
        IFS='/' read -r ns name <<< "$ingress_ref"
        
        if [[ "$DRY_RUN" == "true" ]]; then
            log "DRY-RUN" "Would delete Ingress $ns/$name"
        else
            if kubectl delete ingress "$name" -n "$ns"; then
                log "SUCCESS" "Deleted Ingress $ns/$name"
                total_cleaned=$((total_cleaned + 1))
            else
                log "ERROR" "Failed to delete Ingress $ns/$name"
            fi
        fi
    done
    
    log "INFO" "Cleaned up $total_cleaned Ingress resources"
    
    # Remove the block-ingress webhook (if we want to allow Istio-only mode permanently)
    if [[ "$DRY_RUN" != "true" ]]; then
        kubectl delete validatingadmissionwebhookconfiguration block-ingress-creation || true
        log "INFO" "Removed Ingress blocking webhook"
    fi
}

# Execute steps based on selection
execute_step() {
    case "$STEP" in
        "all")
            disable_ingress_creation
            migrate_existing_ingress
            validate_functionality
            cleanup_old_resources
            ;;
        "disable-ingress")
            disable_ingress_creation
            ;;
        "migrate-existing")
            migrate_existing_ingress
            ;;
        "validate")
            validate_functionality
            ;;
        "cleanup")
            cleanup_old_resources
            ;;
        *)
            log "ERROR" "Unknown step: $STEP"
            show_help
            exit 1
            ;;
    esac
}

# Main execution
main() {
    log "INFO" "Starting Phase 6: Full Production Cutover"
    log "INFO" "Configuration: DRY_RUN=$DRY_RUN, FORCE=$FORCE, STEP=$STEP"
    log "INFO" "Log file: $LOG_FILE"
    
    # Check prerequisites
    command -v kubectl >/dev/null 2>&1 || { log "ERROR" "kubectl is required but not installed."; exit 1; }
    command -v jq >/dev/null 2>&1 || { log "ERROR" "jq is required but not installed."; exit 1; }
    
    # Verify cluster connection
    if ! kubectl cluster-info >/dev/null 2>&1; then
        log "ERROR" "Cannot connect to Kubernetes cluster"
        exit 1
    fi
    
    # Execute the selected step(s)
    execute_step
    
    log "INFO" "Phase 6 execution completed. Check log at: $LOG_FILE"
}

# Run main function
main