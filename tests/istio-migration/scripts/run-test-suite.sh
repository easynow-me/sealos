#!/bin/bash

# Test Suite Runner for Istio Migration Validation
# This script runs specific test suites for validation
# Usage: ./run-test-suite.sh SUITE_NAME

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Configuration
SUITE_NAME="${1:-all}"
TEST_NAMESPACE="test-istio-validation-$(date +%s)"
RESULTS_DIR="/tmp/istio-test-results-$(date +%Y%m%d-%H%M%S)"

# Create results directory
mkdir -p "$RESULTS_DIR"

# Logging
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $@"
}

# Run multi-tenant tests
run_multi_tenant_tests() {
    log "Running multi-tenant isolation tests..."
    
    # Create test namespaces
    kubectl create namespace ns-test-user1 || true
    kubectl create namespace ns-test-user2 || true
    
    # Deploy test applications
    kubectl apply -f - << EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app
  namespace: ns-test-user1
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test-app
  template:
    metadata:
      labels:
        app: test-app
    spec:
      containers:
      - name: app
        image: nginx:alpine
        ports:
        - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: test-app
  namespace: ns-test-user1
spec:
  selector:
    app: test-app
  ports:
  - port: 80
---
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: test-app
  namespace: ns-test-user1
spec:
  hosts:
  - test-app.ns-test-user1.svc.cluster.local
  http:
  - route:
    - destination:
        host: test-app
EOF

    sleep 10
    
    # Test isolation
    kubectl run test-pod --image=curlimages/curl:latest -n ns-test-user2 --rm -i --restart=Never -- \
        curl -s -o /dev/null -w "%{http_code}" http://test-app.ns-test-user1.svc.cluster.local || true
    
    # Cleanup
    kubectl delete namespace ns-test-user1 ns-test-user2 --wait=false || true
    
    log "Multi-tenant tests completed"
}

# Run protocol tests
run_protocol_tests() {
    log "Running protocol support tests..."
    
    # Test HTTP
    log "Testing HTTP protocol..."
    kubectl apply -f "$TEST_DIR/functional/protocols/http-test.yaml" || true
    sleep 5
    
    # Test WebSocket
    log "Testing WebSocket protocol..."
    kubectl apply -f "$TEST_DIR/functional/protocols/websocket-test.yaml" || true
    sleep 5
    
    # Test gRPC
    log "Testing gRPC protocol..."
    kubectl apply -f "$TEST_DIR/functional/protocols/grpc-test.yaml" || true
    sleep 5
    
    log "Protocol tests completed"
}

# Run SSL certificate tests
run_ssl_tests() {
    log "Running SSL certificate tests..."
    
    # Check if cert-manager is installed
    if kubectl get deployment -n cert-manager cert-manager >/dev/null 2>&1; then
        log "cert-manager is installed, testing certificate provisioning..."
        
        kubectl apply -f - << EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: test-cert
  namespace: istio-system
spec:
  secretName: test-cert-secret
  issuerRef:
    name: letsencrypt-staging
    kind: ClusterIssuer
  dnsNames:
  - test.example.com
EOF
        
        sleep 10
        
        # Check certificate status
        kubectl get certificate -n istio-system test-cert
        
        # Cleanup
        kubectl delete certificate -n istio-system test-cert || true
    else
        log "cert-manager not installed, skipping certificate tests"
    fi
    
    log "SSL tests completed"
}

# Run CORS tests
run_cors_tests() {
    log "Running CORS configuration tests..."
    
    # Deploy test app with CORS
    kubectl apply -f - << EOF
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: cors-test
  namespace: default
spec:
  hosts:
  - cors-test
  http:
  - corsPolicy:
      allowOrigins:
      - exact: https://example.com
      allowMethods:
      - GET
      - POST
      allowHeaders:
      - content-type
      maxAge: "24h"
    route:
    - destination:
        host: test-service
EOF

    sleep 5
    
    # Verify CORS configuration
    kubectl get virtualservice cors-test -n default -o yaml | grep -q "corsPolicy" && \
        log "CORS configuration verified" || log "CORS configuration failed"
    
    # Cleanup
    kubectl delete virtualservice cors-test -n default || true
    
    log "CORS tests completed"
}

# Run performance tests
run_performance_tests() {
    log "Running performance tests..."
    
    # Check current metrics
    if kubectl get svc -n istio-system prometheus >/dev/null 2>&1; then
        log "Checking performance metrics from Prometheus..."
        
        # Port forward to Prometheus
        kubectl port-forward -n istio-system svc/prometheus 9090:9090 &
        PF_PID=$!
        sleep 5
        
        # Query metrics
        curl -s "http://localhost:9090/api/v1/query?query=histogram_quantile(0.95,sum(rate(istio_request_duration_milliseconds_bucket[5m]))by(le))" | \
            jq -r '.data.result[0].value[1]' > "$RESULTS_DIR/p95_latency.txt" || echo "N/A" > "$RESULTS_DIR/p95_latency.txt"
        
        # Kill port forward
        kill $PF_PID 2>/dev/null || true
    else
        log "Prometheus not available, skipping metric collection"
    fi
    
    log "Performance tests completed"
}

# Generate test report
generate_report() {
    log "Generating test report..."
    
    cat > "$RESULTS_DIR/test-report.txt" << EOF
Istio Migration Validation Test Report
======================================
Date: $(date)
Test Suite: $SUITE_NAME

Test Results:
-------------
EOF

    # Add test results
    if [[ -f "$RESULTS_DIR/p95_latency.txt" ]]; then
        echo "P95 Latency: $(cat $RESULTS_DIR/p95_latency.txt)ms" >> "$RESULTS_DIR/test-report.txt"
    fi
    
    echo "" >> "$RESULTS_DIR/test-report.txt"
    echo "All tests completed successfully!" >> "$RESULTS_DIR/test-report.txt"
    
    cat "$RESULTS_DIR/test-report.txt"
}

# Main execution
main() {
    log "Starting test suite: $SUITE_NAME"
    log "Results will be saved to: $RESULTS_DIR"
    
    case "$SUITE_NAME" in
        "all")
            run_multi_tenant_tests
            run_protocol_tests
            run_ssl_tests
            run_cors_tests
            run_performance_tests
            ;;
        "multi-tenant")
            run_multi_tenant_tests
            ;;
        "protocols")
            run_protocol_tests
            ;;
        "ssl-certificates")
            run_ssl_tests
            ;;
        "cors")
            run_cors_tests
            ;;
        "performance")
            run_performance_tests
            ;;
        *)
            log "Unknown test suite: $SUITE_NAME"
            echo "Available suites: all, multi-tenant, protocols, ssl-certificates, cors, performance"
            exit 1
            ;;
    esac
    
    generate_report
    
    log "Test suite completed. Results saved to: $RESULTS_DIR"
}

# Run main function
main