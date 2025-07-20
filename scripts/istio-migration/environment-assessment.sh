#!/bin/bash

# Environment Assessment Script for Istio Migration
# This script analyzes the current environment and recommends a migration approach
# Usage: ./environment-assessment.sh

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Assessment results
SCORE=0
RISKS=()
RECOMMENDATIONS=()

echo -e "${BLUE}=== Sealos Istio Migration Environment Assessment ===${NC}\n"

# Function to add risk
add_risk() {
    RISKS+=("$1")
}

# Function to add recommendation
add_recommendation() {
    RECOMMENDATIONS+=("$1")
}

# 1. Check Kubernetes version
echo -e "${YELLOW}Checking Kubernetes version...${NC}"
K8S_VERSION=$(kubectl version --short 2>/dev/null | grep Server | awk '{print $3}')
K8S_MINOR=$(echo $K8S_VERSION | cut -d. -f2)

if [ "$K8S_MINOR" -ge 27 ]; then
    echo -e "${GREEN}✓ Kubernetes version $K8S_VERSION is supported${NC}"
    SCORE=$((SCORE + 10))
else
    echo -e "${RED}✗ Kubernetes version $K8S_VERSION may have compatibility issues${NC}"
    add_risk "Kubernetes version below 1.27"
    add_recommendation "Upgrade Kubernetes to 1.27 or higher"
fi

# 2. Count services and namespaces
echo -e "\n${YELLOW}Analyzing cluster scale...${NC}"
TOTAL_NAMESPACES=$(kubectl get namespaces | grep "^ns-" | wc -l)
TOTAL_SERVICES=$(kubectl get services --all-namespaces | grep "^ns-" | wc -l)
TOTAL_INGRESS=$(kubectl get ingress --all-namespaces --no-headers 2>/dev/null | wc -l)

echo "User namespaces: $TOTAL_NAMESPACES"
echo "User services: $TOTAL_SERVICES"
echo "Total Ingress resources: $TOTAL_INGRESS"

# Determine scale
if [ "$TOTAL_SERVICES" -lt 50 ]; then
    SCALE="SMALL"
    SCORE=$((SCORE + 20))
elif [ "$TOTAL_SERVICES" -lt 200 ]; then
    SCALE="MEDIUM"
    SCORE=$((SCORE + 15))
else
    SCALE="LARGE"
    SCORE=$((SCORE + 10))
    add_risk "Large number of services requires careful planning"
fi

# 3. Check existing Istio installation
echo -e "\n${YELLOW}Checking for existing Istio...${NC}"
if kubectl get namespace istio-system >/dev/null 2>&1; then
    echo -e "${GREEN}✓ Istio namespace exists${NC}"
    ISTIO_INSTALLED=true
    
    # Check Istio version
    if command -v istioctl >/dev/null 2>&1; then
        ISTIO_VERSION=$(istioctl version --remote --short 2>/dev/null || echo "Unknown")
        echo "Istio version: $ISTIO_VERSION"
    fi
    SCORE=$((SCORE + 10))
else
    echo -e "${YELLOW}! Istio not installed${NC}"
    ISTIO_INSTALLED=false
    add_recommendation "Install Istio 1.20.x before migration"
fi

# 4. Check LoadBalancer services
echo -e "\n${YELLOW}Checking LoadBalancer services...${NC}"
LB_SERVICES=$(kubectl get services --all-namespaces --field-selector spec.type=LoadBalancer | grep "^ns-" | wc -l || echo 0)

if [ "$LB_SERVICES" -eq 0 ]; then
    echo -e "${GREEN}✓ No LoadBalancer services in user namespaces (good!)${NC}"
    SCORE=$((SCORE + 10))
else
    echo -e "${RED}✗ Found $LB_SERVICES LoadBalancer services${NC}"
    add_risk "LoadBalancer services need to be migrated to NodePort"
    add_recommendation "Convert LoadBalancer services to NodePort before migration"
fi

# 5. Check resource availability
echo -e "\n${YELLOW}Checking resource availability...${NC}"
NODES=$(kubectl get nodes --no-headers | wc -l)
TOTAL_CPU=$(kubectl get nodes --no-headers | awk '{print $2}' | grep -oE '[0-9]+' | awk '{s+=$1} END {print s}')
TOTAL_MEMORY=$(kubectl top nodes --no-headers 2>/dev/null | awk '{gsub(/[^0-9]/,"",$4); s+=$4} END {print s/1024}' || echo "Unknown")

echo "Total nodes: $NODES"
echo "Total CPU cores: ${TOTAL_CPU:-Unknown}"
echo "Total memory: ${TOTAL_MEMORY:-Unknown} GB"

if [ "$NODES" -ge 3 ]; then
    echo -e "${GREEN}✓ Sufficient nodes for HA deployment${NC}"
    SCORE=$((SCORE + 10))
else
    echo -e "${YELLOW}! Limited nodes may affect HA deployment${NC}"
    add_risk "Less than 3 nodes available"
fi

# 6. Check monitoring stack
echo -e "\n${YELLOW}Checking monitoring capabilities...${NC}"
if kubectl get deployment -n kube-system prometheus-server >/dev/null 2>&1 || \
   kubectl get deployment -n monitoring prometheus >/dev/null 2>&1; then
    echo -e "${GREEN}✓ Prometheus is installed${NC}"
    MONITORING=true
    SCORE=$((SCORE + 5))
else
    echo -e "${YELLOW}! Prometheus not found${NC}"
    MONITORING=false
    add_recommendation "Deploy Prometheus for migration monitoring"
fi

if kubectl get deployment -n kube-system grafana >/dev/null 2>&1 || \
   kubectl get deployment -n monitoring grafana >/dev/null 2>&1; then
    echo -e "${GREEN}✓ Grafana is installed${NC}"
    SCORE=$((SCORE + 5))
else
    echo -e "${YELLOW}! Grafana not found${NC}"
    add_recommendation "Deploy Grafana for visualization"
fi

# 7. Check cert-manager
echo -e "\n${YELLOW}Checking cert-manager...${NC}"
if kubectl get namespace cert-manager >/dev/null 2>&1; then
    echo -e "${GREEN}✓ cert-manager is installed${NC}"
    SCORE=$((SCORE + 10))
else
    echo -e "${YELLOW}! cert-manager not found${NC}"
    add_recommendation "Install cert-manager for automatic certificate management"
fi

# 8. Check team readiness
echo -e "\n${YELLOW}Checking configurations...${NC}"
# Check if controllers are already configured for Istio
ISTIO_READY_CONTROLLERS=0
for controller in terminal-controller adminer-controller resources-controller; do
    if kubectl get deployment/$controller -n sealos-system -o yaml 2>/dev/null | grep -q "USE_ISTIO_NETWORKING"; then
        ISTIO_READY_CONTROLLERS=$((ISTIO_READY_CONTROLLERS + 1))
    fi
done

if [ "$ISTIO_READY_CONTROLLERS" -gt 0 ]; then
    echo -e "${GREEN}✓ $ISTIO_READY_CONTROLLERS controllers are Istio-ready${NC}"
    SCORE=$((SCORE + 10))
else
    echo -e "${YELLOW}! Controllers need Istio configuration${NC}"
fi

# 9. Analyze traffic patterns
echo -e "\n${YELLOW}Analyzing traffic patterns...${NC}"
# Check for WebSocket usage
WS_INGRESS=$(kubectl get ingress --all-namespaces -o yaml 2>/dev/null | grep -i websocket | wc -l || echo 0)
if [ "$WS_INGRESS" -gt 0 ]; then
    echo "Found WebSocket configurations: $WS_INGRESS"
    add_recommendation "Pay special attention to WebSocket service migration"
fi

# Check for gRPC usage
GRPC_SERVICES=$(kubectl get services --all-namespaces -o yaml 2>/dev/null | grep -E "grpc|:50051|:9000" | wc -l || echo 0)
if [ "$GRPC_SERVICES" -gt 0 ]; then
    echo "Possible gRPC services found: $GRPC_SERVICES"
    add_recommendation "Verify gRPC service configurations during migration"
fi

# Calculate final score and recommendation
echo -e "\n${BLUE}=== Assessment Results ===${NC}\n"

echo -e "${YELLOW}Environment Score: $SCORE/100${NC}"

# Determine migration approach based on score and scale
if [ "$SCORE" -ge 80 ] && [ "$SCALE" = "SMALL" ]; then
    APPROACH="${GREEN}✓ Quick Migration (1-2 hours)${NC}"
    APPROACH_DESC="Your environment is well-prepared for a quick migration."
elif [ "$SCORE" -ge 60 ] && [ "$SCALE" != "LARGE" ]; then
    APPROACH="${GREEN}✓ Standard Migration (4-8 hours)${NC}"
    APPROACH_DESC="Your environment is suitable for standard migration with minimal preparation."
elif [ "$SCORE" -ge 40 ] || [ "$SCALE" = "LARGE" ]; then
    APPROACH="${YELLOW}⚡ Gradual Migration (1-2 days)${NC}"
    APPROACH_DESC="Recommend gradual migration due to scale or missing prerequisites."
else
    APPROACH="${RED}⚠ Conservative Migration (3-5 days)${NC}"
    APPROACH_DESC="Significant preparation needed before migration."
fi

echo -e "\nRecommended Approach: $APPROACH"
echo -e "$APPROACH_DESC"

# Display risks
if [ ${#RISKS[@]} -gt 0 ]; then
    echo -e "\n${RED}Identified Risks:${NC}"
    for risk in "${RISKS[@]}"; do
        echo "  ⚠ $risk"
    done
fi

# Display recommendations
if [ ${#RECOMMENDATIONS[@]} -gt 0 ]; then
    echo -e "\n${YELLOW}Recommendations:${NC}"
    for rec in "${RECOMMENDATIONS[@]}"; do
        echo "  → $rec"
    done
fi

# Generate report file
REPORT_FILE="istio-migration-assessment-$(date +%Y%m%d-%H%M%S).txt"
{
    echo "Sealos Istio Migration Assessment Report"
    echo "Generated: $(date)"
    echo "=================================="
    echo ""
    echo "Cluster Information:"
    echo "  Kubernetes Version: $K8S_VERSION"
    echo "  User Namespaces: $TOTAL_NAMESPACES"
    echo "  User Services: $TOTAL_SERVICES"
    echo "  Ingress Resources: $TOTAL_INGRESS"
    echo "  Nodes: $NODES"
    echo "  Scale Category: $SCALE"
    echo ""
    echo "Readiness Score: $SCORE/100"
    echo ""
    echo "Recommended Migration Approach: $(echo $APPROACH | sed 's/\x1b\[[0-9;]*m//g')"
    echo ""
    if [ ${#RISKS[@]} -gt 0 ]; then
        echo "Risks:"
        for risk in "${RISKS[@]}"; do
            echo "  - $risk"
        done
        echo ""
    fi
    if [ ${#RECOMMENDATIONS[@]} -gt 0 ]; then
        echo "Action Items:"
        for rec in "${RECOMMENDATIONS[@]}"; do
            echo "  - $rec"
        done
    fi
} > "$REPORT_FILE"

echo -e "\n${GREEN}Assessment report saved to: $REPORT_FILE${NC}"

# Provide next steps
echo -e "\n${BLUE}=== Next Steps ===${NC}"
case "$APPROACH" in
    *"Quick"*)
        echo "1. Review and address any risks identified above"
        echo "2. Run: ./docs/istio-migration/quick-migration-checklist.md"
        echo "3. Execute: ./scripts/istio-migration/phase6-full-cutover.sh --step all"
        ;;
    *"Standard"*)
        echo "1. Address the recommendations above"
        echo "2. Follow: ./docs/istio-migration/complete-migration-guide.md"
        echo "3. Execute migration in planned maintenance window"
        ;;
    *"Gradual"*)
        echo "1. Complete all recommended preparations"
        echo "2. Review: ./docs/istio-migration/migration-decision-tree.md"
        echo "3. Plan phased migration over 1-2 days"
        ;;
    *"Conservative"*)
        echo "1. Address all risks and recommendations"
        echo "2. Consider getting expert assistance"
        echo "3. Follow conservative migration plan (3-5 days)"
        ;;
esac

echo -e "\nFor detailed guidance, see: ./docs/istio-migration/migration-decision-tree.md"