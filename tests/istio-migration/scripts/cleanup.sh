#!/bin/bash

# Istio Migration Test Cleanup Script
# Cleans up all test resources created during integration testing

set -euo pipefail

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_DIR="$(dirname "$SCRIPT_DIR")"
CONFIG_FILE="$TEST_DIR/config/test-config.yaml"
FORCE=false
VERBOSE=false
DRY_RUN=false

# Help function
show_help() {
    cat << EOF
Istio Migration Test Cleanup Script

Usage: $0 [OPTIONS]

OPTIONS:
    -c, --config FILE      Use custom config file (default: config/test-config.yaml)
    -f, --force           Force cleanup without confirmation
    -v, --verbose         Verbose output
    -d, --dry-run         Show what would be deleted without actually deleting
    -h, --help            Show this help message

EXAMPLES:
    $0                    # Interactive cleanup with confirmation
    $0 --force            # Force cleanup without confirmation
    $0 --dry-run          # Show what would be cleaned up
    $0 --verbose --force  # Verbose forced cleanup
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

log_verbose() {
    if [[ "$VERBOSE" == "true" ]]; then
        echo -e "${BLUE}[VERBOSE]${NC} $1"
    fi
}

# Confirm cleanup
confirm_cleanup() {
    if [[ "$FORCE" == "true" ]]; then
        return 0
    fi
    
    echo -e "${YELLOW}WARNING: This will delete all test resources created during Istio migration testing.${NC}"
    echo
    echo "This includes:"
    echo "  - Test namespaces and all resources within them"
    echo "  - Test applications, services, gateways, virtual services"
    echo "  - Test certificates and secrets"
    echo "  - Test reports (if --include-reports is specified)"
    echo
    read -p "Are you sure you want to continue? (y/N): " -n 1 -r
    echo
    
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        log_info "Cleanup cancelled by user"
        exit 0
    fi
}

# Parse configuration
parse_config() {
    if [[ ! -f "$CONFIG_FILE" ]]; then
        log_warn "Config file not found: $CONFIG_FILE"
        log_info "Using default configuration"
        return
    fi
    
    log_info "Parsing configuration from $CONFIG_FILE..."
    
    # Extract key configuration values
    TEST_NAMESPACE_PREFIX=$(yq e '.environments.dev.namespace_prefix' "$CONFIG_FILE" 2>/dev/null || echo "test-dev")
    TEST_NAMESPACE_COUNT=$(yq e '.functional.multi_tenant.test_namespaces' "$CONFIG_FILE" 2>/dev/null || echo "3")
    
    log_verbose "Test namespace prefix: $TEST_NAMESPACE_PREFIX"
    log_verbose "Test namespace count: $TEST_NAMESPACE_COUNT"
}

# Clean up test namespaces
cleanup_test_namespaces() {
    log_info "Cleaning up test namespaces..."
    
    # Get all test namespaces
    local test_namespaces
    test_namespaces=$(kubectl get namespaces -l istio-test=true -o name 2>/dev/null || echo "")
    
    if [[ -z "$test_namespaces" ]]; then
        log_info "No test namespaces found with label 'istio-test=true'"
        return
    fi
    
    local namespace_count
    namespace_count=$(echo "$test_namespaces" | wc -l)
    log_info "Found $namespace_count test namespaces to clean up"
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Would delete the following namespaces:"
        echo "$test_namespaces" | sed 's/namespace\//  - /'
        return
    fi
    
    # Delete each namespace
    while IFS= read -r namespace; do
        if [[ -n "$namespace" ]]; then
            local ns_name="${namespace#namespace/}"
            log_verbose "Deleting namespace: $ns_name"
            
            if ! kubectl delete "$namespace" --timeout=60s 2>/dev/null; then
                log_warn "Failed to delete namespace: $ns_name"
            else
                log_verbose "Successfully deleted namespace: $ns_name"
            fi
        fi
    done <<< "$test_namespaces"
    
    # Wait for namespaces to be fully deleted
    log_info "Waiting for namespaces to be fully deleted..."
    local timeout=120
    local count=0
    
    while [[ $count -lt $timeout ]]; do
        local remaining
        remaining=$(kubectl get namespaces -l istio-test=true --no-headers 2>/dev/null | wc -l)
        
        if [[ "$remaining" -eq 0 ]]; then
            log_success "All test namespaces deleted successfully"
            return
        fi
        
        sleep 1
        ((count++))
        
        if [[ $((count % 10)) -eq 0 ]]; then
            log_verbose "Waiting for namespace deletion... ($count/${timeout}s)"
        fi
    done
    
    log_warn "Some namespaces may still be terminating after ${timeout}s timeout"
}

# Clean up test resources in existing namespaces
cleanup_test_resources() {
    log_info "Cleaning up test resources in existing namespaces..."
    
    # Define test labels to identify resources
    local test_labels=(
        "test-type=protocol"
        "test-type=multi-tenant"
        "test-type=latency"
        "test-type=performance"
        "test-type=functional"
        "istio-test=true"
    )
    
    # Resource types to clean up
    local resource_types=(
        "deployment"
        "service"
        "gateway"
        "virtualservice"
        "ingress"
        "configmap"
        "secret"
        "pod"
    )
    
    # Get all namespaces (not just test namespaces)
    local all_namespaces
    all_namespaces=$(kubectl get namespaces -o name 2>/dev/null | sed 's/namespace\///')
    
    while IFS= read -r namespace; do
        if [[ -n "$namespace" && "$namespace" != "kube-system" && "$namespace" != "istio-system" ]]; then
            log_verbose "Checking namespace: $namespace"
            
            for label in "${test_labels[@]}"; do
                for resource_type in "${resource_types[@]}"; do
                    if [[ "$DRY_RUN" == "true" ]]; then
                        local resources
                        resources=$(kubectl get "$resource_type" -n "$namespace" -l "$label" --no-headers 2>/dev/null | wc -l)
                        if [[ "$resources" -gt 0 ]]; then
                            log_info "Would delete $resources $resource_type resources with label '$label' in namespace $namespace"
                        fi
                    else
                        local deleted
                        deleted=$(kubectl delete "$resource_type" -n "$namespace" -l "$label" --ignore-not-found=true 2>/dev/null | wc -l)
                        if [[ "$deleted" -gt 0 ]]; then
                            log_verbose "Deleted $resource_type resources with label '$label' in namespace $namespace"
                        fi
                    fi
                done
            done
        fi
    done <<< "$all_namespaces"
}

# Clean up cluster-wide test resources
cleanup_cluster_resources() {
    log_info "Cleaning up cluster-wide test resources..."
    
    # Clean up any cluster-wide resources that might have been created during testing
    local cluster_resource_types=(
        "clusterrole"
        "clusterrolebinding"
    )
    
    for resource_type in "${cluster_resource_types[@]}"; do
        if [[ "$DRY_RUN" == "true" ]]; then
            local resources
            resources=$(kubectl get "$resource_type" -l istio-test=true --no-headers 2>/dev/null | wc -l)
            if [[ "$resources" -gt 0 ]]; then
                log_info "Would delete $resources cluster-wide $resource_type resources"
            fi
        else
            kubectl delete "$resource_type" -l istio-test=true --ignore-not-found=true 2>/dev/null
            log_verbose "Cleaned up cluster-wide $resource_type resources"
        fi
    done
}

# Clean up test reports
cleanup_test_reports() {
    log_info "Cleaning up test reports..."
    
    local reports_dir="$TEST_DIR/reports"
    
    if [[ ! -d "$reports_dir" ]]; then
        log_info "No reports directory found"
        return
    fi
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Would delete reports directory: $reports_dir"
        if [[ -d "$reports_dir" ]]; then
            local file_count
            file_count=$(find "$reports_dir" -type f | wc -l)
            log_info "Would delete $file_count report files"
        fi
        return
    fi
    
    if [[ -d "$reports_dir" ]]; then
        local file_count
        file_count=$(find "$reports_dir" -type f | wc -l)
        
        rm -rf "$reports_dir"
        log_info "Deleted reports directory with $file_count files"
    fi
}

# Clean up dangling test pods
cleanup_dangling_pods() {
    log_info "Cleaning up dangling test pods..."
    
    # Find pods that might have been left running from failed tests
    local test_pod_patterns=(
        "test-client-"
        "http-test-client"
        "grpc-test-client"
        "ws-test-client"
        "latency-test-"
        "performance-test-"
    )
    
    for pattern in "${test_pod_patterns[@]}"; do
        local dangling_pods
        dangling_pods=$(kubectl get pods --all-namespaces --no-headers 2>/dev/null | grep "$pattern" | awk '{print $1":"$2}' || echo "")
        
        if [[ -n "$dangling_pods" ]]; then
            while IFS= read -r pod_info; do
                if [[ -n "$pod_info" ]]; then
                    local namespace="${pod_info%%:*}"
                    local pod_name="${pod_info##*:}"
                    
                    if [[ "$DRY_RUN" == "true" ]]; then
                        log_info "Would delete dangling pod: $pod_name in namespace $namespace"
                    else
                        kubectl delete pod "$pod_name" -n "$namespace" --ignore-not-found=true 2>/dev/null
                        log_verbose "Deleted dangling pod: $pod_name in namespace $namespace"
                    fi
                fi
            done <<< "$dangling_pods"
        fi
    done
}

# Verify cleanup
verify_cleanup() {
    if [[ "$DRY_RUN" == "true" ]]; then
        return
    fi
    
    log_info "Verifying cleanup..."
    
    # Check for remaining test namespaces
    local remaining_namespaces
    remaining_namespaces=$(kubectl get namespaces -l istio-test=true --no-headers 2>/dev/null | wc -l)
    
    if [[ "$remaining_namespaces" -eq 0 ]]; then
        log_success "✓ No test namespaces remaining"
    else
        log_warn "⚠ $remaining_namespaces test namespaces still exist (may be terminating)"
    fi
    
    # Check for remaining test resources
    local test_labels=("test-type=protocol" "test-type=multi-tenant" "test-type=latency")
    local total_remaining=0
    
    for label in "${test_labels[@]}"; do
        local resources
        resources=$(kubectl get all --all-namespaces -l "$label" --no-headers 2>/dev/null | wc -l)
        total_remaining=$((total_remaining + resources))
    done
    
    if [[ "$total_remaining" -eq 0 ]]; then
        log_success "✓ No test resources remaining"
    else
        log_warn "⚠ $total_remaining test resources still exist"
    fi
    
    # Check reports directory
    if [[ ! -d "$TEST_DIR/reports" ]]; then
        log_success "✓ Test reports cleaned up"
    else
        log_warn "⚠ Reports directory still exists"
    fi
}

# Main function
main() {
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -c|--config)
                CONFIG_FILE="$2"
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
            -d|--dry-run)
                DRY_RUN=true
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
    
    log_info "Starting Istio migration test cleanup..."
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_warn "Running in dry-run mode - no resources will be deleted"
    fi
    
    # Parse configuration
    parse_config
    
    # Confirm cleanup
    confirm_cleanup
    
    # Execute cleanup steps
    cleanup_test_namespaces
    cleanup_test_resources
    cleanup_cluster_resources
    cleanup_dangling_pods
    cleanup_test_reports
    
    # Verify cleanup
    verify_cleanup
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Dry-run completed. Use --force to actually perform cleanup."
    else
        log_success "Istio migration test cleanup completed!"
    fi
}

# Execute main function
main "$@"