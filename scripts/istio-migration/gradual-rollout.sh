#!/bin/bash

# Istio Migration Gradual Rollout Script
# 用于控制从 Ingress 到 Istio 的渐进式流量切换

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
CONFIG_FILE=""
DRY_RUN=false
VERBOSE=false
PERCENTAGE=0
COMPONENT=""
ALL_COMPONENTS=false
NAMESPACE="sealos-system"
CONFIRM=true

# Components that support Istio migration
declare -A COMPONENTS=(
    ["terminal"]="terminal-controller"
    ["db-adminer"]="db-adminer-controller"
    ["resources"]="resources-controller"
    ["webhook"]="webhook-admission"
)

# Help function
show_help() {
    cat << EOF
Istio Migration Gradual Rollout Script

Usage: $0 [OPTIONS]

OPTIONS:
    -p, --percentage NUM      Traffic percentage to route to Istio (0-100)
    -c, --component NAME      Component to update (terminal|db-adminer|resources|webhook)
    --all-components          Update all components
    -n, --namespace NS        Kubernetes namespace (default: sealos-system)
    --config FILE             Configuration file path
    -v, --verbose             Verbose output
    -d, --dry-run            Show what would be done without executing
    --no-confirm             Skip confirmation prompts
    -h, --help               Show this help message

EXAMPLES:
    $0 --percentage 10 --component terminal    # Route 10% of terminal traffic to Istio
    $0 --percentage 50 --all-components        # Route 50% of all traffic to Istio
    $0 --percentage 0 --component terminal     # Rollback terminal to 100% Ingress
    $0 --dry-run --percentage 25 --all-components  # Preview changes
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

log_step() {
    echo -e "${PURPLE}[STEP]${NC} $1"
}

log_verbose() {
    if [[ "$VERBOSE" == "true" ]]; then
        echo -e "${BLUE}[VERBOSE]${NC} $1"
    fi
}

# Validate percentage
validate_percentage() {
    if [[ ! "$PERCENTAGE" =~ ^[0-9]+$ ]] || [[ "$PERCENTAGE" -lt 0 ]] || [[ "$PERCENTAGE" -gt 100 ]]; then
        log_error "Percentage must be a number between 0 and 100"
        exit 1
    fi
}

# Validate component
validate_component() {
    if [[ "$ALL_COMPONENTS" == "false" && -z "$COMPONENT" ]]; then
        log_error "Must specify either --component or --all-components"
        exit 1
    fi
    
    if [[ -n "$COMPONENT" && ! "${COMPONENTS[$COMPONENT]:-}" ]]; then
        log_error "Invalid component: $COMPONENT"
        log_error "Valid components: ${!COMPONENTS[*]}"
        exit 1
    fi
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
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
    
    # Check Istio installation
    if ! kubectl get namespace istio-system >/dev/null 2>&1; then
        log_error "Istio is not installed (istio-system namespace not found)"
        exit 1
    fi
    
    log_success "Prerequisites check passed"
}

# Get current rollout status
get_current_status() {
    local component="$1"
    local deployment="${COMPONENTS[$component]}"
    
    log_verbose "Getting current status for $component ($deployment)"
    
    # Check if deployment exists
    if ! kubectl get deployment "$deployment" -n "$NAMESPACE" >/dev/null 2>&1; then
        echo "deployment_not_found"
        return
    fi
    
    # Get current configuration
    local current_mode
    current_mode=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="NETWORKING_MODE")].value}' 2>/dev/null || echo "ingress")
    
    local current_percentage
    current_percentage=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="ISTIO_PERCENTAGE")].value}' 2>/dev/null || echo "0")
    
    echo "${current_mode}:${current_percentage}"
}

# Update component configuration
update_component() {
    local component="$1"
    local percentage="$2"
    local deployment="${COMPONENTS[$component]}"
    
    log_step "Updating $component to $percentage% Istio traffic..."
    
    # Determine networking mode based on percentage
    local networking_mode
    if [[ "$percentage" -eq 0 ]]; then
        networking_mode="ingress"
    elif [[ "$percentage" -eq 100 ]]; then
        networking_mode="istio"
    else
        networking_mode="dual"
    fi
    
    log_verbose "Setting networking mode: $networking_mode, percentage: $percentage"
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "DRY RUN: Would update $deployment with NETWORKING_MODE=$networking_mode, ISTIO_PERCENTAGE=$percentage"
        return
    fi
    
    # Update deployment environment variables
    kubectl patch deployment "$deployment" -n "$NAMESPACE" --type='merge' -p="{
        \"spec\": {
            \"template\": {
                \"spec\": {
                    \"containers\": [{
                        \"name\": \"controller\",
                        \"env\": [
                            {\"name\": \"NETWORKING_MODE\", \"value\": \"$networking_mode\"},
                            {\"name\": \"ISTIO_PERCENTAGE\", \"value\": \"$percentage\"},
                            {\"name\": \"ENABLE_ISTIO_MONITORING\", \"value\": \"true\"}
                        ]
                    }]
                }
            }
        }
    }"
    
    # Wait for rollout to complete
    log_info "Waiting for rollout to complete..."
    kubectl rollout status deployment/"$deployment" -n "$NAMESPACE" --timeout=300s
    
    log_success "Successfully updated $component"
}

# Monitor rollout
monitor_rollout() {
    local component="$1"
    local percentage="$2"
    
    log_info "Monitoring rollout for $component..."
    
    # Wait a bit for metrics to be available
    sleep 30
    
    # Check basic health
    local deployment="${COMPONENTS[$component]}"
    local replicas_ready
    replicas_ready=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.status.readyReplicas}')
    local replicas_desired
    replicas_desired=$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.spec.replicas}')
    
    if [[ "$replicas_ready" != "$replicas_desired" ]]; then
        log_warn "Deployment $deployment: $replicas_ready/$replicas_desired replicas ready"
    else
        log_success "Deployment $deployment: All replicas ready"
    fi
    
    # Check for recent errors in logs
    log_verbose "Checking recent logs for errors..."
    local error_count
    error_count=$(kubectl logs deployment/"$deployment" -n "$NAMESPACE" --since=60s | grep -i error | wc -l || echo "0")
    
    if [[ "$error_count" -gt 0 ]]; then
        log_warn "Found $error_count error messages in recent logs"
    else
        log_success "No errors found in recent logs"
    fi
}

# Verify rollout
verify_rollout() {
    local component="$1"
    local expected_percentage="$2"
    
    log_info "Verifying rollout for $component..."
    
    # Get current status
    local status
    status=$(get_current_status "$component")
    
    if [[ "$status" == "deployment_not_found" ]]; then
        log_error "Deployment not found for $component"
        return 1
    fi
    
    local current_mode="${status%%:*}"
    local current_percentage="${status##*:}"
    
    # Verify configuration
    local expected_mode
    if [[ "$expected_percentage" -eq 0 ]]; then
        expected_mode="ingress"
    elif [[ "$expected_percentage" -eq 100 ]]; then
        expected_mode="istio"
    else
        expected_mode="dual"
    fi
    
    if [[ "$current_mode" == "$expected_mode" && "$current_percentage" == "$expected_percentage" ]]; then
        log_success "✓ $component configuration verified: $current_mode mode, $current_percentage% Istio"
        return 0
    else
        log_error "✗ $component configuration mismatch: expected $expected_mode/$expected_percentage%, got $current_mode/$current_percentage%"
        return 1
    fi
}

# Generate rollout report
generate_report() {
    local timestamp
    timestamp=$(date -Iseconds)
    
    log_info "Generating rollout report..."
    
    local report_file="/tmp/istio-rollout-report-$(date +%Y%m%d-%H%M%S).json"
    
    # Collect status for all components
    local component_status=()
    
    if [[ "$ALL_COMPONENTS" == "true" ]]; then
        for comp in "${!COMPONENTS[@]}"; do
            local status
            status=$(get_current_status "$comp")
            component_status+=("\"$comp\": \"$status\"")
        done
    else
        local status
        status=$(get_current_status "$COMPONENT")
        component_status+=("\"$COMPONENT\": \"$status\"")
    fi
    
    # Create JSON report
    cat > "$report_file" << EOF
{
  "rollout_report": {
    "timestamp": "$timestamp",
    "target_percentage": $PERCENTAGE,
    "all_components": $ALL_COMPONENTS,
    "specific_component": "$COMPONENT",
    "namespace": "$NAMESPACE",
    "dry_run": $DRY_RUN,
    "component_status": {
      $(IFS=','; echo "${component_status[*]}")
    },
    "cluster_info": {
      "kubectl_version": "$(kubectl version --client --short 2>/dev/null | head -1)",
      "cluster_version": "$(kubectl version --short 2>/dev/null | tail -1)"
    }
  }
}
EOF
    
    log_success "Rollout report generated: $report_file"
    
    if [[ "$VERBOSE" == "true" ]]; then
        echo "Report contents:"
        cat "$report_file"
    fi
}

# Show current status
show_status() {
    log_info "Current Istio rollout status:"
    echo
    
    for component in "${!COMPONENTS[@]}"; do
        local status
        status=$(get_current_status "$component")
        
        if [[ "$status" == "deployment_not_found" ]]; then
            echo "  $component: Deployment not found"
        else
            local mode="${status%%:*}"
            local percentage="${status##*:}"
            echo "  $component: $mode mode, $percentage% Istio traffic"
        fi
    done
    echo
}

# Confirm action
confirm_action() {
    if [[ "$CONFIRM" == "false" || "$DRY_RUN" == "true" ]]; then
        return 0
    fi
    
    echo -e "${YELLOW}You are about to update the following:${NC}"
    
    if [[ "$ALL_COMPONENTS" == "true" ]]; then
        echo "  Components: ALL (${!COMPONENTS[*]})"
    else
        echo "  Component: $COMPONENT"
    fi
    
    echo "  Target Istio percentage: $PERCENTAGE%"
    echo "  Namespace: $NAMESPACE"
    echo
    
    read -p "Are you sure you want to continue? (y/N): " -n 1 -r
    echo
    
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        log_info "Operation cancelled by user"
        exit 0
    fi
}

# Main rollout function
execute_rollout() {
    local components_to_update=()
    
    if [[ "$ALL_COMPONENTS" == "true" ]]; then
        components_to_update=("${!COMPONENTS[@]}")
    else
        components_to_update=("$COMPONENT")
    fi
    
    local total_components=${#components_to_update[@]}
    local current_index=0
    
    for component in "${components_to_update[@]}"; do
        ((current_index++))
        log_info "Processing component $current_index/$total_components: $component"
        
        # Update component
        update_component "$component" "$PERCENTAGE"
        
        # Monitor rollout
        monitor_rollout "$component" "$PERCENTAGE"
        
        # Verify rollout
        if ! verify_rollout "$component" "$PERCENTAGE"; then
            log_error "Rollout verification failed for $component"
            exit 1
        fi
        
        # Brief pause between components
        if [[ $current_index -lt $total_components ]]; then
            sleep 10
        fi
    done
}

# Main function
main() {
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -p|--percentage)
                PERCENTAGE="$2"
                shift 2
                ;;
            -c|--component)
                COMPONENT="$2"
                shift 2
                ;;
            --all-components)
                ALL_COMPONENTS=true
                shift
                ;;
            -n|--namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            --config)
                CONFIG_FILE="$2"
                shift 2
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            -d|--dry-run)
                DRY_RUN=true
                shift
                ;;
            --no-confirm)
                CONFIRM=false
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
    
    # Show current status if no percentage specified
    if [[ $# -eq 0 || "$PERCENTAGE" == "0" && "$COMPONENT" == "" && "$ALL_COMPONENTS" == "false" ]]; then
        show_status
        exit 0
    fi
    
    # Validate inputs
    validate_percentage
    validate_component
    
    log_info "Starting Istio migration gradual rollout..."
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_warn "Running in dry-run mode - no changes will be made"
    fi
    
    # Check prerequisites
    check_prerequisites
    
    # Show current status
    show_status
    
    # Confirm action
    confirm_action
    
    # Execute rollout
    execute_rollout
    
    # Generate report
    generate_report
    
    # Show final status
    show_status
    
    log_success "Istio rollout completed successfully!"
    
    if [[ "$PERCENTAGE" -gt 0 ]]; then
        log_info "Monitor the system closely for the next 30 minutes"
        log_info "Use './emergency-rollback.sh' if issues are detected"
    fi
}

# Execute main function
main "$@"