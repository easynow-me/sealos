#!/bin/bash

# Istio Migration Utility Functions
# Common utility functions for Istio migration scripts

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Global variables
VERBOSE=${VERBOSE:-false}
LOG_FILE=${LOG_FILE:-"/tmp/sealos-istio-migration.log"}

# Logging functions
log_info() {
    local message="$1"
    echo -e "${BLUE}[INFO]${NC} $message" | tee -a "$LOG_FILE"
}

log_warn() {
    local message="$1"
    echo -e "${YELLOW}[WARN]${NC} $message" | tee -a "$LOG_FILE"
}

log_error() {
    local message="$1"
    echo -e "${RED}[ERROR]${NC} $message" | tee -a "$LOG_FILE"
}

log_success() {
    local message="$1"
    echo -e "${GREEN}[SUCCESS]${NC} $message" | tee -a "$LOG_FILE"
}

log_debug() {
    local message="$1"
    if [[ "$VERBOSE" == "true" ]]; then
        echo -e "${PURPLE}[DEBUG]${NC} $message" | tee -a "$LOG_FILE"
    fi
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Check prerequisites
check_prerequisites() {
    local missing_tools=()
    
    # Check required tools
    if ! command_exists kubectl; then
        missing_tools+=("kubectl")
    fi
    
    if ! command_exists jq; then
        missing_tools+=("jq")
    fi
    
    if [[ ${#missing_tools[@]} -gt 0 ]]; then
        log_error "Missing required tools: ${missing_tools[*]}"
        log_error "Please install the missing tools and try again."
        exit 1
    fi
    
    # Check cluster connectivity
    if ! kubectl cluster-info >/dev/null 2>&1; then
        log_error "Cannot connect to Kubernetes cluster"
        log_error "Please check your kubeconfig and try again."
        exit 1
    fi
    
    log_success "All prerequisites check passed"
}

# Confirm action with user
confirm_action() {
    local message="$1"
    local force_flag="${2:-false}"
    
    if [[ "$force_flag" == "true" ]]; then
        return 0
    fi
    
    echo -n "$message (yes/no): "
    read -r response
    if [[ "$response" != "yes" ]]; then
        log_info "Action cancelled by user"
        return 1
    fi
    return 0
}

# Check if namespace exists
namespace_exists() {
    local namespace="$1"
    kubectl get namespace "$namespace" >/dev/null 2>&1
}

# Create namespace if it doesn't exist
ensure_namespace() {
    local namespace="$1"
    
    if ! namespace_exists "$namespace"; then
        log_info "Creating namespace: $namespace"
        kubectl create namespace "$namespace"
        log_success "Namespace $namespace created"
    else
        log_debug "Namespace $namespace already exists"
    fi
}

# Get user namespaces (starting with ns-)
get_user_namespaces() {
    kubectl get namespaces -o json | \
        jq -r '.items[] | select(.metadata.name | startswith("ns-")) | .metadata.name'
}

# Initialize logging
init_logging() {
    local log_dir
    log_dir=$(dirname "$LOG_FILE")
    
    if [[ ! -d "$log_dir" ]]; then
        mkdir -p "$log_dir"
    fi
    
    log_debug "Logging initialized to: $LOG_FILE"
}

# Initialize utils
init_utils() {
    init_logging
    log_debug "Utils initialized"
}

# Auto-initialize when sourced
if [[ "${BASH_SOURCE[0]}" != "${0}" ]]; then
    init_utils
fi