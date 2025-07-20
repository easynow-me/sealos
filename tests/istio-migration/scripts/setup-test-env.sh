#!/bin/bash

# Istio Migration Test Environment Setup Script
# This script sets up the test environment for Istio migration testing

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
DRY_RUN=false
VERBOSE=false
CLEANUP_FIRST=false

# Help function
show_help() {
    cat << EOF
Istio Migration Test Environment Setup

Usage: $0 [OPTIONS]

OPTIONS:
    -c, --config FILE      Use custom config file (default: config/test-config.yaml)
    -d, --dry-run         Show what would be done without executing
    -v, --verbose         Verbose output
    --cleanup-first       Clean up existing test resources first
    -h, --help            Show this help message

EXAMPLES:
    $0                    # Setup with default configuration
    $0 --verbose          # Setup with verbose output
    $0 --cleanup-first    # Cleanup and then setup
    $0 --dry-run          # Show what would be done
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

# Check if a command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    local missing_tools=()
    
    # Check required tools
    for tool in kubectl helm yq jq curl; do
        if ! command_exists "$tool"; then
            missing_tools+=("$tool")
        fi
    done
    
    if [[ ${#missing_tools[@]} -gt 0 ]]; then
        log_error "Missing required tools: ${missing_tools[*]}"
        log_info "Please install the missing tools and try again."
        exit 1
    fi
    
    # Check kubectl connection
    if ! kubectl cluster-info >/dev/null 2>&1; then
        log_error "Cannot connect to Kubernetes cluster. Please check your kubeconfig."
        exit 1
    fi
    
    # Check if config file exists
    if [[ ! -f "$CONFIG_FILE" ]]; then
        log_error "Config file not found: $CONFIG_FILE"
        exit 1
    fi
    
    log_success "All prerequisites met"
}

# Parse configuration
parse_config() {
    log_info "Parsing configuration from $CONFIG_FILE..."
    
    # Extract key configuration values
    ISTIO_NAMESPACE=$(yq e '.istio.namespace' "$CONFIG_FILE")
    SEALOS_NAMESPACE=$(yq e '.sealos.namespace' "$CONFIG_FILE")
    TEST_NAMESPACE_PREFIX=$(yq e '.environments.dev.namespace_prefix' "$CONFIG_FILE")
    
    log_verbose "Istio namespace: $ISTIO_NAMESPACE"
    log_verbose "Sealos namespace: $SEALOS_NAMESPACE"
    log_verbose "Test namespace prefix: $TEST_NAMESPACE_PREFIX"
}

# Check Istio installation
check_istio() {
    log_info "Checking Istio installation..."
    
    if ! kubectl get namespace "$ISTIO_NAMESPACE" >/dev/null 2>&1; then
        log_warn "Istio namespace '$ISTIO_NAMESPACE' not found"
        log_info "Please install Istio first or run with --install-istio flag"
        return 1
    fi
    
    # Check if Istio pods are running
    local istio_pods
    istio_pods=$(kubectl get pods -n "$ISTIO_NAMESPACE" --no-headers 2>/dev/null | wc -l)
    
    if [[ "$istio_pods" -eq 0 ]]; then
        log_warn "No Istio pods found in namespace '$ISTIO_NAMESPACE'"
        return 1
    fi
    
    # Check istio-ingressgateway
    if ! kubectl get service istio-ingressgateway -n "$ISTIO_NAMESPACE" >/dev/null 2>&1; then
        log_warn "Istio ingress gateway not found"
        return 1
    fi
    
    log_success "Istio installation verified"
}

# Check Sealos installation  
check_sealos() {
    log_info "Checking Sealos installation..."
    
    if ! kubectl get namespace "$SEALOS_NAMESPACE" >/dev/null 2>&1; then
        log_warn "Sealos namespace '$SEALOS_NAMESPACE' not found"
        log_info "Please install Sealos first"
        return 1
    fi
    
    log_success "Sealos installation verified"
}

# Create test namespaces
create_test_namespaces() {
    log_info "Creating test namespaces..."
    
    local test_count
    test_count=$(yq e '.functional.multi_tenant.test_namespaces' "$CONFIG_FILE")
    
    for ((i=1; i<=test_count; i++)); do
        local ns_name="${TEST_NAMESPACE_PREFIX}-${i}"
        
        if [[ "$DRY_RUN" == "true" ]]; then
            log_info "Would create namespace: $ns_name"
            continue
        fi
        
        if ! kubectl get namespace "$ns_name" >/dev/null 2>&1; then
            log_verbose "Creating namespace: $ns_name"
            kubectl create namespace "$ns_name"
            
            # Add labels for testing
            kubectl label namespace "$ns_name" \
                istio-test=true \
                test-type=functional \
                test-category=multi-tenant \
                --overwrite
                
            # Enable Istio injection
            kubectl label namespace "$ns_name" istio-injection=enabled --overwrite
        else
            log_verbose "Namespace $ns_name already exists"
        fi
    done
    
    log_success "Test namespaces created"
}

# Deploy test applications
deploy_test_applications() {
    log_info "Deploying test applications..."
    
    local app_configs
    app_configs=$(yq e '.test_data.applications | keys | .[]' "$CONFIG_FILE")
    
    while IFS= read -r app_name; do
        log_verbose "Processing application: $app_name"
        
        local image port protocol
        image=$(yq e ".test_data.applications.$app_name.image" "$CONFIG_FILE")
        port=$(yq e ".test_data.applications.$app_name.port" "$CONFIG_FILE")
        protocol=$(yq e ".test_data.applications.$app_name.protocol" "$CONFIG_FILE")
        
        # Deploy to first test namespace
        local target_ns="${TEST_NAMESPACE_PREFIX}-1"
        
        if [[ "$DRY_RUN" == "true" ]]; then
            log_info "Would deploy $app_name ($image) to $target_ns"
            continue
        fi
        
        # Create deployment
        local deployment_name="test-$app_name"
        
        if ! kubectl get deployment "$deployment_name" -n "$target_ns" >/dev/null 2>&1; then
            cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $deployment_name
  namespace: $target_ns
  labels:
    app: $deployment_name
    test-type: functional
    protocol: $(echo "$protocol" | tr '[:upper:]' '[:lower:]')
spec:
  replicas: 1
  selector:
    matchLabels:
      app: $deployment_name
  template:
    metadata:
      labels:
        app: $deployment_name
        test-type: functional
        protocol: $(echo "$protocol" | tr '[:upper:]' '[:lower:]')
    spec:
      containers:
      - name: app
        image: $image
        ports:
        - containerPort: $port
          name: service-port
        resources:
          limits:
            cpu: 100m
            memory: 128Mi
          requests:
            cpu: 50m
            memory: 64Mi
---
apiVersion: v1
kind: Service
metadata:
  name: $deployment_name
  namespace: $target_ns
  labels:
    app: $deployment_name
    test-type: functional
spec:
  selector:
    app: $deployment_name
  ports:
  - port: $port
    targetPort: $port
    name: service-port
EOF
            log_verbose "Deployed $app_name to $target_ns"
        else
            log_verbose "Application $app_name already exists in $target_ns"
        fi
    done <<< "$app_configs"
    
    log_success "Test applications deployed"
}

# Create test certificates
create_test_certificates() {
    log_info "Creating test certificates..."
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Would create test certificates"
        return
    fi
    
    # Create a simple self-signed certificate for testing
    local cert_ns="${TEST_NAMESPACE_PREFIX}-1"
    local cert_name="test-tls-cert"
    
    if ! kubectl get secret "$cert_name" -n "$cert_ns" >/dev/null 2>&1; then
        # Generate self-signed certificate
        local temp_dir
        temp_dir=$(mktemp -d)
        
        openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
            -keyout "$temp_dir/tls.key" \
            -out "$temp_dir/tls.crt" \
            -subj "/CN=test.sealos.io"
            
        kubectl create secret tls "$cert_name" \
            --cert="$temp_dir/tls.crt" \
            --key="$temp_dir/tls.key" \
            -n "$cert_ns"
            
        rm -rf "$temp_dir"
        log_verbose "Created test certificate: $cert_name"
    else
        log_verbose "Test certificate already exists: $cert_name"
    fi
    
    log_success "Test certificates created"
}

# Install test tools
install_test_tools() {
    log_info "Installing test tools..."
    
    # Check if we need to install additional tools
    local tools_needed=()
    
    # Check for load testing tools
    if ! command_exists "hey"; then
        tools_needed+=("hey")
    fi
    
    if ! command_exists "grpcurl"; then
        tools_needed+=("grpcurl")
    fi
    
    if [[ ${#tools_needed[@]} -gt 0 ]]; then
        log_warn "Some test tools are missing: ${tools_needed[*]}"
        log_info "Please install these tools manually for full test coverage:"
        log_info "  hey: go install github.com/rakyll/hey@latest"
        log_info "  grpcurl: go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest"
    else
        log_success "All test tools available"
    fi
}

# Cleanup existing resources
cleanup_existing() {
    if [[ "$CLEANUP_FIRST" != "true" ]]; then
        return
    fi
    
    log_info "Cleaning up existing test resources..."
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Would clean up existing test resources"
        return
    fi
    
    # Remove test namespaces
    kubectl get namespaces -l istio-test=true -o name | xargs -r kubectl delete
    
    log_success "Cleanup completed"
}

# Verify setup
verify_setup() {
    log_info "Verifying test environment setup..."
    
    local errors=0
    
    # Check test namespaces
    local expected_ns
    expected_ns=$(yq e '.functional.multi_tenant.test_namespaces' "$CONFIG_FILE")
    local actual_ns
    actual_ns=$(kubectl get namespaces -l istio-test=true --no-headers | wc -l)
    
    if [[ "$actual_ns" -ne "$expected_ns" ]]; then
        log_error "Expected $expected_ns test namespaces, found $actual_ns"
        ((errors++))
    fi
    
    # Check test applications
    local app_count
    app_count=$(kubectl get deployments -n "${TEST_NAMESPACE_PREFIX}-1" -l test-type=functional --no-headers | wc -l)
    local expected_apps
    expected_apps=$(yq e '.test_data.applications | length' "$CONFIG_FILE")
    
    if [[ "$app_count" -ne "$expected_apps" ]]; then
        log_error "Expected $expected_apps test applications, found $app_count"
        ((errors++))
    fi
    
    if [[ "$errors" -eq 0 ]]; then
        log_success "Test environment verification passed"
    else
        log_error "Test environment verification failed with $errors errors"
        return 1
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
            -d|--dry-run)
                DRY_RUN=true
                shift
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            --cleanup-first)
                CLEANUP_FIRST=true
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
    
    log_info "Starting Istio migration test environment setup..."
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_warn "Running in dry-run mode - no changes will be made"
    fi
    
    # Execute setup steps
    check_prerequisites
    parse_config
    cleanup_existing
    check_istio
    check_sealos
    create_test_namespaces
    deploy_test_applications
    create_test_certificates
    install_test_tools
    
    if [[ "$DRY_RUN" != "true" ]]; then
        verify_setup
    fi
    
    log_success "Test environment setup completed!"
    log_info "You can now run the integration tests with: ./scripts/run-all-tests.sh"
}

# Execute main function
main "$@"