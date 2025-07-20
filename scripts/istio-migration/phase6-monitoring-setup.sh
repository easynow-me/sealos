#!/bin/bash

# Phase 6: 24/7 Monitoring Setup Script
# This script sets up comprehensive monitoring for the Istio migration
# Usage: ./phase6-monitoring-setup.sh [--component COMPONENT] [--dry-run]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/utils.sh"

# Configuration
DRY_RUN=false
COMPONENT="all"
MONITORING_NAMESPACE="sealos-monitoring"
ALERT_WEBHOOK_URL="${ALERT_WEBHOOK_URL:-}"
ALERT_EMAIL="${ALERT_EMAIL:-}"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --component)
            COMPONENT="$2"
            shift 2
            ;;
        --webhook-url)
            ALERT_WEBHOOK_URL="$2"
            shift 2
            ;;
        --alert-email)
            ALERT_EMAIL="$2"
            shift 2
            ;;
        --help)
            show_help
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

show_help() {
    cat << EOF
Phase 6: 24/7 Monitoring Setup Script

Usage: $0 [OPTIONS]

Options:
    --component COMPONENT   Setup specific monitoring component (all, metrics, alerts, dashboard)
    --webhook-url URL      Webhook URL for alerts (e.g., Slack, DingTalk)
    --alert-email EMAIL    Email address for critical alerts
    --dry-run              Show what would be done without making changes
    --help                 Show this help message

Components:
    metrics     Setup Prometheus metrics collection
    alerts      Configure AlertManager rules
    dashboard   Deploy Grafana dashboards
    all         Setup all components

Example:
    $0 --component all --webhook-url https://hooks.slack.com/xxx --alert-email ops@example.com
EOF
}

# Setup Prometheus metrics collection
setup_metrics_collection() {
    echo "Setting up Prometheus metrics collection..."
    
    if [[ "$DRY_RUN" == "true" ]]; then
        echo "[DRY-RUN] Would create ServiceMonitor resources for Istio components"
        return
    fi
    
    # Create ServiceMonitor for Istio components
    cat > /tmp/istio-servicemonitor.yaml << EOF
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: istio-component-monitor
  namespace: $MONITORING_NAMESPACE
spec:
  selector:
    matchExpressions:
    - key: app
      operator: In
      values:
      - istiod
      - istio-ingressgateway
      - istio-egressgateway
  endpoints:
  - port: http-monitoring
    interval: 30s
    path: /stats/prometheus
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: istio-mesh-metrics
  namespace: $MONITORING_NAMESPACE
spec:
  selector:
    matchExpressions:
    - key: app.kubernetes.io/managed-by
      operator: Exists
  namespaceSelector:
    matchExpressions:
    - key: name
      operator: In
      values:
      - istio-system
      - sealos-system
  endpoints:
  - port: http-metrics
    interval: 30s
    path: /stats/prometheus
    relabelings:
    - action: keep
      regex: .*-envoy-prom
      sourceLabels:
      - __name__
EOF

    kubectl apply -f /tmp/istio-servicemonitor.yaml
    echo "✅ ServiceMonitor resources created"
    
    # Create PodMonitor for application sidecars
    cat > /tmp/sidecar-podmonitor.yaml << EOF
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: envoy-sidecar-monitor
  namespace: $MONITORING_NAMESPACE
spec:
  selector:
    matchLabels:
      sidecar.istio.io/inject: "true"
  namespaceSelector:
    matchExpressions:
    - key: kubernetes.io/metadata.name
      operator: In
      values:
      - ns-*
  podMetricsEndpoints:
  - path: /stats/prometheus
    port: "15090"
    interval: 30s
    relabelings:
    - action: keep
      regex: (istio_request_duration_milliseconds|istio_tcp_connections_opened_total|istio_tcp_connections_closed_total)
      sourceLabels:
      - __name__
EOF

    kubectl apply -f /tmp/sidecar-podmonitor.yaml
    echo "✅ PodMonitor resources created"
}

# Setup AlertManager rules
setup_alert_rules() {
    echo "Setting up AlertManager rules..."
    
    if [[ "$DRY_RUN" == "true" ]]; then
        echo "[DRY-RUN] Would create PrometheusRule resources for Istio monitoring"
        return
    fi
    
    # Create comprehensive alert rules
    cat > /tmp/istio-alert-rules.yaml << EOF
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: istio-migration-alerts
  namespace: $MONITORING_NAMESPACE
spec:
  groups:
  - name: istio_performance
    interval: 30s
    rules:
    - alert: HighRequestLatency
      expr: |
        histogram_quantile(0.95,
          sum(rate(istio_request_duration_milliseconds_bucket[5m])) by (destination_service_name, le)
        ) > 1000
      for: 5m
      labels:
        severity: warning
        component: istio
      annotations:
        summary: "High request latency detected"
        description: "95th percentile latency for {{ \$labels.destination_service_name }} is {{ \$value }}ms"
    
    - alert: CriticalRequestLatency
      expr: |
        histogram_quantile(0.95,
          sum(rate(istio_request_duration_milliseconds_bucket[5m])) by (destination_service_name, le)
        ) > 5000
      for: 5m
      labels:
        severity: critical
        component: istio
      annotations:
        summary: "Critical request latency detected"
        description: "95th percentile latency for {{ \$labels.destination_service_name }} is {{ \$value }}ms"
    
    - alert: HighErrorRate
      expr: |
        sum(rate(istio_request_total{response_code=~"5.."}[5m])) by (destination_service_name)
        /
        sum(rate(istio_request_total[5m])) by (destination_service_name)
        > 0.05
      for: 5m
      labels:
        severity: warning
        component: istio
      annotations:
        summary: "High error rate detected"
        description: "Error rate for {{ \$labels.destination_service_name }} is {{ \$value | humanizePercentage }}"
    
    - alert: ServiceDown
      expr: |
        up{job=~".*-envoy-prom"} == 0
      for: 5m
      labels:
        severity: critical
        component: istio
      annotations:
        summary: "Service is down"
        description: "{{ \$labels.job }} has been down for more than 5 minutes"
    
  - name: istio_resources
    interval: 30s
    rules:
    - alert: GatewayDown
      expr: |
        kube_deployment_status_replicas_available{deployment="istio-ingressgateway"} == 0
      for: 5m
      labels:
        severity: critical
        component: istio
      annotations:
        summary: "Istio Gateway is down"
        description: "No available replicas for Istio IngressGateway"
    
    - alert: IstiodDown
      expr: |
        kube_deployment_status_replicas_available{deployment="istiod"} == 0
      for: 5m
      labels:
        severity: critical
        component: istio
      annotations:
        summary: "Istiod is down"
        description: "No available replicas for Istiod control plane"
    
    - alert: SidecarInjectionFailure
      expr: |
        increase(sidecar_injection_failure_total[5m]) > 0
      for: 5m
      labels:
        severity: warning
        component: istio
      annotations:
        summary: "Sidecar injection failures detected"
        description: "{{ \$value }} sidecar injection failures in the last 5 minutes"
    
  - name: migration_specific
    interval: 30s
    rules:
    - alert: IngressStillActive
      expr: |
        count(kube_ingress_info) by (namespace) > 0
      for: 30m
      labels:
        severity: warning
        component: migration
      annotations:
        summary: "Ingress resources still active"
        description: "{{ \$value }} Ingress resources found in namespace {{ \$labels.namespace }}"
    
    - alert: VirtualServiceMisconfigured
      expr: |
        istio_request_total{response_code="503"} > 0
      for: 5m
      labels:
        severity: warning
        component: migration
      annotations:
        summary: "VirtualService may be misconfigured"
        description: "503 errors detected for {{ \$labels.destination_service_name }}"
    
    - alert: TrafficShiftAnomaly
      expr: |
        abs(
          rate(istio_request_total[5m]) - 
          rate(istio_request_total[5m] offset 1h)
        ) / rate(istio_request_total[5m] offset 1h) > 0.5
      for: 10m
      labels:
        severity: warning
        component: migration
      annotations:
        summary: "Significant traffic shift detected"
        description: "Traffic for {{ \$labels.destination_service_name }} changed by {{ \$value | humanizePercentage }}"
EOF

    kubectl apply -f /tmp/istio-alert-rules.yaml
    echo "✅ Alert rules created"
    
    # Configure AlertManager
    if [[ -n "$ALERT_WEBHOOK_URL" ]] || [[ -n "$ALERT_EMAIL" ]]; then
        setup_alertmanager_config
    fi
}

# Configure AlertManager
setup_alertmanager_config() {
    echo "Configuring AlertManager..."
    
    cat > /tmp/alertmanager-config.yaml << EOF
apiVersion: v1
kind: Secret
metadata:
  name: alertmanager-main
  namespace: $MONITORING_NAMESPACE
stringData:
  alertmanager.yaml: |
    global:
      resolve_timeout: 5m
    route:
      group_by: ['alertname', 'severity', 'component']
      group_wait: 10s
      group_interval: 10s
      repeat_interval: 12h
      receiver: 'default'
      routes:
      - match:
          severity: critical
        receiver: 'critical'
        continue: true
      - match:
          component: migration
        receiver: 'migration'
    receivers:
    - name: 'default'
EOF

    # Add webhook receiver if configured
    if [[ -n "$ALERT_WEBHOOK_URL" ]]; then
        cat >> /tmp/alertmanager-config.yaml << EOF
      webhook_configs:
      - url: '$ALERT_WEBHOOK_URL'
        send_resolved: true
EOF
    fi

    # Add email receiver if configured
    if [[ -n "$ALERT_EMAIL" ]]; then
        cat >> /tmp/alertmanager-config.yaml << EOF
      email_configs:
      - to: '$ALERT_EMAIL'
        from: 'sealos-monitoring@example.com'
        smarthost: 'smtp.example.com:587'
        auth_username: 'monitoring@example.com'
        auth_password: 'password'
        headers:
          Subject: 'Sealos Istio Alert: {{ .GroupLabels.alertname }}'
EOF
    fi

    # Add critical receiver
    cat >> /tmp/alertmanager-config.yaml << EOF
    - name: 'critical'
EOF

    if [[ -n "$ALERT_WEBHOOK_URL" ]]; then
        cat >> /tmp/alertmanager-config.yaml << EOF
      webhook_configs:
      - url: '$ALERT_WEBHOOK_URL'
        send_resolved: true
EOF
    fi

    # Add migration receiver
    cat >> /tmp/alertmanager-config.yaml << EOF
    - name: 'migration'
EOF

    if [[ -n "$ALERT_WEBHOOK_URL" ]]; then
        cat >> /tmp/alertmanager-config.yaml << EOF
      webhook_configs:
      - url: '$ALERT_WEBHOOK_URL'
        send_resolved: true
EOF
    fi

    kubectl apply -f /tmp/alertmanager-config.yaml
    echo "✅ AlertManager configured"
}

# Setup Grafana dashboards
setup_grafana_dashboards() {
    echo "Setting up Grafana dashboards..."
    
    if [[ "$DRY_RUN" == "true" ]]; then
        echo "[DRY-RUN] Would create Grafana dashboard ConfigMaps"
        return
    fi
    
    # Create Istio overview dashboard
    cat > /tmp/istio-overview-dashboard.yaml << EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: istio-overview-dashboard
  namespace: $MONITORING_NAMESPACE
  labels:
    grafana_dashboard: "1"
data:
  istio-overview.json: |
    {
      "dashboard": {
        "title": "Istio Migration Overview",
        "panels": [
          {
            "title": "Request Rate",
            "targets": [
              {
                "expr": "sum(rate(istio_request_total[5m])) by (destination_service_name)"
              }
            ],
            "gridPos": {"x": 0, "y": 0, "w": 12, "h": 8}
          },
          {
            "title": "P95 Latency",
            "targets": [
              {
                "expr": "histogram_quantile(0.95, sum(rate(istio_request_duration_milliseconds_bucket[5m])) by (destination_service_name, le))"
              }
            ],
            "gridPos": {"x": 12, "y": 0, "w": 12, "h": 8}
          },
          {
            "title": "Error Rate",
            "targets": [
              {
                "expr": "sum(rate(istio_request_total{response_code=~\"5..\"}[5m])) by (destination_service_name) / sum(rate(istio_request_total[5m])) by (destination_service_name)"
              }
            ],
            "gridPos": {"x": 0, "y": 8, "w": 12, "h": 8}
          },
          {
            "title": "Active VirtualServices",
            "targets": [
              {
                "expr": "count(kube_virtualservice_info) by (namespace)"
              }
            ],
            "gridPos": {"x": 12, "y": 8, "w": 12, "h": 8}
          }
        ]
      }
    }
EOF

    kubectl apply -f /tmp/istio-overview-dashboard.yaml
    echo "✅ Grafana dashboards created"
}

# Setup performance tracking
setup_performance_tracking() {
    echo "Setting up performance tracking..."
    
    # Create a CronJob for regular performance reports
    cat > /tmp/performance-tracker.yaml << EOF
apiVersion: batch/v1
kind: CronJob
metadata:
  name: istio-performance-tracker
  namespace: $MONITORING_NAMESPACE
spec:
  schedule: "0 * * * *"  # Every hour
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: performance-tracker
            image: curlimages/curl:latest
            command:
            - /bin/sh
            - -c
            - |
              # Query Prometheus for key metrics
              PROM_URL="http://prometheus:9090"
              
              # Get average latency
              AVG_LATENCY=\$(curl -s "\$PROM_URL/api/v1/query?query=avg(histogram_quantile(0.95,sum(rate(istio_request_duration_milliseconds_bucket[1h]))by(le)))" | jq -r '.data.result[0].value[1]')
              
              # Get error rate
              ERROR_RATE=\$(curl -s "\$PROM_URL/api/v1/query?query=sum(rate(istio_request_total{response_code=~\"5..\"}[1h]))/sum(rate(istio_request_total[1h]))" | jq -r '.data.result[0].value[1]')
              
              # Get request rate
              REQUEST_RATE=\$(curl -s "\$PROM_URL/api/v1/query?query=sum(rate(istio_request_total[1h]))" | jq -r '.data.result[0].value[1]')
              
              # Log metrics
              echo "Performance Report: \$(date)"
              echo "Average P95 Latency: \$AVG_LATENCY ms"
              echo "Error Rate: \$ERROR_RATE"
              echo "Request Rate: \$REQUEST_RATE req/s"
              
              # Send to webhook if configured
              if [ -n "$ALERT_WEBHOOK_URL" ]; then
                curl -X POST "$ALERT_WEBHOOK_URL" \
                  -H 'Content-Type: application/json' \
                  -d "{\"text\":\"Hourly Performance Report\\nP95 Latency: \${AVG_LATENCY}ms\\nError Rate: \${ERROR_RATE}\\nRequest Rate: \${REQUEST_RATE} req/s\"}"
              fi
          restartPolicy: OnFailure
EOF

    kubectl apply -f /tmp/performance-tracker.yaml
    echo "✅ Performance tracking CronJob created"
}

# Main execution
main() {
    echo "Phase 6: Setting up 24/7 monitoring for Istio migration"
    echo "Configuration: COMPONENT=$COMPONENT, DRY_RUN=$DRY_RUN"
    
    # Create monitoring namespace if it doesn't exist
    if [[ "$DRY_RUN" != "true" ]]; then
        kubectl create namespace "$MONITORING_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
    fi
    
    case "$COMPONENT" in
        "all")
            setup_metrics_collection
            setup_alert_rules
            setup_grafana_dashboards
            setup_performance_tracking
            ;;
        "metrics")
            setup_metrics_collection
            ;;
        "alerts")
            setup_alert_rules
            ;;
        "dashboard")
            setup_grafana_dashboards
            ;;
        *)
            echo "Unknown component: $COMPONENT"
            show_help
            exit 1
            ;;
    esac
    
    echo ""
    echo "✅ Monitoring setup completed!"
    echo ""
    echo "Next steps:"
    echo "1. Access Grafana dashboards to view real-time metrics"
    echo "2. Check AlertManager for any active alerts"
    echo "3. Review performance tracking reports hourly"
    echo ""
    echo "Useful commands:"
    echo "- View active alerts: kubectl get alerts -n $MONITORING_NAMESPACE"
    echo "- Check metrics: kubectl port-forward -n $MONITORING_NAMESPACE svc/prometheus 9090:9090"
    echo "- Access Grafana: kubectl port-forward -n $MONITORING_NAMESPACE svc/grafana 3000:3000"
}

# Run main function
main