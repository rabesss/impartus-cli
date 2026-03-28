# Observability Guide

This document describes the observability stack for the Impartus CLI, including metrics, logging, and error tracking.

## Metrics & Monitoring

### OpenTelemetry Metrics

The CLI exports metrics via OpenTelemetry when configured. To enable metrics export:

```bash
# Set OTLP endpoint for metrics export
export OTEL_EXPORTER_OTLP_ENDPOINT=https://your-otel-collector:4317
```

### Available Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `impartus_downloads_total` | Counter | Total downloads |
| `impartus_download_duration_seconds` | Histogram | Download duration |
| `impartus_download_errors_total` | Counter | Download errors |
| `impartus_download_bytes_total` | Counter | Total bytes downloaded |
| `impartus_active_downloads` | Gauge | Active downloads |
| `impartus_api_requests_total` | Counter | API requests |
| `impartus_api_request_duration_seconds` | Histogram | API request duration |
| `impartus_api_errors_total` | Counter | API errors |
| `impartus_active_jobs` | Gauge | Active jobs |
| `impartus_jobs_completed_total` | Counter | Completed jobs |
| `impartus_jobs_failed_total` | Counter | Failed jobs |

### Prometheus Integration

To expose metrics for Prometheus scraping, enable the metrics endpoint via config.json `metricsEndpoint: true` or `IMPARTUS_METRICS_ENDPOINT=true`.

Metrics will be available at `/metrics` when running the API server.

### Monitoring Dashboards

#### Grafana

Import the OpenTelemetry metrics into Grafana:

1. Configure OTLP endpoint in your Grafana OTEL data source
2. Use PromQL queries to visualize metrics

Example queries:

```promql
# Download success rate
rate(impartus_downloads_total[5m])

# Average download duration
rate(impartus_download_duration_seconds_sum[5m]) / rate(impartus_download_duration_seconds_count[5m])

# Error rate
rate(impartus_download_errors_total[5m]) / rate(impartus_downloads_total[5m])

# Active downloads
impartus_active_downloads
```

#### Datadog

For Datadog integration:

1. Set `OTEL_EXPORTER_OTLP_ENDPOINT` to your Datadog OTLP intake
2. Configure API key: `DD_API_KEY`

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=https://api.datadoghq.com:4317
export DD_API_KEY=your-datadog-api-key
export DD_SITE=datadoghq.com
```

#### New Relic

For New Relic integration:

1. Set OTLP endpoint to New Relic's OTLP endpoint
2. Configure license key

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp.nr-data.net:4317
export NEW_RELIC_LICENSE_KEY=your-license-key
```

## Error Tracking

### Sentry Integration

Sentry is integrated for error tracking. To enable:

```bash
export SENTRY_DSN=https://your-sentry-dsn@o123456.ingest.sentry.io/1234567
export SENTRY_ENVIRONMENT=production
export SENTRY_RELEASE=impartus-cli@<release-version>
```

### Sentry Features

- **Error grouping**: Similar errors are grouped automatically
- **Stack traces**: Full Go stack traces for crashes
- **Context**: Request IDs, user info, and custom tags
- **Release tracking**: Track which version introduced errors

### Sentry Dashboard

View errors at: https://sentry.io/organizations/your-org/issues/

## Logging

### Structured Logging

Logs are written to stderr with structured format:

```json
{
  "level": "info",
  "timestamp": "2026-03-21T12:00:00Z",
  "message": "Download started",
  "request_id": "abc-123",
  "lecture_id": 456
}
```

### Log Levels

Configure log level via environment:

```bash
# Enable verbose logging via config.json verbose: true or set IMpartus_VERBOSE=true
```

### Log Sanitization

Sensitive data is automatically redacted:
- Passwords
- Tokens
- API keys
- Email addresses (partial)

## Alerting

### Webhook Alerts

Configure alerts for important events:

```bash
export ALERT_WEBHOOK_URL=https://your-webhook-endpoint
export ALERT_ON_ERRORS=true
```

### Slack Integration

For Slack notifications, configure a Slack webhook:

```bash
export ALERT_WEBHOOK_URL=https://hooks.slack.com/services/xxx/yyy/zzz
```

### PagerDuty Integration

For PagerDuty alerts:

```bash
export ALERT_WEBHOOK_URL=https://events.pagerduty.com/v2/enqueue
export PD_ROUTING_KEY=your-routing-key
```

## Deployment Observability

### Health Checks

Monitor deployment health:

```bash
curl http://localhost:8080/api/v1/health
```

Response:
```json
{
  "status": "healthy",
  "timestamp": "2026-03-21T12:00:00Z"
}
```

### Key Performance Indicators

Track these KPIs for deployment health:

1. **Download Success Rate**: Should be > 95%
2. **API Error Rate**: Should be < 5%
3. **Job Completion Rate**: Should be > 90%
4. **Average Download Duration**: Monitor for degradation

### Dashboard Links

Configure these dashboard URLs based on your observability stack:

| Tool | Dashboard URL |
|------|---------------|
| Grafana | https://grafana.example.com/d/impartus |
| Datadog | https://app.datadoghq.com/dashboard/impartus |
| New Relic | https://one.newrelic.com/entity/123456789 |
| Sentry | https://sentry.io/organizations/your-org/issues/ |

## Metrics Endpoint

When running the API server, metrics are available at:

```
GET /api/v1/metrics
```

Returns Prometheus-formatted metrics.

## Environment Variables

| Variable | Description |
|----------|-------------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OpenTelemetry collector endpoint |
| `SENTRY_DSN` | Sentry DSN for error tracking |
| `SENTRY_ENVIRONMENT` | Environment name (production, staging) |
| `SENTRY_RELEASE` | Release version |
| `ALERT_WEBHOOK_URL` | Webhook URL for alerts |
| `ALERT_ON_ERRORS` | Enable error alerts |
| `DD_API_KEY` | Datadog API key |
| `NEW_RELIC_LICENSE_KEY` | New Relic license key |
