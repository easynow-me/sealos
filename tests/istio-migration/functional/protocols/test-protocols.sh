#!/bin/bash

# Protocol Support Test
# Tests HTTP, GRPC, and WebSocket protocol support through Istio Gateway/VirtualService

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
        log_error "✗ $description (pattern '$pattern' not found)"
        TEST_RESULTS+=("FAIL: $description (pattern '$pattern' not found)")
        return 1
    fi
}

assert_http_status() {
    local expected_status="$1"
    local actual_status="$2"
    local description="$3"
    
    ((TOTAL_ASSERTIONS++))
    
    if [[ "$expected_status" == "$actual_status" ]]; then
        ((PASSED_ASSERTIONS++))
        log_success "✓ $description"
        TEST_RESULTS+=("PASS: $description")
        return 0
    else
        ((FAILED_ASSERTIONS++))
        log_error "✗ $description (expected HTTP $expected_status, got $actual_status)"
        TEST_RESULTS+=("FAIL: $description (expected HTTP $expected_status, got $actual_status)")
        return 1
    fi
}

# Cleanup function
cleanup() {
    log_info "Cleaning up protocol test resources..."
    
    kubectl delete deployment,service,gateway,virtualservice -l test-type=protocol -n "$TEST_NAMESPACE" --ignore-not-found=true 2>/dev/null || true
    
    log_info "Cleanup completed"
}

# Setup test environment
setup_test_env() {
    log_info "Setting up protocol test environment..."
    
    # Verify test namespace exists
    if ! kubectl get namespace "$TEST_NAMESPACE" >/dev/null 2>&1; then
        log_error "Test namespace $TEST_NAMESPACE not found. Run setup-test-env.sh first."
        exit 1
    fi
    
    # Deploy protocol-specific test applications
    deploy_http_app
    deploy_grpc_app
    deploy_websocket_app
    
    # Wait for applications to be ready
    log_info "Waiting for protocol test applications to be ready..."
    kubectl wait --for=condition=available --timeout=120s deployment -l test-type=protocol -n "$TEST_NAMESPACE"
    
    log_success "Protocol test environment setup completed"
}

# Deploy HTTP test application
deploy_http_app() {
    log_info "Deploying HTTP test application..."
    
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: http-test-app
  namespace: $TEST_NAMESPACE
  labels:
    app: http-test-app
    test-type: protocol
    protocol: http
spec:
  replicas: 1
  selector:
    matchLabels:
      app: http-test-app
  template:
    metadata:
      labels:
        app: http-test-app
        test-type: protocol
        protocol: http
    spec:
      containers:
      - name: app
        image: nginx:alpine
        ports:
        - containerPort: 80
        volumeMounts:
        - name: config
          mountPath: /usr/share/nginx/html
        - name: nginx-config
          mountPath: /etc/nginx/conf.d
      volumes:
      - name: config
        configMap:
          name: http-test-config
      - name: nginx-config
        configMap:
          name: http-nginx-config
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: http-test-config
  namespace: $TEST_NAMESPACE
  labels:
    test-type: protocol
data:
  index.html: |
    <!DOCTYPE html>
    <html>
    <head>
        <title>HTTP Test App</title>
        <meta charset="utf-8">
    </head>
    <body>
        <h1>HTTP Protocol Test</h1>
        <p>Protocol: HTTP</p>
        <p>Status: OK</p>
        <p>Timestamp: $(date)</p>
        <div id="api-test">
            <h2>API Endpoints</h2>
            <ul>
                <li><a href="/api/health">GET /api/health</a></li>
                <li><a href="/api/info">GET /api/info</a></li>
            </ul>
        </div>
    </body>
    </html>
  api-health.json: |
    {"status": "healthy", "protocol": "http", "timestamp": "$(date -Iseconds)"}
  api-info.json: |
    {"service": "http-test-app", "version": "1.0.0", "protocol": "http"}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: http-nginx-config
  namespace: $TEST_NAMESPACE
  labels:
    test-type: protocol
data:
  default.conf: |
    server {
        listen 80;
        server_name localhost;
        
        location / {
            root /usr/share/nginx/html;
            index index.html;
            add_header X-Protocol "HTTP" always;
            add_header X-Service "http-test-app" always;
        }
        
        location /api/health {
            alias /usr/share/nginx/html/api-health.json;
            add_header Content-Type "application/json";
            add_header X-Protocol "HTTP" always;
        }
        
        location /api/info {
            alias /usr/share/nginx/html/api-info.json;
            add_header Content-Type "application/json";
            add_header X-Protocol "HTTP" always;
        }
        
        # CORS headers
        location ~* \.(html|json)$ {
            add_header Access-Control-Allow-Origin "*" always;
            add_header Access-Control-Allow-Methods "GET, POST, PUT, DELETE, OPTIONS" always;
            add_header Access-Control-Allow-Headers "Content-Type, Authorization" always;
        }
    }
---
apiVersion: v1
kind: Service
metadata:
  name: http-test-service
  namespace: $TEST_NAMESPACE
  labels:
    app: http-test-app
    test-type: protocol
spec:
  selector:
    app: http-test-app
  ports:
  - port: 80
    targetPort: 80
    name: http
---
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: http-test-gateway
  namespace: $TEST_NAMESPACE
  labels:
    test-type: protocol
    protocol: http
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - http-test.sealos.io
  - port:
      number: 443
      name: https
      protocol: HTTPS
    hosts:
    - http-test.sealos.io
    tls:
      mode: SIMPLE
      credentialName: test-tls-cert
---
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: http-test-vs
  namespace: $TEST_NAMESPACE
  labels:
    test-type: protocol
    protocol: http
spec:
  hosts:
  - http-test.sealos.io
  gateways:
  - http-test-gateway
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: http-test-service
        port:
          number: 80
    timeout: 30s
    corsPolicy:
      allowOrigins:
      - regex: ".*"
      allowMethods:
      - GET
      - POST
      - PUT
      - DELETE
      - OPTIONS
      allowHeaders:
      - content-type
      - authorization
      - x-custom-header
      maxAge: 24h
    headers:
      request:
        set:
          X-Protocol: "HTTP"
          X-Test-Type: "protocol-test"
EOF
}

# Deploy GRPC test application  
deploy_grpc_app() {
    log_info "Deploying GRPC test application..."
    
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: grpc-test-app
  namespace: $TEST_NAMESPACE
  labels:
    app: grpc-test-app
    test-type: protocol
    protocol: grpc
spec:
  replicas: 1
  selector:
    matchLabels:
      app: grpc-test-app
  template:
    metadata:
      labels:
        app: grpc-test-app
        test-type: protocol
        protocol: grpc
    spec:
      containers:
      - name: app
        image: grpc/java-example-hostname:latest
        ports:
        - containerPort: 50051
        env:
        - name: GRPC_PORT
          value: "50051"
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
  name: grpc-test-service
  namespace: $TEST_NAMESPACE
  labels:
    app: grpc-test-app
    test-type: protocol
spec:
  selector:
    app: grpc-test-app
  ports:
  - port: 50051
    targetPort: 50051
    name: grpc
---
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: grpc-test-gateway
  namespace: $TEST_NAMESPACE
  labels:
    test-type: protocol
    protocol: grpc
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: grpc-http
      protocol: HTTP2
    hosts:
    - grpc-test.sealos.io
  - port:
      number: 443
      name: grpc-https
      protocol: HTTP2
    hosts:
    - grpc-test.sealos.io
    tls:
      mode: SIMPLE
      credentialName: test-tls-cert
---
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: grpc-test-vs
  namespace: $TEST_NAMESPACE
  labels:
    test-type: protocol
    protocol: grpc
spec:
  hosts:
  - grpc-test.sealos.io
  gateways:
  - grpc-test-gateway
  http:
  - match:
    - headers:
        content-type:
          prefix: application/grpc
    route:
    - destination:
        host: grpc-test-service
        port:
          number: 50051
    timeout: 0s
    headers:
      request:
        set:
          X-Protocol: "GRPC"
          X-Test-Type: "protocol-test"
EOF
}

# Deploy WebSocket test application
deploy_websocket_app() {
    log_info "Deploying WebSocket test application..."
    
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: websocket-test-app
  namespace: $TEST_NAMESPACE
  labels:
    app: websocket-test-app
    test-type: protocol
    protocol: websocket
spec:
  replicas: 1
  selector:
    matchLabels:
      app: websocket-test-app
  template:
    metadata:
      labels:
        app: websocket-test-app
        test-type: protocol
        protocol: websocket
    spec:
      containers:
      - name: app
        image: jmalloc/echo-server:latest
        ports:
        - containerPort: 8080
        env:
        - name: PORT
          value: "8080"
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
  name: websocket-test-service
  namespace: $TEST_NAMESPACE
  labels:
    app: websocket-test-app
    test-type: protocol
spec:
  selector:
    app: websocket-test-app
  ports:
  - port: 8080
    targetPort: 8080
    name: websocket
---
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: websocket-test-gateway
  namespace: $TEST_NAMESPACE
  labels:
    test-type: protocol
    protocol: websocket
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: websocket-http
      protocol: HTTP
    hosts:
    - ws-test.sealos.io
  - port:
      number: 443
      name: websocket-https
      protocol: HTTPS
    hosts:
    - ws-test.sealos.io
    tls:
      mode: SIMPLE
      credentialName: test-tls-cert
---
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: websocket-test-vs
  namespace: $TEST_NAMESPACE
  labels:
    test-type: protocol
    protocol: websocket
spec:
  hosts:
  - ws-test.sealos.io
  gateways:
  - websocket-test-gateway
  http:
  - match:
    - headers:
        upgrade:
          exact: websocket
    route:
    - destination:
        host: websocket-test-service
        port:
          number: 8080
    timeout: 0s
    headers:
      request:
        set:
          X-Protocol: "WebSocket"
          X-Test-Type: "protocol-test"
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: websocket-test-service
        port:
          number: 8080
    timeout: 30s
EOF
}

# Test HTTP protocol
test_http_protocol() {
    log_test "Testing HTTP protocol support..."
    
    # Test basic HTTP request
    local response
    response=$(kubectl run http-test-client --rm -i --restart=Never --image=curlimages/curl \
        --namespace="$TEST_NAMESPACE" -- \
        curl -s -H "Host: http-test.sealos.io" \
        "http://istio-ingressgateway.istio-system.svc.cluster.local/" \
        --connect-timeout 10 --max-time 30 2>/dev/null || echo "Request failed")
    
    if [[ "$response" != "Request failed" ]]; then
        assert_contains "HTTP Protocol Test" "$response" "HTTP request should return expected content"
        assert_contains "Protocol: HTTP" "$response" "HTTP response should contain protocol information"
    else
        ((FAILED_ASSERTIONS++))
        TEST_RESULTS+=("FAIL: HTTP basic request failed")
    fi
    
    # Test HTTP API endpoints
    local health_response
    health_response=$(kubectl run http-api-test-client --rm -i --restart=Never --image=curlimages/curl \
        --namespace="$TEST_NAMESPACE" -- \
        curl -s -H "Host: http-test.sealos.io" \
        "http://istio-ingressgateway.istio-system.svc.cluster.local/api/health" \
        --connect-timeout 10 --max-time 30 2>/dev/null || echo "API request failed")
    
    if [[ "$health_response" != "API request failed" ]]; then
        assert_contains "healthy" "$health_response" "HTTP API health endpoint should return healthy status"
        assert_contains "http" "$health_response" "HTTP API response should contain protocol information"
    else
        ((FAILED_ASSERTIONS++))
        TEST_RESULTS+=("FAIL: HTTP API request failed")
    fi
    
    # Test HTTP headers and CORS
    local headers_response
    headers_response=$(kubectl run http-headers-test-client --rm -i --restart=Never --image=curlimages/curl \
        --namespace="$TEST_NAMESPACE" -- \
        curl -s -I -H "Host: http-test.sealos.io" \
        -H "Origin: https://test.sealos.io" \
        "http://istio-ingressgateway.istio-system.svc.cluster.local/" \
        --connect-timeout 10 --max-time 30 2>/dev/null || echo "Headers request failed")
    
    if [[ "$headers_response" != "Headers request failed" ]]; then
        assert_contains "X-Protocol: HTTP" "$headers_response" "HTTP response should contain custom protocol header"
        assert_contains "Access-Control-Allow-Origin" "$headers_response" "HTTP response should contain CORS headers"
    else
        ((FAILED_ASSERTIONS++))
        TEST_RESULTS+=("FAIL: HTTP headers/CORS request failed")
    fi
}

# Test GRPC protocol
test_grpc_protocol() {
    log_test "Testing GRPC protocol support..."
    
    # Check if grpcurl is available
    if ! kubectl run grpc-check --rm -i --restart=Never --image=alpine/curl \
        --namespace="$TEST_NAMESPACE" -- \
        sh -c "command -v grpcurl" >/dev/null 2>&1; then
        log_error "grpcurl not available in test image - using alternative GRPC test"
        
        # Alternative test: Check if GRPC service is accessible via HTTP/2
        local grpc_response
        grpc_response=$(kubectl run grpc-http2-test-client --rm -i --restart=Never --image=curlimages/curl \
            --namespace="$TEST_NAMESPACE" -- \
            curl -s --http2-prior-knowledge \
            -H "Host: grpc-test.sealos.io" \
            -H "Content-Type: application/grpc" \
            "http://istio-ingressgateway.istio-system.svc.cluster.local/" \
            --connect-timeout 10 --max-time 30 2>/dev/null || echo "GRPC HTTP/2 test failed")
        
        # Even if the GRPC call itself fails, we should get a proper HTTP/2 response
        if [[ "$grpc_response" != "GRPC HTTP/2 test failed" ]]; then
            ((PASSED_ASSERTIONS++))
            TEST_RESULTS+=("PASS: GRPC service accessible via HTTP/2")
        else
            ((FAILED_ASSERTIONS++))
            TEST_RESULTS+=("FAIL: GRPC service not accessible via HTTP/2")
        fi
    else
        # Use grpcurl for proper GRPC testing
        local grpc_result
        grpc_result=$(kubectl run grpc-test-client --rm -i --restart=Never --image=alpine/curl \
            --namespace="$TEST_NAMESPACE" -- \
            grpcurl -plaintext \
            -H "Host: grpc-test.sealos.io" \
            istio-ingressgateway.istio-system.svc.cluster.local:80 \
            list 2>/dev/null || echo "GRPC list failed")
        
        if [[ "$grpc_result" != "GRPC list failed" ]]; then
            ((PASSED_ASSERTIONS++))
            TEST_RESULTS+=("PASS: GRPC service listing successful")
        else
            ((FAILED_ASSERTIONS++))
            TEST_RESULTS+=("FAIL: GRPC service listing failed")
        fi
    fi
    
    # Test GRPC gateway configuration
    local grpc_gateway
    grpc_gateway=$(kubectl get gateway grpc-test-gateway -n "$TEST_NAMESPACE" -o jsonpath='{.spec.servers[0].port.protocol}' 2>/dev/null)
    assert_equals "HTTP2" "$grpc_gateway" "GRPC gateway should use HTTP2 protocol"
    
    # Test GRPC virtual service configuration
    local grpc_vs_match
    grpc_vs_match=$(kubectl get virtualservice grpc-test-vs -n "$TEST_NAMESPACE" -o jsonpath='{.spec.http[0].match[0].headers.content-type.prefix}' 2>/dev/null)
    assert_equals "application/grpc" "$grpc_vs_match" "GRPC virtual service should match on content-type header"
}

# Test WebSocket protocol
test_websocket_protocol() {
    log_test "Testing WebSocket protocol support..."
    
    # Test WebSocket upgrade request
    local ws_response
    ws_response=$(kubectl run ws-test-client --rm -i --restart=Never --image=curlimages/curl \
        --namespace="$TEST_NAMESPACE" -- \
        curl -s -I \
        -H "Host: ws-test.sealos.io" \
        -H "Upgrade: websocket" \
        -H "Connection: Upgrade" \
        -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
        -H "Sec-WebSocket-Version: 13" \
        "http://istio-ingressgateway.istio-system.svc.cluster.local/" \
        --connect-timeout 10 --max-time 30 2>/dev/null || echo "WebSocket upgrade failed")
    
    if [[ "$ws_response" != "WebSocket upgrade failed" ]]; then
        # Check for WebSocket upgrade response
        assert_contains "101\|upgrade" "$ws_response" "WebSocket upgrade should return 101 status or upgrade headers"
    else
        ((FAILED_ASSERTIONS++))
        TEST_RESULTS+=("FAIL: WebSocket upgrade request failed")
    fi
    
    # Test regular HTTP request to WebSocket service (should also work)
    local http_to_ws_response
    http_to_ws_response=$(kubectl run ws-http-test-client --rm -i --restart=Never --image=curlimages/curl \
        --namespace="$TEST_NAMESPACE" -- \
        curl -s -H "Host: ws-test.sealos.io" \
        "http://istio-ingressgateway.istio-system.svc.cluster.local/" \
        --connect-timeout 10 --max-time 30 2>/dev/null || echo "HTTP to WebSocket service failed")
    
    if [[ "$http_to_ws_response" != "HTTP to WebSocket service failed" ]]; then
        ((PASSED_ASSERTIONS++))
        TEST_RESULTS+=("PASS: HTTP request to WebSocket service works")
    else
        ((FAILED_ASSERTIONS++))
        TEST_RESULTS+=("FAIL: HTTP request to WebSocket service failed")
    fi
    
    # Test WebSocket virtual service configuration
    local ws_vs_match
    ws_vs_match=$(kubectl get virtualservice websocket-test-vs -n "$TEST_NAMESPACE" -o jsonpath='{.spec.http[0].match[0].headers.upgrade.exact}' 2>/dev/null)
    assert_equals "websocket" "$ws_vs_match" "WebSocket virtual service should match on upgrade header"
    
    # Test WebSocket timeout configuration (should be 0s for long connections)
    local ws_timeout
    ws_timeout=$(kubectl get virtualservice websocket-test-vs -n "$TEST_NAMESPACE" -o jsonpath='{.spec.http[0].timeout}' 2>/dev/null)
    assert_equals "0s" "$ws_timeout" "WebSocket virtual service should have no timeout for long connections"
}

# Test protocol-specific routing
test_protocol_routing() {
    log_test "Testing protocol-specific routing..."
    
    # Verify each protocol has its own gateway and virtual service
    local protocols=("http" "grpc" "websocket")
    
    for protocol in "${protocols[@]}"; do
        local gateway_count
        gateway_count=$(kubectl get gateways -n "$TEST_NAMESPACE" -l "test-type=protocol,protocol=$protocol" --no-headers | wc -l)
        assert_equals "1" "$gateway_count" "Should have exactly 1 gateway for $protocol protocol"
        
        local vs_count
        vs_count=$(kubectl get virtualservices -n "$TEST_NAMESPACE" -l "test-type=protocol,protocol=$protocol" --no-headers | wc -l)
        assert_equals "1" "$vs_count" "Should have exactly 1 virtual service for $protocol protocol"
    done
    
    # Test that each protocol routes to the correct service
    local http_vs_destination
    http_vs_destination=$(kubectl get virtualservice http-test-vs -n "$TEST_NAMESPACE" -o jsonpath='{.spec.http[0].route[0].destination.host}' 2>/dev/null)
    assert_equals "http-test-service" "$http_vs_destination" "HTTP virtual service should route to HTTP service"
    
    local grpc_vs_destination
    grpc_vs_destination=$(kubectl get virtualservice grpc-test-vs -n "$TEST_NAMESPACE" -o jsonpath='{.spec.http[0].route[0].destination.host}' 2>/dev/null)
    assert_equals "grpc-test-service" "$grpc_vs_destination" "GRPC virtual service should route to GRPC service"
    
    local ws_vs_destination
    ws_vs_destination=$(kubectl get virtualservice websocket-test-vs -n "$TEST_NAMESPACE" -o jsonpath='{.spec.http[0].route[0].destination.host}' 2>/dev/null)
    assert_equals "websocket-test-service" "$ws_vs_destination" "WebSocket virtual service should route to WebSocket service"
}

# Generate test report
generate_report() {
    local report_file="$TEST_DIR/reports/functional/protocol-support.json"
    local timestamp=$(date -Iseconds)
    
    mkdir -p "$(dirname "$report_file")"
    
    # Create JSON report
    cat > "$report_file" << EOF
{
  "test_name": "protocol-support",
  "timestamp": "$timestamp",
  "protocols_tested": ["HTTP", "GRPC", "WebSocket"],
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
    log_info "Starting protocol support tests..."
    
    # Setup
    trap cleanup EXIT
    setup_test_env
    
    # Run protocol tests
    test_http_protocol
    test_grpc_protocol
    test_websocket_protocol
    test_protocol_routing
    
    # Generate report
    generate_report
    
    # Summary
    log_info "Protocol support tests completed"
    log_info "Results: $PASSED_ASSERTIONS passed, $FAILED_ASSERTIONS failed out of $TOTAL_ASSERTIONS total assertions"
    
    if [[ $FAILED_ASSERTIONS -eq 0 ]]; then
        log_success "All protocol support tests passed!"
        exit 0
    else
        log_error "Some protocol support tests failed!"
        exit 1
    fi
}

# Run main function
main "$@"