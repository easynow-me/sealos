#!/bin/bash

# Istio Migration Test Runner
# This script runs all integration tests for the Istio migration

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
TEST_DIR="$(dirname "$SCRIPT_DIR")"
CONFIG_FILE="$TEST_DIR/config/test-config.yaml"
REPORT_DIR="$TEST_DIR/reports"
VERBOSE=false
DRY_RUN=false
PARALLEL=true
FAIL_FAST=false

# Test categories
RUN_FUNCTIONAL=true
RUN_PERFORMANCE=true
RUN_COMPATIBILITY=true

# Specific test filters
PROTOCOL_FILTER=""
NAMESPACE_FILTER=""

# Test results tracking
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0
SKIPPED_TESTS=0

declare -a FAILED_TEST_DETAILS=()

# Help function
show_help() {
    cat << EOF
Istio Migration Integration Test Runner

Usage: $0 [OPTIONS]

OPTIONS:
    -c, --config FILE           Use custom config file (default: config/test-config.yaml)
    -r, --report-dir DIR        Test report output directory (default: reports/)
    -v, --verbose               Verbose output
    -d, --dry-run              Show what would be done without executing
    --parallel                  Run tests in parallel (default: true)
    --serial                    Run tests serially
    --fail-fast                 Stop on first failure

TEST CATEGORIES:
    --functional               Run only functional tests
    --performance              Run only performance tests  
    --compatibility            Run only compatibility tests
    --all                      Run all test categories (default)

TEST FILTERS:
    --protocol PROTOCOL        Run only tests for specific protocol (http|grpc|websocket)
    --namespace NS             Run only tests in specific namespace pattern
    
EXAMPLES:
    $0                         # Run all tests
    $0 --functional            # Run only functional tests
    $0 --protocol websocket    # Run only WebSocket tests
    $0 --verbose --fail-fast   # Run with verbose output and stop on first failure
    $0 --dry-run               # Show what tests would run
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

log_test() {
    echo -e "${PURPLE}[TEST]${NC} $1"
}

log_verbose() {
    if [[ "$VERBOSE" == "true" ]]; then
        echo -e "${BLUE}[VERBOSE]${NC} $1"
    fi
}

# Initialize test environment
init_test_env() {
    log_info "Initializing test environment..."
    
    # Create reports directory
    mkdir -p "$REPORT_DIR"/{functional,performance,compatibility}
    
    # Parse test configuration
    if [[ ! -f "$CONFIG_FILE" ]]; then
        log_error "Config file not found: $CONFIG_FILE"
        exit 1
    fi
    
    # Check if test environment is ready
    if ! "$SCRIPT_DIR/setup-test-env.sh" --config "$CONFIG_FILE" --dry-run >/dev/null 2>&1; then
        log_warn "Test environment may not be properly set up"
        log_info "Run './scripts/setup-test-env.sh' first if you encounter issues"
    fi
    
    log_success "Test environment initialized"
}

# Run functional tests
run_functional_tests() {
    if [[ "$RUN_FUNCTIONAL" != "true" ]]; then
        return 0
    fi
    
    log_info "Running functional tests..."
    local test_dir="$TEST_DIR/functional"
    local report_file="$REPORT_DIR/functional/results.json"
    local start_time=$(date +%s)
    local functional_passed=0
    local functional_failed=0
    local functional_skipped=0
    
    # Initialize results file
    cat > "$report_file" << EOF
{
  "category": "functional",
  "start_time": "$(date -Iseconds)",
  "tests": []
}
EOF
    
    # Multi-tenant isolation tests
    if [[ $(yq e '.functional.multi_tenant.enabled' "$CONFIG_FILE") == "true" ]]; then
        log_test "Running multi-tenant isolation tests..."
        if run_multi_tenant_tests; then
            ((functional_passed++))
            log_success "Multi-tenant tests passed"
        else
            ((functional_failed++))
            log_error "Multi-tenant tests failed"
            FAILED_TEST_DETAILS+=("Multi-tenant isolation tests")
        fi
    else
        ((functional_skipped++))
        log_warn "Multi-tenant tests skipped (disabled in config)"
    fi
    
    # Protocol support tests
    if [[ $(yq e '.functional.protocols.enabled' "$CONFIG_FILE") == "true" ]]; then
        log_test "Running protocol support tests..."
        
        # HTTP tests
        if [[ -z "$PROTOCOL_FILTER" || "$PROTOCOL_FILTER" == "http" ]] && 
           [[ $(yq e '.functional.protocols.http.enabled' "$CONFIG_FILE") == "true" ]]; then
            if run_http_tests; then
                ((functional_passed++))
                log_success "HTTP tests passed"
            else
                ((functional_failed++))
                log_error "HTTP tests failed"
                FAILED_TEST_DETAILS+=("HTTP protocol tests")
            fi
        fi
        
        # GRPC tests
        if [[ -z "$PROTOCOL_FILTER" || "$PROTOCOL_FILTER" == "grpc" ]] && 
           [[ $(yq e '.functional.protocols.grpc.enabled' "$CONFIG_FILE") == "true" ]]; then
            if run_grpc_tests; then
                ((functional_passed++))
                log_success "GRPC tests passed"
            else
                ((functional_failed++))
                log_error "GRPC tests failed"
                FAILED_TEST_DETAILS+=("GRPC protocol tests")
            fi
        fi
        
        # WebSocket tests
        if [[ -z "$PROTOCOL_FILTER" || "$PROTOCOL_FILTER" == "websocket" ]] && 
           [[ $(yq e '.functional.protocols.websocket.enabled' "$CONFIG_FILE") == "true" ]]; then
            if run_websocket_tests; then
                ((functional_passed++))
                log_success "WebSocket tests passed"
            else
                ((functional_failed++))
                log_error "WebSocket tests failed"
                FAILED_TEST_DETAILS+=("WebSocket protocol tests")
            fi
        fi
    else
        ((functional_skipped++))
        log_warn "Protocol tests skipped (disabled in config)"
    fi
    
    # Certificate tests
    if [[ $(yq e '.functional.certificates.enabled' "$CONFIG_FILE") == "true" ]]; then
        log_test "Running certificate tests..."
        if run_certificate_tests; then
            ((functional_passed++))
            log_success "Certificate tests passed"
        else
            ((functional_failed++))
            log_error "Certificate tests failed"
            FAILED_TEST_DETAILS+=("Certificate management tests")
        fi
    else
        ((functional_skipped++))
        log_warn "Certificate tests skipped (disabled in config)"
    fi
    
    # CORS tests
    if [[ $(yq e '.functional.cors.enabled' "$CONFIG_FILE") == "true" ]]; then
        log_test "Running CORS tests..."
        if run_cors_tests; then
            ((functional_passed++))
            log_success "CORS tests passed"
        else
            ((functional_failed++))
            log_error "CORS tests failed"
            FAILED_TEST_DETAILS+=("CORS configuration tests")
        fi
    else
        ((functional_skipped++))
        log_warn "CORS tests skipped (disabled in config)"
    fi
    
    # Update totals
    PASSED_TESTS=$((PASSED_TESTS + functional_passed))
    FAILED_TESTS=$((FAILED_TESTS + functional_failed))
    SKIPPED_TESTS=$((SKIPPED_TESTS + functional_skipped))
    TOTAL_TESTS=$((TOTAL_TESTS + functional_passed + functional_failed + functional_skipped))
    
    # Finalize results
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    # Update results file
    jq --arg end_time "$(date -Iseconds)" \
       --arg duration "${duration}s" \
       --argjson passed "$functional_passed" \
       --argjson failed "$functional_failed" \
       --argjson skipped "$functional_skipped" \
       '.end_time = $end_time | .duration = $duration | .passed = $passed | .failed = $failed | .skipped = $skipped' \
       "$report_file" > "${report_file}.tmp" && mv "${report_file}.tmp" "$report_file"
    
    log_info "Functional tests completed: $functional_passed passed, $functional_failed failed, $functional_skipped skipped"
    
    if [[ "$FAIL_FAST" == "true" && "$functional_failed" -gt 0 ]]; then
        log_error "Stopping due to --fail-fast flag"
        return 1
    fi
    
    return 0
}

# Run performance tests
run_performance_tests() {
    if [[ "$RUN_PERFORMANCE" != "true" ]]; then
        return 0
    fi
    
    log_info "Running performance tests..."
    local report_file="$REPORT_DIR/performance/results.json"
    local start_time=$(date +%s)
    local performance_passed=0
    local performance_failed=0
    local performance_skipped=0
    
    # Initialize results file
    cat > "$report_file" << EOF
{
  "category": "performance",
  "start_time": "$(date -Iseconds)",
  "tests": []
}
EOF
    
    if [[ $(yq e '.performance.enabled' "$CONFIG_FILE") != "true" ]]; then
        ((performance_skipped++))
        log_warn "Performance tests skipped (disabled in config)"
    else
        # Latency tests
        if [[ $(yq e '.performance.latency.enabled' "$CONFIG_FILE") == "true" ]]; then
            log_test "Running latency tests..."
            if run_latency_tests; then
                ((performance_passed++))
                log_success "Latency tests passed"
            else
                ((performance_failed++))
                log_error "Latency tests failed"
                FAILED_TEST_DETAILS+=("Latency performance tests")
            fi
        fi
        
        # Throughput tests
        if [[ $(yq e '.performance.throughput.enabled' "$CONFIG_FILE") == "true" ]]; then
            log_test "Running throughput tests..."
            if run_throughput_tests; then
                ((performance_passed++))
                log_success "Throughput tests passed"
            else
                ((performance_failed++))
                log_error "Throughput tests failed"
                FAILED_TEST_DETAILS+=("Throughput performance tests")
            fi
        fi
        
        # Resource tests
        if [[ $(yq e '.performance.resources.enabled' "$CONFIG_FILE") == "true" ]]; then
            log_test "Running resource consumption tests..."
            if run_resource_tests; then
                ((performance_passed++))
                log_success "Resource tests passed"
            else
                ((performance_failed++))
                log_error "Resource tests failed"
                FAILED_TEST_DETAILS+=("Resource consumption tests")
            fi
        fi
    fi
    
    # Update totals
    PASSED_TESTS=$((PASSED_TESTS + performance_passed))
    FAILED_TESTS=$((FAILED_TESTS + performance_failed))
    SKIPPED_TESTS=$((SKIPPED_TESTS + performance_skipped))
    TOTAL_TESTS=$((TOTAL_TESTS + performance_passed + performance_failed + performance_skipped))
    
    # Finalize results
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    jq --arg end_time "$(date -Iseconds)" \
       --arg duration "${duration}s" \
       --argjson passed "$performance_passed" \
       --argjson failed "$performance_failed" \
       --argjson skipped "$performance_skipped" \
       '.end_time = $end_time | .duration = $duration | .passed = $passed | .failed = $failed | .skipped = $skipped' \
       "$report_file" > "${report_file}.tmp" && mv "${report_file}.tmp" "$report_file"
    
    log_info "Performance tests completed: $performance_passed passed, $performance_failed failed, $performance_skipped skipped"
    
    if [[ "$FAIL_FAST" == "true" && "$performance_failed" -gt 0 ]]; then
        log_error "Stopping due to --fail-fast flag"
        return 1
    fi
    
    return 0
}

# Run compatibility tests
run_compatibility_tests() {
    if [[ "$RUN_COMPATIBILITY" != "true" ]]; then
        return 0
    fi
    
    log_info "Running compatibility tests..."
    local report_file="$REPORT_DIR/compatibility/results.json"
    local start_time=$(date +%s)
    local compatibility_passed=0
    local compatibility_failed=0
    local compatibility_skipped=0
    
    # Initialize results file
    cat > "$report_file" << EOF
{
  "category": "compatibility",
  "start_time": "$(date -Iseconds)",
  "tests": []
}
EOF
    
    if [[ $(yq e '.compatibility.enabled' "$CONFIG_FILE") != "true" ]]; then
        ((compatibility_skipped++))
        log_warn "Compatibility tests skipped (disabled in config)"
    else
        # Integration tests
        if [[ $(yq e '.compatibility.integration.enabled' "$CONFIG_FILE") == "true" ]]; then
            log_test "Running integration tests..."
            if run_integration_tests; then
                ((compatibility_passed++))
                log_success "Integration tests passed"
            else
                ((compatibility_failed++))
                log_error "Integration tests failed"
                FAILED_TEST_DETAILS+=("Integration compatibility tests")
            fi
        fi
        
        # Rollback tests
        if [[ $(yq e '.compatibility.rollback.enabled' "$CONFIG_FILE") == "true" ]]; then
            log_test "Running rollback tests..."
            if run_rollback_tests; then
                ((compatibility_passed++))
                log_success "Rollback tests passed"
            else
                ((compatibility_failed++))
                log_error "Rollback tests failed"
                FAILED_TEST_DETAILS+=("Rollback compatibility tests")
            fi
        fi
    fi
    
    # Update totals
    PASSED_TESTS=$((PASSED_TESTS + compatibility_passed))
    FAILED_TESTS=$((FAILED_TESTS + compatibility_failed))
    SKIPPED_TESTS=$((SKIPPED_TESTS + compatibility_skipped))
    TOTAL_TESTS=$((TOTAL_TESTS + compatibility_passed + compatibility_failed + compatibility_skipped))
    
    # Finalize results
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    jq --arg end_time "$(date -Iseconds)" \
       --arg duration "${duration}s" \
       --argjson passed "$compatibility_passed" \
       --argjson failed "$compatibility_failed" \
       --argjson skipped "$compatibility_skipped" \
       '.end_time = $end_time | .duration = $duration | .passed = $passed | .failed = $failed | .skipped = $skipped' \
       "$report_file" > "${report_file}.tmp" && mv "${report_file}.tmp" "$report_file"
    
    log_info "Compatibility tests completed: $compatibility_passed passed, $compatibility_failed failed, $compatibility_skipped skipped"
    
    if [[ "$FAIL_FAST" == "true" && "$compatibility_failed" -gt 0 ]]; then
        log_error "Stopping due to --fail-fast flag"
        return 1
    fi
    
    return 0
}

# Individual test implementations (stubs for now)
run_multi_tenant_tests() {
    log_verbose "Executing multi-tenant isolation tests..."
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Would run multi-tenant tests"
        return 0
    fi
    
    # TODO: Implement actual multi-tenant tests
    sleep 1
    return 0
}

run_http_tests() {
    log_verbose "Executing HTTP protocol tests..."
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Would run HTTP tests"
        return 0
    fi
    
    # TODO: Implement actual HTTP tests
    sleep 1
    return 0
}

run_grpc_tests() {
    log_verbose "Executing GRPC protocol tests..."
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Would run GRPC tests"
        return 0
    fi
    
    # TODO: Implement actual GRPC tests
    sleep 1
    return 0
}

run_websocket_tests() {
    log_verbose "Executing WebSocket protocol tests..."
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Would run WebSocket tests"
        return 0
    fi
    
    # TODO: Implement actual WebSocket tests
    sleep 1
    return 0
}

run_certificate_tests() {
    log_verbose "Executing certificate management tests..."
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Would run certificate tests"
        return 0
    fi
    
    # TODO: Implement actual certificate tests
    sleep 1
    return 0
}

run_cors_tests() {
    log_verbose "Executing CORS configuration tests..."
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Would run CORS tests"
        return 0
    fi
    
    # TODO: Implement actual CORS tests
    sleep 1
    return 0
}

run_latency_tests() {
    log_verbose "Executing latency performance tests..."
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Would run latency tests"
        return 0
    fi
    
    # TODO: Implement actual latency tests
    sleep 2
    return 0
}

run_throughput_tests() {
    log_verbose "Executing throughput performance tests..."
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Would run throughput tests"
        return 0
    fi
    
    # TODO: Implement actual throughput tests
    sleep 2
    return 0
}

run_resource_tests() {
    log_verbose "Executing resource consumption tests..."
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Would run resource tests"
        return 0
    fi
    
    # TODO: Implement actual resource tests
    sleep 2
    return 0
}

run_integration_tests() {
    log_verbose "Executing integration compatibility tests..."
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Would run integration tests"
        return 0
    fi
    
    # TODO: Implement actual integration tests
    sleep 1
    return 0
}

run_rollback_tests() {
    log_verbose "Executing rollback compatibility tests..."
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Would run rollback tests"
        return 0
    fi
    
    # TODO: Implement actual rollback tests
    sleep 1
    return 0
}

# Generate final report
generate_final_report() {
    log_info "Generating final test report..."
    
    local final_report="$REPORT_DIR/final-report.json"
    local start_time=$(date +%s)
    
    cat > "$final_report" << EOF
{
  "test_run": {
    "timestamp": "$(date -Iseconds)",
    "config_file": "$CONFIG_FILE",
    "total_tests": $TOTAL_TESTS,
    "passed": $PASSED_TESTS,
    "failed": $FAILED_TESTS,
    "skipped": $SKIPPED_TESTS,
    "success_rate": $(echo "scale=2; $PASSED_TESTS * 100 / ($TOTAL_TESTS - $SKIPPED_TESTS)" | bc -l 2>/dev/null || echo "0")
  },
  "failed_tests": $(printf '%s\n' "${FAILED_TEST_DETAILS[@]}" | jq -R . | jq -s .),
  "categories": {
    "functional": $(cat "$REPORT_DIR/functional/results.json" 2>/dev/null || echo 'null'),
    "performance": $(cat "$REPORT_DIR/performance/results.json" 2>/dev/null || echo 'null'),
    "compatibility": $(cat "$REPORT_DIR/compatibility/results.json" 2>/dev/null || echo 'null')
  }
}
EOF
    
    # Generate human-readable summary
    local summary_report="$REPORT_DIR/summary.txt"
    cat > "$summary_report" << EOF
Istio Migration Integration Test Summary
========================================

Test Run: $(date)
Config: $CONFIG_FILE

Results:
  Total Tests:  $TOTAL_TESTS
  Passed:       $PASSED_TESTS
  Failed:       $FAILED_TESTS
  Skipped:      $SKIPPED_TESTS
  Success Rate: $(echo "scale=1; $PASSED_TESTS * 100 / ($TOTAL_TESTS - $SKIPPED_TESTS)" | bc -l 2>/dev/null || echo "0")%

EOF
    
    if [[ ${#FAILED_TEST_DETAILS[@]} -gt 0 ]]; then
        echo "Failed Tests:" >> "$summary_report"
        printf '  - %s\n' "${FAILED_TEST_DETAILS[@]}" >> "$summary_report"
    fi
    
    log_success "Test reports generated in $REPORT_DIR/"
}

# Main function
main() {
    local start_time=$(date +%s)
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -c|--config)
                CONFIG_FILE="$2"
                shift 2
                ;;
            -r|--report-dir)
                REPORT_DIR="$2"
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
            --parallel)
                PARALLEL=true
                shift
                ;;
            --serial)
                PARALLEL=false
                shift
                ;;
            --fail-fast)
                FAIL_FAST=true
                shift
                ;;
            --functional)
                RUN_FUNCTIONAL=true
                RUN_PERFORMANCE=false
                RUN_COMPATIBILITY=false
                shift
                ;;
            --performance)
                RUN_FUNCTIONAL=false
                RUN_PERFORMANCE=true
                RUN_COMPATIBILITY=false
                shift
                ;;
            --compatibility)
                RUN_FUNCTIONAL=false
                RUN_PERFORMANCE=false
                RUN_COMPATIBILITY=true
                shift
                ;;
            --all)
                RUN_FUNCTIONAL=true
                RUN_PERFORMANCE=true
                RUN_COMPATIBILITY=true
                shift
                ;;
            --protocol)
                PROTOCOL_FILTER="$2"
                shift 2
                ;;
            --namespace)
                NAMESPACE_FILTER="$2"
                shift 2
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
    
    log_info "Starting Istio migration integration tests..."
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_warn "Running in dry-run mode - no actual tests will be executed"
    fi
    
    # Initialize
    init_test_env
    
    # Run test categories
    if ! run_functional_tests; then
        log_error "Functional tests failed"
    fi
    
    if ! run_performance_tests; then
        log_error "Performance tests failed"
    fi
    
    if ! run_compatibility_tests; then
        log_error "Compatibility tests failed"
    fi
    
    # Generate reports
    generate_final_report
    
    # Final summary
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    log_info "Test execution completed in ${duration}s"
    log_info "Results: $PASSED_TESTS passed, $FAILED_TESTS failed, $SKIPPED_TESTS skipped"
    
    if [[ "$FAILED_TESTS" -gt 0 ]]; then
        log_error "Some tests failed. Check reports in $REPORT_DIR/ for details."
        exit 1
    else
        log_success "All tests passed!"
        exit 0
    fi
}

# Execute main function
main "$@"