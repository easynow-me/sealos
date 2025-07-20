#!/bin/bash

# Multi-tenant Isolation Test
# Tests network isolation between different tenant namespaces using Istio

set -euo pipefail

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Test configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_DIR="$(dirname "$(dirname "$SCRIPT_DIR")")"
CONFIG_FILE="$TEST_DIR/config/test-config.yaml"

# Test variables
TEST_NAMESPACE_PREFIX="test-dev"
TOTAL_NAMESPACES=3
TOTAL_ASSERTIONS=0
PASSED_ASSERTIONS=0
FAILED_ASSERTIONS=0

# Test results
declare -a TEST_RESULTS=()

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1" >&2
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1" >&2
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

log_test() {
    echo -e "${YELLOW}[TEST]${NC} $1" >&2
}

# Test assertion helper
assert_equals() {
    local expected="$1"
    local actual="$2"
    local description="$3"
    
    ((TOTAL_ASSERTIONS++))
    
    if [[ "$expected" == "$actual" ]]; then
        ((PASSED_ASSERTIONS++))
        log_success "✓ $description"
        TEST_RESULTS+=("PASS: $description")
        return 0
    else
        ((FAILED_ASSERTIONS++))
        log_error "✗ $description (expected: $expected, actual: $actual)"
        TEST_RESULTS+=("FAIL: $description (expected: $expected, actual: $actual)")
        return 1
    fi
}

assert_not_equals() {
    local not_expected="$1"
    local actual="$2"
    local description="$3"
    
    ((TOTAL_ASSERTIONS++))
    
    if [[ "$not_expected" != "$actual" ]]; then
        ((PASSED_ASSERTIONS++))
        log_success "✓ $description"
        TEST_RESULTS+=("PASS: $description")
        return 0
    else
        ((FAILED_ASSERTIONS++))
        log_error "✗ $description (should not equal: $not_expected)"
        TEST_RESULTS+=("FAIL: $description (should not equal: $not_expected)")
        return 1
    fi
}

assert_contains() {
    local pattern="$1"
    local text="$2"
    local description="$3"
    
    ((TOTAL_ASSERTIONS++))
    
    if echo "$text" | grep -q "$pattern"; then
        ((PASSED_ASSERTIONS++))
        log_success "✓ $description"
        TEST_RESULTS+=("PASS: $description")
        return 0
    else
        ((FAILED_ASSERTIONS++))
        log_error "✗ $description (pattern '$pattern' not found in text)"
        TEST_RESULTS+=("FAIL: $description (pattern '$pattern' not found)")
        return 1
    fi
}

# Cleanup function
cleanup() {
    log_info "Cleaning up test resources..."
    
    # Remove test applications and networking resources
    for ((i=1; i<=TOTAL_NAMESPACES; i++)); do
        local ns="${TEST_NAMESPACE_PREFIX}-${i}"
        kubectl delete virtualservice,gateway,deployment,service -l test-type=multi-tenant -n "$ns" --ignore-not-found=true 2>/dev/null || true
    done
    
    log_info "Cleanup completed"
}

# Setup test environment
setup_test_env() {
    log_info "Setting up multi-tenant isolation test environment..."
    
    # Verify test namespaces exist
    for ((i=1; i<=TOTAL_NAMESPACES; i++)); do
        local ns="${TEST_NAMESPACE_PREFIX}-${i}"
        if ! kubectl get namespace "$ns" >/dev/null 2>&1; then
            log_error "Test namespace $ns not found. Run setup-test-env.sh first."
            exit 1
        fi
    done
    
    # Deploy test applications to each namespace
    for ((i=1; i<=TOTAL_NAMESPACES; i++)); do
        local ns="${TEST_NAMESPACE_PREFIX}-${i}"
        deploy_test_app "$ns" "$i"
    done
    
    # Wait for applications to be ready
    log_info "Waiting for test applications to be ready..."
    for ((i=1; i<=TOTAL_NAMESPACES; i++)); do
        local ns="${TEST_NAMESPACE_PREFIX}-${i}"
        kubectl wait --for=condition=available --timeout=60s deployment/test-app-$i -n "$ns"
    done
    
    log_success "Test environment setup completed"
}

# Deploy test application
deploy_test_app() {
    local namespace="$1"
    local app_id="$2"
    local app_name="test-app-$app_id"
    local service_name="test-service-$app_id"
    local gateway_name="test-gateway-$app_id"
    local vs_name="test-vs-$app_id"
    local host="app${app_id}.test.sealos.io"
    
    log_info "Deploying test app $app_id to namespace $namespace..."
    
    # Create deployment and service
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $app_name
  namespace: $namespace
  labels:
    app: $app_name
    test-type: multi-tenant
    tenant-id: "$app_id"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: $app_name
  template:
    metadata:
      labels:
        app: $app_name
        test-type: multi-tenant
        tenant-id: "$app_id"
    spec:
      containers:
      - name: app
        image: nginx:alpine
        ports:
        - containerPort: 80
        env:
        - name: TENANT_ID
          value: "$app_id"
        - name: NAMESPACE
          value: "$namespace"
        volumeMounts:
        - name: config
          mountPath: /usr/share/nginx/html/index.html
          subPath: index.html
      volumes:
      - name: config
        configMap:
          name: $app_name-config
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: $app_name-config
  namespace: $namespace
  labels:
    test-type: multi-tenant
data:
  index.html: |
    <!DOCTYPE html>
    <html>
    <head><title>Tenant $app_id</title></head>
    <body>
      <h1>Tenant $app_id Application</h1>
      <p>Namespace: $namespace</p>
      <p>App ID: $app_id</p>
      <p>Timestamp: $(date)</p>
    </body>
    </html>
---
apiVersion: v1
kind: Service
metadata:
  name: $service_name
  namespace: $namespace
  labels:
    app: $app_name
    test-type: multi-tenant
spec:
  selector:
    app: $app_name
  ports:
  - port: 80
    targetPort: 80
    name: http
---
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: $gateway_name
  namespace: $namespace
  labels:
    test-type: multi-tenant
    tenant-id: "$app_id"
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - $host
  - port:
      number: 443
      name: https
      protocol: HTTPS
    hosts:
    - $host
    tls:
      mode: SIMPLE
      credentialName: test-tls-cert
---
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: $vs_name
  namespace: $namespace
  labels:
    test-type: multi-tenant
    tenant-id: "$app_id"
spec:
  hosts:
  - $host
  gateways:
  - $gateway_name
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: $service_name
        port:
          number: 80
    headers:
      request:
        set:
          X-Tenant-ID: "$app_id"
          X-Namespace: "$namespace"
EOF
}

# Test namespace isolation
test_namespace_isolation() {
    log_test "Testing namespace isolation..."
    
    # Test 1: Verify each namespace has its own resources
    for ((i=1; i<=TOTAL_NAMESPACES; i++)); do
        local ns="${TEST_NAMESPACE_PREFIX}-${i}"
        
        # Check deployments
        local deployment_count
        deployment_count=$(kubectl get deployments -n "$ns" -l test-type=multi-tenant --no-headers | wc -l)
        assert_equals "1" "$deployment_count" "Namespace $ns should have exactly 1 test deployment"
        
        # Check services
        local service_count
        service_count=$(kubectl get services -n "$ns" -l test-type=multi-tenant --no-headers | wc -l)
        assert_equals "1" "$service_count" "Namespace $ns should have exactly 1 test service"
        
        # Check gateways
        local gateway_count
        gateway_count=$(kubectl get gateways -n "$ns" -l test-type=multi-tenant --no-headers | wc -l)
        assert_equals "1" "$gateway_count" "Namespace $ns should have exactly 1 test gateway"
        
        # Check virtual services
        local vs_count
        vs_count=$(kubectl get virtualservices -n "$ns" -l test-type=multi-tenant --no-headers | wc -l)
        assert_equals "1" "$vs_count" "Namespace $ns should have exactly 1 test virtual service"
    done
}

# Test cross-namespace access restrictions
test_cross_namespace_access() {
    log_test "Testing cross-namespace access restrictions..."
    
    # Test that services from one namespace cannot directly access services in another namespace
    for ((i=1; i<=TOTAL_NAMESPACES; i++)); do
        local source_ns="${TEST_NAMESPACE_PREFIX}-${i}"
        
        for ((j=1; j<=TOTAL_NAMESPACES; j++)); do
            if [[ $i -ne $j ]]; then
                local target_ns="${TEST_NAMESPACE_PREFIX}-${j}"
                local target_service="test-service-$j"
                
                # Try to access service from another namespace (should fail or be restricted)
                local result
                result=$(kubectl run test-client-$i-$j --rm -i --restart=Never --image=curlimages/curl \
                    --namespace="$source_ns" -- \
                    curl -s -o /dev/null -w "%{http_code}" \
                    "http://${target_service}.${target_ns}.svc.cluster.local" \
                    --connect-timeout 5 --max-time 10 2>/dev/null || echo "000")
                
                # Expect connection failure or access denied (not 200)
                assert_not_equals "200" "$result" "Cross-namespace access from $source_ns to $target_ns should be restricted"
            fi
        done
    done
}

# Test gateway isolation
test_gateway_isolation() {
    log_test "Testing gateway isolation..."
    
    # Each tenant should only be able to access their own gateway configuration
    for ((i=1; i<=TOTAL_NAMESPACES; i++)); do
        local ns="${TEST_NAMESPACE_PREFIX}-${i}"
        local gateway_name="test-gateway-$i"
        
        # Check gateway exists in the correct namespace
        local gateway_exists
        gateway_exists=$(kubectl get gateway "$gateway_name" -n "$ns" --no-headers 2>/dev/null | wc -l)
        assert_equals "1" "$gateway_exists" "Gateway $gateway_name should exist in namespace $ns"
        
        # Check that gateway is not visible in other namespaces
        for ((j=1; j<=TOTAL_NAMESPACES; j++)); do
            if [[ $i -ne $j ]]; then
                local other_ns="${TEST_NAMESPACE_PREFIX}-${j}"
                local gateway_in_other_ns
                gateway_in_other_ns=$(kubectl get gateway "$gateway_name" -n "$other_ns" --no-headers 2>/dev/null | wc -l)
                assert_equals "0" "$gateway_in_other_ns" "Gateway $gateway_name should not exist in namespace $other_ns"
            fi
        done
    done
}

# Test virtual service isolation
test_virtualservice_isolation() {
    log_test "Testing virtual service isolation..."
    
    for ((i=1; i<=TOTAL_NAMESPACES; i++)); do
        local ns="${TEST_NAMESPACE_PREFIX}-${i}"
        local vs_name="test-vs-$i"
        
        # Check virtual service configuration
        local vs_host
        vs_host=$(kubectl get virtualservice "$vs_name" -n "$ns" -o jsonpath='{.spec.hosts[0]}' 2>/dev/null)
        local expected_host="app${i}.test.sealos.io"
        assert_equals "$expected_host" "$vs_host" "VirtualService $vs_name should have correct host configuration"
        
        # Check that virtual service routes to correct service
        local vs_destination
        vs_destination=$(kubectl get virtualservice "$vs_name" -n "$ns" -o jsonpath='{.spec.http[0].route[0].destination.host}' 2>/dev/null)
        local expected_service="test-service-$i"
        assert_equals "$expected_service" "$vs_destination" "VirtualService $vs_name should route to correct service"
        
        # Check that virtual service has tenant-specific headers
        local tenant_header
        tenant_header=$(kubectl get virtualservice "$vs_name" -n "$ns" -o jsonpath='{.spec.http[0].headers.request.set.X-Tenant-ID}' 2>/dev/null)
        assert_equals "$i" "$tenant_header" "VirtualService $vs_name should set correct tenant ID header"
    done
}

# Test traffic isolation
test_traffic_isolation() {
    log_test "Testing traffic isolation through Istio ingress gateway..."
    
    # Get ingress gateway external IP/port
    local ingress_host
    local ingress_port
    
    if kubectl get service istio-ingressgateway -n istio-system >/dev/null 2>&1; then
        ingress_host=$(kubectl get service istio-ingressgateway -n istio-system -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "localhost")
        ingress_port=$(kubectl get service istio-ingressgateway -n istio-system -o jsonpath='{.spec.ports[?(@.name=="http2")].port}' 2>/dev/null || echo "80")
        
        if [[ "$ingress_host" == "" || "$ingress_host" == "localhost" ]]; then
            # Try nodeport or port-forward for local testing
            ingress_host="localhost"
            ingress_port="8080"
            log_info "Using localhost:8080 for ingress testing (consider port-forwarding istio-ingressgateway)"
        fi
        
        # Test traffic to each tenant's application
        for ((i=1; i<=TOTAL_NAMESPACES; i++)); do
            local host_header="app${i}.test.sealos.io"
            
            # Try to access the application through the ingress gateway
            local response
            response=$(kubectl run test-traffic-client-$i --rm -i --restart=Never --image=curlimages/curl \
                --namespace="${TEST_NAMESPACE_PREFIX}-1" -- \
                curl -s -H "Host: $host_header" \
                "http://$ingress_host:$ingress_port/" \
                --connect-timeout 5 --max-time 10 2>/dev/null || echo "Connection failed")
            
            # Check if response contains tenant-specific content
            if [[ "$response" != "Connection failed" ]]; then
                assert_contains "Tenant $i" "$response" "Traffic to $host_header should return tenant $i content"
                assert_contains "App ID: $i" "$response" "Traffic to $host_header should contain correct app ID"
            else
                log_error "Could not test traffic isolation - ingress gateway not accessible"
                ((FAILED_ASSERTIONS++))
                TEST_RESULTS+=("FAIL: Traffic isolation test - ingress gateway not accessible")
            fi
        done
    else
        log_error "Istio ingress gateway not found - skipping traffic isolation tests"
        ((FAILED_ASSERTIONS++))
        TEST_RESULTS+=("FAIL: Traffic isolation test - istio-ingressgateway service not found")
    fi
}

# Test label-based isolation
test_label_isolation() {
    log_test "Testing label-based resource isolation..."
    
    for ((i=1; i<=TOTAL_NAMESPACES; i++)); do
        local ns="${TEST_NAMESPACE_PREFIX}-${i}"
        local tenant_id="$i"
        
        # Check that all resources have correct tenant labels
        local resources=("deployment" "service" "gateway" "virtualservice")
        
        for resource in "${resources[@]}"; do
            local resource_with_label
            resource_with_label=$(kubectl get "$resource" -n "$ns" -l "test-type=multi-tenant,tenant-id=$tenant_id" --no-headers | wc -l)
            assert_equals "1" "$resource_with_label" "Namespace $ns should have exactly 1 $resource with correct tenant label"
        done
        
        # Check that resources don't have other tenant labels
        for ((j=1; j<=TOTAL_NAMESPACES; j++)); do
            if [[ $i -ne $j ]]; then
                local other_tenant_id="$j"
                local resources_with_wrong_label
                resources_with_wrong_label=$(kubectl get all -n "$ns" -l "tenant-id=$other_tenant_id" --no-headers | wc -l)
                assert_equals "0" "$resources_with_wrong_label" "Namespace $ns should not have resources with tenant-id=$other_tenant_id"
            fi
        done
    done
}

# Generate test report
generate_report() {
    local report_file="$TEST_DIR/reports/functional/multi-tenant-isolation.json"
    local timestamp=$(date -Iseconds)
    
    mkdir -p "$(dirname "$report_file")"
    
    # Create JSON report
    cat > "$report_file" << EOF
{
  "test_name": "multi-tenant-isolation",
  "timestamp": "$timestamp",
  "summary": {
    "total_assertions": $TOTAL_ASSERTIONS,
    "passed": $PASSED_ASSERTIONS,
    "failed": $FAILED_ASSERTIONS,
    "success_rate": $(echo "scale=2; $PASSED_ASSERTIONS * 100 / $TOTAL_ASSERTIONS" | bc -l 2>/dev/null || echo "0")
  },
  "results": [
$(printf '    "%s"' "${TEST_RESULTS[@]}" | sed 's/$/,/' | sed '$s/,$//')
  ]
}
EOF
    
    log_info "Test report generated: $report_file"
}

# Main test execution
main() {
    log_info "Starting multi-tenant isolation tests..."
    
    # Setup
    trap cleanup EXIT
    setup_test_env
    
    # Run tests
    test_namespace_isolation
    test_cross_namespace_access
    test_gateway_isolation
    test_virtualservice_isolation
    test_traffic_isolation
    test_label_isolation
    
    # Generate report
    generate_report
    
    # Summary
    log_info "Multi-tenant isolation tests completed"
    log_info "Results: $PASSED_ASSERTIONS passed, $FAILED_ASSERTIONS failed out of $TOTAL_ASSERTIONS total assertions"
    
    if [[ $FAILED_ASSERTIONS -eq 0 ]]; then
        log_success "All multi-tenant isolation tests passed!"
        exit 0
    else
        log_error "Some multi-tenant isolation tests failed!"
        exit 1
    fi
}

# Run main function
main "$@"