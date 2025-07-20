#!/bin/bash

# Latency Performance Test
# Compares latency between Ingress and Istio Gateway/VirtualService

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
TEST_NAMESPACE="${TEST_NAMESPACE_PREFIX:-test-dev}-1"
TOTAL_ASSERTIONS=0
PASSED_ASSERTIONS=0
FAILED_ASSERTIONS=0

# Performance thresholds (from config)
TARGET_P95_LATENCY="100ms"
TEST_DURATION="60s"
CONCURRENT_USERS=(1 10 50 100)

# Test results
declare -a TEST_RESULTS=()
declare -A LATENCY_RESULTS=()

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

# Convert time units to milliseconds
time_to_ms() {
    local time_str="$1"
    if [[ "$time_str" =~ ^([0-9.]+)([a-z]+)$ ]]; then
        local value="${BASH_REMATCH[1]}"
        local unit="${BASH_REMATCH[2]}"
        
        case "$unit" in
            "ms") echo "$value" ;;
            "s") echo "$(echo "$value * 1000" | bc -l)" ;;
            "m") echo "$(echo "$value * 60000" | bc -l)" ;;
            *) echo "0" ;;
        esac
    else
        echo "0"
    fi
}

# Performance assertion helper
assert_latency_threshold() {
    local actual_p95="$1"
    local threshold="$2"
    local description="$3"
    
    ((TOTAL_ASSERTIONS++))
    
    local actual_ms=$(time_to_ms "$actual_p95")
    local threshold_ms=$(time_to_ms "$threshold")
    
    if (( $(echo "$actual_ms <= $threshold_ms" | bc -l) )); then
        ((PASSED_ASSERTIONS++))
        log_success "✓ $description (P95: ${actual_p95}, threshold: ${threshold})"
        TEST_RESULTS+=("PASS: $description (P95: ${actual_p95})")
        return 0
    else
        ((FAILED_ASSERTIONS++))
        log_error "✗ $description (P95: ${actual_p95} exceeds threshold: ${threshold})"
        TEST_RESULTS+=("FAIL: $description (P95: ${actual_p95} > ${threshold})")
        return 1
    fi
}

# Cleanup function
cleanup() {
    log_info "Cleaning up latency test resources..."
    
    kubectl delete deployment,service,gateway,virtualservice,ingress -l test-type=latency -n "$TEST_NAMESPACE" --ignore-not-found=true 2>/dev/null || true
    
    log_info "Cleanup completed"
}

# Setup test environment
setup_test_env() {
    log_info "Setting up latency test environment..."
    
    # Verify test namespace exists
    if ! kubectl get namespace "$TEST_NAMESPACE" >/dev/null 2>&1; then
        log_error "Test namespace $TEST_NAMESPACE not found. Run setup-test-env.sh first."
        exit 1
    fi
    
    # Parse configuration
    if [[ -f "$CONFIG_FILE" ]]; then
        TARGET_P95_LATENCY=$(yq e '.performance.latency.target_p95' "$CONFIG_FILE" 2>/dev/null || echo "100ms")
        TEST_DURATION=$(yq e '.performance.latency.duration' "$CONFIG_FILE" 2>/dev/null || echo "60s")
        
        # Parse concurrent users array
        local users_config
        users_config=$(yq e '.performance.latency.concurrent_users' "$CONFIG_FILE" 2>/dev/null || echo "[1,10,50,100]")
        if [[ "$users_config" != "null" ]]; then
            CONCURRENT_USERS=($(echo "$users_config" | jq -r '.[]' 2>/dev/null || echo "1 10 50 100"))
        fi
    fi
    
    # Deploy test applications for both Ingress and Istio
    deploy_test_apps
    
    # Wait for applications to be ready
    log_info "Waiting for latency test applications to be ready..."
    kubectl wait --for=condition=available --timeout=120s deployment -l test-type=latency -n "$TEST_NAMESPACE"
    
    log_success "Latency test environment setup completed"
}

# Deploy test applications
deploy_test_apps() {
    log_info "Deploying latency test applications..."
    
    # Deploy identical applications for Ingress and Istio comparison
    deploy_ingress_app
    deploy_istio_app
}

# Deploy application with Ingress
deploy_ingress_app() {
    log_info "Deploying Ingress-based test application..."
    
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: latency-ingress-app
  namespace: $TEST_NAMESPACE
  labels:
    app: latency-ingress-app
    test-type: latency
    networking: ingress
spec:
  replicas: 2
  selector:
    matchLabels:
      app: latency-ingress-app
  template:
    metadata:
      labels:
        app: latency-ingress-app
        test-type: latency
        networking: ingress
    spec:
      containers:
      - name: app
        image: nginx:alpine
        ports:
        - containerPort: 80
        resources:
          limits:
            cpu: 100m
            memory: 128Mi
          requests:
            cpu: 50m
            memory: 64Mi
        volumeMounts:
        - name: config
          mountPath: /usr/share/nginx/html
      volumes:
      - name: config
        configMap:
          name: latency-ingress-config
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: latency-ingress-config
  namespace: $TEST_NAMESPACE
  labels:
    test-type: latency
data:
  index.html: |
    <!DOCTYPE html>
    <html>
    <head><title>Latency Test - Ingress</title></head>
    <body>
        <h1>Latency Test Application - Ingress</h1>
        <p>Networking: Kubernetes Ingress</p>
        <p>Timestamp: $(date -Iseconds)</p>
        <p>Response Size: This is a test response for latency measurements. The content is standardized to ensure consistent response sizes across all tests.</p>
    </body>
    </html>
---
apiVersion: v1
kind: Service
metadata:
  name: latency-ingress-service
  namespace: $TEST_NAMESPACE
  labels:
    app: latency-ingress-app
    test-type: latency
spec:
  selector:
    app: latency-ingress-app
  ports:
  - port: 80
    targetPort: 80
    name: http
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: latency-ingress
  namespace: $TEST_NAMESPACE
  labels:
    test-type: latency
    networking: ingress
  annotations:
    kubernetes.io/ingress.class: "nginx"
    nginx.ingress.kubernetes.io/ssl-redirect: "false"
    nginx.ingress.kubernetes.io/backend-protocol: "HTTP"
spec:
  rules:
  - host: latency-ingress.test.sealos.io
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: latency-ingress-service
            port:
              number: 80
EOF
}

# Deploy application with Istio
deploy_istio_app() {
    log_info "Deploying Istio-based test application..."
    
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: latency-istio-app
  namespace: $TEST_NAMESPACE
  labels:
    app: latency-istio-app
    test-type: latency
    networking: istio
spec:
  replicas: 2
  selector:
    matchLabels:
      app: latency-istio-app
  template:
    metadata:
      labels:
        app: latency-istio-app
        test-type: latency
        networking: istio
    spec:
      containers:
      - name: app
        image: nginx:alpine
        ports:
        - containerPort: 80
        resources:
          limits:
            cpu: 100m
            memory: 128Mi
          requests:
            cpu: 50m
            memory: 64Mi
        volumeMounts:
        - name: config
          mountPath: /usr/share/nginx/html
      volumes:
      - name: config
        configMap:
          name: latency-istio-config
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: latency-istio-config
  namespace: $TEST_NAMESPACE
  labels:
    test-type: latency
data:
  index.html: |
    <!DOCTYPE html>
    <html>
    <head><title>Latency Test - Istio</title></head>
    <body>
        <h1>Latency Test Application - Istio</h1>
        <p>Networking: Istio Gateway/VirtualService</p>
        <p>Timestamp: $(date -Iseconds)</p>
        <p>Response Size: This is a test response for latency measurements. The content is standardized to ensure consistent response sizes across all tests.</p>
    </body>
    </html>
---
apiVersion: v1
kind: Service
metadata:
  name: latency-istio-service
  namespace: $TEST_NAMESPACE
  labels:
    app: latency-istio-app
    test-type: latency
spec:
  selector:
    app: latency-istio-app
  ports:
  - port: 80
    targetPort: 80
    name: http
---
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: latency-istio-gateway
  namespace: $TEST_NAMESPACE
  labels:
    test-type: latency
    networking: istio
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - latency-istio.test.sealos.io
---
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: latency-istio-vs
  namespace: $TEST_NAMESPACE
  labels:
    test-type: latency
    networking: istio
spec:
  hosts:
  - latency-istio.test.sealos.io
  gateways:
  - latency-istio-gateway
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: latency-istio-service
        port:
          number: 80
    timeout: 30s
EOF
}

# Run latency test
run_latency_test() {
    local networking_type="$1"  # "ingress" or "istio"
    local concurrent_users="$2"
    local target_host="$3"
    
    log_test "Running latency test: $networking_type with $concurrent_users concurrent users"
    
    # Use hey for load testing
    local test_result
    test_result=$(kubectl run "latency-test-${networking_type}-${concurrent_users}" --rm -i --restart=Never \
        --image=williamyeh/hey:latest \
        --namespace="$TEST_NAMESPACE" -- \
        hey -n 1000 -c "$concurrent_users" -t 30 \
        -H "Host: $target_host" \
        "http://istio-ingressgateway.istio-system.svc.cluster.local/" \
        2>/dev/null || echo "Load test failed")
    
    if [[ "$test_result" == "Load test failed" ]]; then
        log_error "Latency test failed for $networking_type with $concurrent_users users"
        LATENCY_RESULTS["${networking_type}_${concurrent_users}_p95"]="999ms"
        LATENCY_RESULTS["${networking_type}_${concurrent_users}_p50"]="999ms"
        LATENCY_RESULTS["${networking_type}_${concurrent_users}_avg"]="999ms"
        return 1
    fi
    
    # Parse results
    local p95_latency p50_latency avg_latency
    p95_latency=$(echo "$test_result" | grep "95%" | awk '{print $2}' | sed 's/secs/ms/' || echo "0ms")
    p50_latency=$(echo "$test_result" | grep "50%" | awk '{print $2}' | sed 's/secs/ms/' || echo "0ms")
    avg_latency=$(echo "$test_result" | grep "Average:" | awk '{print $2}' | sed 's/secs/ms/' || echo "0ms")
    
    # Convert seconds to milliseconds if needed
    if [[ "$p95_latency" == *"secs"* ]]; then
        p95_latency=$(echo "$p95_latency" | sed 's/secs//' | awk '{print $1 * 1000 "ms"}')
    fi
    if [[ "$p50_latency" == *"secs"* ]]; then
        p50_latency=$(echo "$p50_latency" | sed 's/secs//' | awk '{print $1 * 1000 "ms"}')
    fi
    if [[ "$avg_latency" == *"secs"* ]]; then
        avg_latency=$(echo "$avg_latency" | sed 's/secs//' | awk '{print $1 * 1000 "ms"}')
    fi
    
    # Store results
    LATENCY_RESULTS["${networking_type}_${concurrent_users}_p95"]="$p95_latency"
    LATENCY_RESULTS["${networking_type}_${concurrent_users}_p50"]="$p50_latency"
    LATENCY_RESULTS["${networking_type}_${concurrent_users}_avg"]="$avg_latency"
    
    log_info "Results for $networking_type ($concurrent_users users): P95=$p95_latency, P50=$p50_latency, Avg=$avg_latency"
    
    return 0
}

# Test latency comparison
test_latency_comparison() {
    log_test "Running latency comparison tests..."
    
    # Test both networking types with different concurrent user loads
    for users in "${CONCURRENT_USERS[@]}"; do
        log_info "Testing with $users concurrent users..."
        
        # Test Ingress
        run_latency_test "ingress" "$users" "latency-ingress.test.sealos.io"
        
        # Test Istio
        run_latency_test "istio" "$users" "latency-istio.test.sealos.io"
        
        # Compare results
        local ingress_p95="${LATENCY_RESULTS[ingress_${users}_p95]}"
        local istio_p95="${LATENCY_RESULTS[istio_${users}_p95]}"
        
        # Check if both meet the threshold
        assert_latency_threshold "$ingress_p95" "$TARGET_P95_LATENCY" "Ingress P95 latency with $users users should meet threshold"
        assert_latency_threshold "$istio_p95" "$TARGET_P95_LATENCY" "Istio P95 latency with $users users should meet threshold"
        
        # Calculate relative performance
        local ingress_ms=$(time_to_ms "$ingress_p95")
        local istio_ms=$(time_to_ms "$istio_p95")
        
        if [[ "$ingress_ms" != "0" && "$istio_ms" != "0" ]]; then
            local overhead_percent
            overhead_percent=$(echo "scale=1; ($istio_ms - $ingress_ms) * 100 / $ingress_ms" | bc -l)
            
            # Log performance comparison
            if (( $(echo "$overhead_percent <= 15" | bc -l) )); then
                log_success "✓ Istio overhead acceptable: ${overhead_percent}% (≤15%)"
                TEST_RESULTS+=("PASS: Istio overhead with $users users: ${overhead_percent}%")
            else
                log_error "✗ Istio overhead too high: ${overhead_percent}% (>15%)"
                TEST_RESULTS+=("FAIL: Istio overhead with $users users: ${overhead_percent}% (>15%)")
                ((FAILED_ASSERTIONS++))
            fi
            ((TOTAL_ASSERTIONS++))
        fi
    done
}

# Test latency consistency
test_latency_consistency() {
    log_test "Testing latency consistency across multiple runs..."
    
    # Run the same test multiple times to check consistency
    local consistency_runs=3
    local networking_type="istio"
    local users=10
    local target_host="latency-istio.test.sealos.io"
    
    declare -a p95_values=()
    
    for ((i=1; i<=consistency_runs; i++)); do
        log_info "Consistency run $i/$consistency_runs..."
        
        if run_latency_test "${networking_type}_consistency_$i" "$users" "$target_host"; then
            local p95="${LATENCY_RESULTS[${networking_type}_consistency_${i}_${users}_p95]}"
            p95_values+=("$(time_to_ms "$p95")")
        fi
    done
    
    # Calculate coefficient of variation
    if [[ ${#p95_values[@]} -eq $consistency_runs ]]; then
        local sum=0
        for value in "${p95_values[@]}"; do
            sum=$(echo "$sum + $value" | bc -l)
        done
        local mean=$(echo "scale=2; $sum / ${#p95_values[@]}" | bc -l)
        
        local variance=0
        for value in "${p95_values[@]}"; do
            local diff=$(echo "$value - $mean" | bc -l)
            variance=$(echo "$variance + ($diff * $diff)" | bc -l)
        done
        variance=$(echo "scale=2; $variance / ${#p95_values[@]}" | bc -l)
        
        local std_dev=$(echo "scale=2; sqrt($variance)" | bc -l)
        local cv=$(echo "scale=2; $std_dev * 100 / $mean" | bc -l)
        
        # Latency should be consistent (CV < 20%)
        ((TOTAL_ASSERTIONS++))
        if (( $(echo "$cv < 20" | bc -l) )); then
            ((PASSED_ASSERTIONS++))
            log_success "✓ Latency consistency good: CV=${cv}% (<20%)"
            TEST_RESULTS+=("PASS: Latency consistency CV=${cv}%")
        else
            ((FAILED_ASSERTIONS++))
            log_error "✗ Latency consistency poor: CV=${cv}% (≥20%)"
            TEST_RESULTS+=("FAIL: Latency consistency CV=${cv}% (≥20%)")
        fi
    fi
}

# Generate performance report
generate_report() {
    local report_file="$TEST_DIR/reports/performance/latency-results.json"
    local timestamp=$(date -Iseconds)
    
    mkdir -p "$(dirname "$report_file")"
    
    # Create detailed results object
    local results_json="{"
    for key in "${!LATENCY_RESULTS[@]}"; do
        results_json+='"'$key'":"'${LATENCY_RESULTS[$key]}'",'
    done
    results_json="${results_json%,}}"
    
    # Create JSON report
    cat > "$report_file" << EOF
{
  "test_name": "latency-performance",
  "timestamp": "$timestamp",
  "configuration": {
    "target_p95_threshold": "$TARGET_P95_LATENCY",
    "test_duration": "$TEST_DURATION",
    "concurrent_users": [$(printf '%s,' "${CONCURRENT_USERS[@]}" | sed 's/,$//')]
  },
  "summary": {
    "total_assertions": $TOTAL_ASSERTIONS,
    "passed": $PASSED_ASSERTIONS,
    "failed": $FAILED_ASSERTIONS,
    "success_rate": $(echo "scale=2; $PASSED_ASSERTIONS * 100 / $TOTAL_ASSERTIONS" | bc -l 2>/dev/null || echo "0")
  },
  "latency_results": $results_json,
  "test_results": [
$(printf '    "%s"' "${TEST_RESULTS[@]}" | sed 's/$/,/' | sed '$s/,$//')
  ]
}
EOF
    
    log_info "Latency test report generated: $report_file"
}

# Main test execution
main() {
    log_info "Starting latency performance tests..."
    
    # Setup
    trap cleanup EXIT
    setup_test_env
    
    # Run tests
    test_latency_comparison
    test_latency_consistency
    
    # Generate report
    generate_report
    
    # Summary
    log_info "Latency performance tests completed"
    log_info "Results: $PASSED_ASSERTIONS passed, $FAILED_ASSERTIONS failed out of $TOTAL_ASSERTIONS total assertions"
    
    if [[ $FAILED_ASSERTIONS -eq 0 ]]; then
        log_success "All latency performance tests passed!"
        exit 0
    else
        log_error "Some latency performance tests failed!"
        exit 1
    fi
}

# Run main function
main "$@"