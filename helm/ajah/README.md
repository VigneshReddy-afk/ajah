# Ajah Helm Chart

Deploy Ajah — self-hosted LLM observability
gateway — on Kubernetes.

## Prerequisites

- Kubernetes 1.24+
- Helm 3.0+

## Installation

helm install ajah ./helm/ajah \
  --namespace ajah \
  --create-namespace

## Access

kubectl port-forward svc/ajah-gateway 8080:8080 -n ajah
kubectl port-forward svc/ajah-dashboard 3000:3000 -n ajah

Dashboard: http://localhost:3000
Gateway: http://localhost:8080

## Configuration

Override values in your own file:

helm install ajah ./helm/ajah \
  -f my-values.yaml \
  --namespace ajah \
  --create-namespace

Key values to override:

| Value | Description | Default |
|-------|-------------|---------|
| clickhouse.password | ClickHouse password | ajahprod |
| postgres.password | PostgreSQL password | observatory |
| config.otelEndpoint | OpenTelemetry endpoint | "" |
| ingress.enabled | Enable ingress | false |

## Uninstall

helm uninstall ajah -n ajah
