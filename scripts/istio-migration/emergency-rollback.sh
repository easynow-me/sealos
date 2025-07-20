#!/bin/bash

# Istio Migration Emergency Rollback Script
# 紧急回滚脚本，用于快速将流量从 Istio 切换回 Ingress

set -euo pipefail

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
NC='\033[0m' # No Color

# Default values
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NAMESPACE="sealos-system"
MODE="ingress"  # ingress, dual
PERCENTAGE=0    # For dual mode
FORCE=false
VERBOSE=false
REASON=""

# Components that support Istio migration
declare -A COMPONENTS=(
    ["terminal"]="terminal-controller"
    ["db-adminer"]="db-adminer-controller"
    ["resources"]="resources-controller" 
    ["webhook"]="webhook-admission"
)

# Emergency contacts
EMERGENCY_CONTACTS=(
    "sre-team@sealos.io"
    "dev-team@sealos.io"
    "on-call@sealos.io"
)

# Help function
show_help() {
    cat << EOF
Istio Migration Emergency Rollback Script

Usage: $0 [OPTIONS]

OPTIONS:
    -m, --mode MODE           Rollback mode (ingress|dual) [default: ingress]
    -p, --percentage NUM      For dual mode: percentage of Istio traffic (0-100) [default: 0]
    -n, --namespace NS        Kubernetes namespace [default: sealos-system]
    -r, --reason TEXT         Reason for emergency rollback (required)
    -f, --force              Skip confirmation prompts
    -v, --verbose            Verbose output
    -h, --help               Show this help message

MODES:
    ingress                   Complete rollback to Ingress (0% Istio)
    dual                      Partial rollback, keep some Istio traffic

EXAMPLES:
    $0 --reason "High error rate detected"                    # Full rollback to Ingress
    $0 --mode dual --percentage 10 --reason "Performance issue"  # Keep 10% Istio
    $0 --force --reason "Service outage"                      # Emergency rollback without confirmation
EOF
}

# Logging functions
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

log_emergency() {
    echo -e "${RED}[EMERGENCY]${NC} $1"
}

log_verbose() {
    if [[ "$VERBOSE" == "true" ]]; then
        echo -e "${BLUE}[VERBOSE]${NC} $1"
    fi
}

# Send emergency notification
send_emergency_notification() {
    local subject="$1"
    local message="$2"
    
    log_info "Sending emergency notifications..."
    
    # Log to system logs
    logger -t "istio-emergency-rollback" "$subject: $message"
    
    # Write to notification file for external systems to pick up
    local notification_file="/tmp/emergency-rollback-$(date +%Y%m%d-%H%M%S).alert"
    cat > "$notification_file" << EOF
{
  "timestamp": "$(date -Iseconds)",
  "level": "EMERGENCY",
  "subject": "$subject",
  "message": "$message",
  "cluster": "$(kubectl config current-context 2>/dev/null || echo 'unknown')",
  "namespace": "$NAMESPACE",
  "contacts": $(printf '"%s",' "${EMERGENCY_CONTACTS[@]}" | sed 's/,$//'),
  "reason": "$REASON"
}
EOF
    
    log_warn "Emergency notification written to: $notification_file"
    
    # If slack webhook is configured, send notification
    if [[ -n "${SLACK_WEBHOOK_URL:-}" ]]; then
        curl -X POST -H 'Content-type: application/json' \
            --data "{\"text\":\"🚨 EMERGENCY ROLLBACK: $subject\\n$message\\nReason: $REASON\"}" \
            "$SLACK_WEBHOOK_URL" 2>/dev/null || log_warn "Failed to send Slack notification"
    fi
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking emergency rollback prerequisites..."
    
    # Check kubectl
    if ! command -v kubectl >/dev/null 2>&1; then
        log_error "kubectl is required but not installed"
        exit 1
    fi
    
    # Check cluster connectivity
    if ! kubectl cluster-info >/dev/null 2>&1; then
        log_error "Cannot connect to Kubernetes cluster"
        exit 1
    fi
    
    # Check namespace exists
    if ! kubectl get namespace "$NAMESPACE" >/dev/null 2>&1; then
        log_error "Namespace $NAMESPACE does not exist"
        exit 1
    fi
    
    # Check if controllers are running
    local missing_controllers=()
    for component in "${!COMPONENTS[@]}"; do
        local deployment="${COMPONENTS[$component]}"
        if ! kubectl get deployment "$deployment" -n "$NAMESPACE" >/dev/null 2>&1; then
            missing_controllers+=("$deployment")
        fi
    done
    
    if [[ ${#missing_controllers[@]} -gt 0 ]]; then
        log_warn "Some controllers not found: ${missing_controllers[*]}"
        log_warn "Will proceed with available controllers only"
    fi
    
    log_success "Prerequisites check completed"
}

# Get system health snapshot
get_system_snapshot() {
    log_info "Capturing system health snapshot..."
    
    local snapshot_file="/tmp/system-snapshot-$(date +%Y%m%d-%H%M%S).json"
    
    # Collect current status
    local component_status=()
    local pod_status=()
    
    # Get component status
    for component in "${!COMPONENTS[@]}"; do
        local deployment="${COMPONENTS[$component]}"
        if kubectl get deployment "$deployment" -n "$NAMESPACE" >/dev/null 2>&1; then
            local replicas_ready
            replicas_ready=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
            local replicas_desired
            replicas_desired=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "0")
            
            local networking_mode
            networking_mode=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="NETWORKING_MODE")].value}' 2>/dev/null || echo "unknown")
            
            local istio_percentage
            istio_percentage=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="ISTIO_PERCENTAGE")].value}' 2>/dev/null || echo "0")
            
            component_status+=("\"$component\": {\"ready\": $replicas_ready, \"desired\": $replicas_desired, \"mode\": \"$networking_mode\", \"istio_percentage\": $istio_percentage}")
        fi
    done
    
    # Get pod status summary
    local total_pods
    total_pods=$(kubectl get pods -n "$NAMESPACE" --no-headers | wc -l)
    local running_pods
    running_pods=$(kubectl get pods -n "$NAMESPACE" --no-headers | grep Running | wc -l)
    
    # Create snapshot
    cat > "$snapshot_file" << EOF
{
  "snapshot": {
    "timestamp": "$(date -Iseconds)",
    "reason": "$REASON",
    "cluster": "$(kubectl config current-context 2>/dev/null || echo 'unknown')",
    "namespace": "$NAMESPACE",
    "components": {
      $(IFS=','; echo "${component_status[*]}")
    },
    "pod_summary": {
      "total": $total_pods,
      "running": $running_pods
    },
    "istio_status": {
      "proxy_version": "$(kubectl get pods -n istio-system -l app=istiod -o jsonpath='{.items[0].metadata.labels.version}' 2>/dev/null || echo 'unknown')",
      "gateway_ready": $(kubectl get pods -n istio-system -l app=istio-ingressgateway --no-headers | grep Running | wc -l)
    }
  }
}
EOF
    
    log_info "System snapshot saved: $snapshot_file"
    
    if [[ "$VERBOSE" == "true" ]]; then
        echo "Snapshot contents:"
        cat "$snapshot_file"
    fi
}

# Execute emergency rollback for a component
rollback_component() {
    local component="$1"
    local deployment="${COMPONENTS[$component]}"
    
    log_emergency "Emergency rollback for $component..."
    
    # Check if deployment exists
    if ! kubectl get deployment "$deployment" -n "$NAMESPACE" >/dev/null 2>&1; then
        log_warn "Deployment $deployment not found, skipping"
        return
    fi
    
    # Determine target configuration
    local target_mode="$MODE"
    local target_percentage="$PERCENTAGE"
    
    if [[ "$MODE" == "ingress" ]]; then
        target_mode="ingress"
        target_percentage="0"
    fi
    
    log_info "Setting $component to $target_mode mode with $target_percentage% Istio traffic"
    
    # Apply emergency configuration
    kubectl patch deployment "$deployment" -n "$NAMESPACE" --type='merge' -p="{
        \"spec\": {
            \"template\": {
                \"spec\": {
                    \"containers\": [{
                        \"name\": \"controller\",
                        \"env\": [
                            {\"name\": \"NETWORKING_MODE\", \"value\": \"$target_mode\"},
                            {\"name\": \"ISTIO_PERCENTAGE\", \"value\": \"$target_percentage\"},
                            {\"name\": \"ENABLE_ISTIO_MONITORING\", \"value\": \"true\"},
                            {\"name\": \"EMERGENCY_ROLLBACK\", \"value\": \"true\"},
                            {\"name\": \"ROLLBACK_TIMESTAMP\", \"value\": \"$(date -Iseconds)\"}
                        ]
                    }]
                }
            }
        }
    }"
    
    # Force immediate rollout
    kubectl rollout restart deployment/"$deployment" -n "$NAMESPACE"
    
    log_info "Waiting for emergency rollback to complete for $component..."
    
    # Wait for rollout with shorter timeout for emergency
    if ! kubectl rollout status deployment/"$deployment" -n "$NAMESPACE" --timeout=180s; then
        log_error "Emergency rollback timeout for $component"
        return 1
    fi
    
    log_success "Emergency rollback completed for $component"
}

# Verify rollback
verify_rollback() {
    log_info "Verifying emergency rollback..."
    
    local failed_components=()
    local success_count=0
    
    for component in "${!COMPONENTS[@]}"; do
        local deployment="${COMPONENTS[$component]}"
        
        if ! kubectl get deployment "$deployment" -n "$NAMESPACE" >/dev/null 2>&1; then
            continue
        fi
        
        # Check if all replicas are ready
        local replicas_ready
        replicas_ready=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
        local replicas_desired
        replicas_desired=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "0")
        
        if [[ "$replicas_ready" != "$replicas_desired" || "$replicas_ready" == "0" ]]; then
            failed_components+=("$component")
            log_error "✗ $component: $replicas_ready/$replicas_desired replicas ready"
        else
            ((success_count++))
            log_success "✓ $component: All replicas ready"
        fi
        
        # Check networking configuration
        local current_mode
        current_mode=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="NETWORKING_MODE")].value}' 2>/dev/null || echo "unknown")
        
        local current_percentage
        current_percentage=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="ISTIO_PERCENTAGE")].value}' 2>/dev/null || echo "0")
        
        log_verbose "$component: mode=$current_mode, istio_percentage=$current_percentage%"
    done
    
    if [[ ${#failed_components[@]} -eq 0 ]]; then
        log_success "All components ($success_count) successfully rolled back"
        return 0
    else
        log_error "Failed components: ${failed_components[*]}"
        return 1
    fi
}

# Check post-rollback health
check_post_rollback_health() {
    log_info "Checking post-rollback system health..."
    
    # Wait for system to stabilize
    sleep 30
    
    # Check error rates in logs
    local total_errors=0
    for component in "${!COMPONENTS[@]}"; do
        local deployment="${COMPONENTS[$component]}"
        
        if kubectl get deployment "$deployment" -n "$NAMESPACE" >/dev/null 2>&1; then
            local error_count
            error_count=$(kubectl logs deployment/"$deployment" -n "$NAMESPACE" --since=60s | grep -i error | wc -l || echo "0")
            total_errors=$((total_errors + error_count))
            
            if [[ "$error_count" -gt 0 ]]; then
                log_warn "$component: $error_count errors in last 60 seconds"
            fi
        fi
    done
    
    if [[ "$total_errors" -eq 0 ]]; then
        log_success "No errors detected in recent logs"
    else
        log_warn "Total errors in last 60 seconds: $total_errors"
    fi
    
    # Check if critical pods are running
    local critical_pods=("istio-ingressgateway" "istiod")
    for pod_label in "${critical_pods[@]}"; do
        local pod_count
        pod_count=$(kubectl get pods -n istio-system -l app="$pod_label" --no-headers | grep Running | wc -l)
        
        if [[ "$pod_count" -gt 0 ]]; then
            log_success "✓ $pod_label: $pod_count pod(s) running"
        else
            log_warn "⚠ $pod_label: No running pods found"
        fi
    done
}

# Generate rollback report
generate_rollback_report() {
    local timestamp
    timestamp=$(date -Iseconds)
    
    log_info "Generating emergency rollback report..."
    
    local report_file="/tmp/emergency-rollback-report-$(date +%Y%m%d-%H%M%S).json"
    
    # Collect final status
    local component_status=()
    for component in "${!COMPONENTS[@]}"; do
        local deployment="${COMPONENTS[$component]}"
        if kubectl get deployment "$deployment" -n "$NAMESPACE" >/dev/null 2>&1; then
            local mode
            mode=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="NETWORKING_MODE")].value}' 2>/dev/null || echo "unknown")
            local percentage
            percentage=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="ISTIO_PERCENTAGE")].value}' 2>/dev/null || echo "0")
            
            component_status+=("\"$component\": {\"mode\": \"$mode\", \"istio_percentage\": $percentage}")
        fi
    done
    
    # Create report
    cat > "$report_file" << EOF
{
  "emergency_rollback_report": {
    "timestamp": "$timestamp",
    "reason": "$REASON",
    "target_mode": "$MODE",
    "target_percentage": $PERCENTAGE,
    "namespace": "$NAMESPACE",
    "cluster": "$(kubectl config current-context 2>/dev/null || echo 'unknown')",
    "execution_time": "$(date -Iseconds)",
    "component_final_status": {
      $(IFS=','; echo "${component_status[*]}")
    },
    "emergency_contacts_notified": $(printf '"%s",' "${EMERGENCY_CONTACTS[@]}" | sed 's/,$//'),
    "operator": "$(whoami)@$(hostname)"
  }
}
EOF
    
    log_success "Emergency rollback report generated: $report_file"
    
    # Show summary
    echo
    log_emergency "EMERGENCY ROLLBACK SUMMARY"
    echo "  Reason: $REASON"
    echo "  Target Mode: $MODE"
    echo "  Target Istio %: $PERCENTAGE%"
    echo "  Components: ${!COMPONENTS[*]}"
    echo "  Report: $report_file"
    echo
}

# Confirm emergency action
confirm_emergency_action() {
    if [[ "$FORCE" == "true" ]]; then
        return 0
    fi
    
    echo
    log_emergency "EMERGENCY ROLLBACK CONFIRMATION"
    echo -e "${RED}You are about to perform an emergency rollback!${NC}"
    echo
    echo "  Target Mode: $MODE"
    echo "  Target Istio Percentage: $PERCENTAGE%"
    echo "  Namespace: $NAMESPACE"
    echo "  Reason: $REASON"
    echo "  Components: ${!COMPONENTS[*]}"
    echo
    echo -e "${YELLOW}This action will immediately change traffic routing for all components.${NC}"
    echo
    
    read -p "Type 'EMERGENCY' to confirm: " -r
    
    if [[ "$REPLY" != "EMERGENCY" ]]; then
        log_info "Emergency rollback cancelled"
        exit 0
    fi
}

# Main function
main() {
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -m|--mode)
                MODE="$2"
                shift 2
                ;;
            -p|--percentage)
                PERCENTAGE="$2"
                shift 2
                ;;
            -n|--namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            -r|--reason)
                REASON="$2"
                shift 2
                ;;
            -f|--force)
                FORCE=true
                shift
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    # Validate inputs
    if [[ -z "$REASON" ]]; then
        log_error "Reason for emergency rollback is required (--reason)"
        exit 1
    fi
    
    if [[ "$MODE" != "ingress" && "$MODE" != "dual" ]]; then
        log_error "Invalid mode: $MODE (must be 'ingress' or 'dual')"
        exit 1
    fi
    
    if [[ "$MODE" == "dual" && ("$PERCENTAGE" -lt 0 || "$PERCENTAGE" -gt 100) ]]; then
        log_error "For dual mode, percentage must be between 0 and 100"
        exit 1
    fi
    
    log_emergency "Starting emergency rollback procedure..."
    log_emergency "Reason: $REASON"
    
    # Send immediate notification
    send_emergency_notification "Emergency Rollback Initiated" "Emergency rollback started for namespace $NAMESPACE. Reason: $REASON"
    
    # Check prerequisites
    check_prerequisites
    
    # Capture system snapshot
    get_system_snapshot
    
    # Confirm action
    confirm_emergency_action
    
    # Execute emergency rollback
    log_emergency "Executing emergency rollback..."
    
    local start_time
    start_time=$(date +%s)
    
    # Rollback all components in parallel for speed
    local pids=()
    for component in "${!COMPONENTS[@]}"; do
        rollback_component "$component" &
        pids+=($!)
    done
    
    # Wait for all rollbacks to complete
    for pid in "${pids[@]}"; do
        if ! wait "$pid"; then
            log_error "One or more component rollbacks failed"
        fi
    done
    
    local end_time
    end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    log_info "Emergency rollback execution completed in ${duration}s"
    
    # Verify rollback
    if verify_rollback; then
        log_success "Emergency rollback verification passed"
    else
        log_error "Emergency rollback verification failed"
        send_emergency_notification "Emergency Rollback Verification Failed" "Some components failed rollback verification"
    fi
    
    # Check post-rollback health
    check_post_rollback_health
    
    # Generate report
    generate_rollback_report
    
    # Final notification
    send_emergency_notification "Emergency Rollback Completed" "Emergency rollback completed in ${duration}s. All components set to $MODE mode with $PERCENTAGE% Istio traffic."
    
    log_emergency "Emergency rollback procedure completed!"
    log_info "Continue monitoring the system closely"
    log_info "Review logs and metrics to ensure stability"
}

# Execute main function
main "$@"