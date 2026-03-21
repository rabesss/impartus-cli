# Runbooks - Incident Response Playbooks

This document provides operational runbooks for common incidents and troubleshooting scenarios.

## Table of Contents

1. [Service Health Checks](#service-health-checks)
2. [Common Issues & Troubleshooting](#common-issues--troubleshooting)
3. [Incident Response Procedures](#incident-response-procedures)
4. [Rollback Procedures](#rollback-procedures)
5. [Monitoring & Alerts](#monitoring--alerts)

---

## Service Health Checks

### Check API Server Status

```bash
# Health endpoint
curl -s http://localhost:8080/api/v1/health

# Expected response
{"status":"healthy","timestamp":"2026-02-26T..."}
```

### Check Download Jobs

```bash
# List active jobs
curl -s http://localhost:8080/api/v1/jobs \
  -H "Authorization: Bearer <token>" | jq

# Check specific job
curl -s http://localhost:8080/api/v1/jobs/<job-id> \
  -H "Authorization: Bearer <token>" | jq
```

---

## Common Issues & Troubleshooting

### Issue: "ffmpeg not found" Error

**Symptoms:** Download fails with "please add ffmpeg to your path"

**Resolution:**
```bash
# Check ffmpeg is installed
ffmpeg -version

# Install on Ubuntu/Debian
sudo apt update && sudo apt install ffmpeg

# Install on macOS
brew install ffmpeg

# Install on Arch Linux
sudo pacman -S ffmpeg
```

### Issue: Authentication Failed (401)

**Symptoms:** API returns 401 Unauthorized

**Resolution:**
1. Verify credentials in `config.json` match Impartus account
2. Check base URL is correct (e.g., `http://bitshyd.impartus.com/api`)
3. Test credentials manually via browser login
4. Regenerate token: `POST /api/v1/auth/login`

### Issue: Download Timeout

**Symptoms:** "context deadline exceeded" or chunk download failures

**Resolution:**
1. Increase HTTP timeout in config:
```json
{
  "httpTimeout": "20m"
}
```
2. Reduce parallel workers:
```json
{
  "numWorkers": 3,
  "downloadWorkersPerLecture": 2
}
```
3. Check network connectivity to Impartus servers

### Issue: WebSocket Connection Drops

**Symptoms:** Progress updates stop mid-download

**Resolution:**
1. Check token expiry (24-hour limit)
2. Re-authenticate and reconnect
3. Verify network stability
4. Check server logs at `api.log`

### Issue: Rate Limiting / IP Ban

**Symptoms:** 429 Too Many Requests or connection refused

**Resolution:**
1. Reduce rate limits in config:
```json
{
  "rateLimit": 5,
  "apiRateLimit": 1,
  "enableJitter": true
}
```
2. Wait 10-15 minutes before retrying
3. Contact Impartus support if persistent

---

## Incident Response Procedures

### P1: Complete Service Outage

**Impact:** API server completely unresponsive

**Steps:**
1. Check process status: `ps aux | grep impartus`
2. Check port availability: `netstat -tlnp | grep 8080`
3. Review logs: `tail -100 api.log`
4. Restart service:
   ```bash
   pkill impartus
   ./impartus serve --port 8080
   ```
5. Verify health: `curl http://localhost:8080/api/v1/health`

### P2: Download Failures

**Impact:** Multiple users unable to download

**Steps:**
1. Check Impartus service status
2. Review error patterns in logs
3. Test single download manually
4. Apply rate limit adjustments
5. Notify users of workaround

### P3: Performance Degradation

**Impact:** Slow downloads, high latency

**Steps:**
1. Check system resources: `top`, `free -h`, `df -h`
2. Review concurrent job count
3. Adjust worker pool settings
4. Clear temp directory: `rm -rf ./.temp/*`

---

## Rollback Procedures

### Binary Rollback

```bash
# Keep previous version backup
cp impartus impartus.backup

# Rollback to previous version
cp impartus.backup impartus
chmod +x impartus
```

### Config Rollback

```bash
# Restore previous config
cp config.json config.json.backup
git checkout HEAD~1 -- config.json
```

### Database/State Recovery

The application is stateless (in-memory job store). Restart clears all job state.

---

## Monitoring & Alerts

### Observability Stack

This application supports the following observability infrastructure:

| Component | Tool | Purpose |
|-----------|------|---------|
| **Metrics** | OpenTelemetry | Performance counters, download stats |
| **Tracing** | X-Request-ID | Request correlation across handlers |
| **Error Tracking** | Sentry | Error aggregation and stack traces |
| **Alerting** | Configurable webhooks | Incident notifications |

### Deployment Observability

**Where to check deploy impact:**

1. **Health Endpoint**: `GET /api/v1/health` - Verify service is running
2. **API Logs**: `api.log` - Check for errors after deployment
3. **CI/CD Status**: GitHub Actions workflow runs - Verify build/test passed
4. **Coverage Reports**: GitHub Actions artifacts - Check coverage didn't drop

**Post-deployment verification:**
```bash
# Verify service health
curl -s http://localhost:8080/api/v1/health | jq

# Check recent logs for errors
tail -50 api.log | grep -i error

# Verify download functionality
./impartus courses --json | jq
```

**Deployment annotations**: Add deployment markers to logs:
```bash
# Mark deployment in logs
echo "[$(date -Iseconds)] DEPLOYMENT: version=$(./impartus version)" >> api.log
```

### Alerting Configuration

Alerts can be configured via webhook integration. Set environment variable:

```bash
# Alert webhook URL (Slack, PagerDuty, custom)
ALERT_WEBHOOK_URL=https://hooks.slack.com/services/...

# Alert on critical errors
ALERT_ON_ERRORS=true

# Alert threshold (errors per minute)
ALERT_THRESHOLD=10
```

**Alert types supported:**
- Service health failures
- Download failure rate exceeds threshold
- Authentication failures spike
- Memory usage exceeds limit

### Error Tracking (Sentry)

To enable Sentry error tracking:

1. Set environment variables:
```bash
SENTRY_DSN=https://xxx@xxx.ingest.sentry.io/xxx
SENTRY_ENVIRONMENT=production
```

2. Errors are automatically reported with:
   - Stack traces
   - Request context (X-Request-ID)
   - User context (session ID)
   - Custom tags (download type, lecture ID)

### Key Metrics to Monitor

| Metric | Description | Threshold |
|--------|-------------|-----------|
| Health check | API server response | Alert if down > 1 min |
| Download success rate | Completed/total downloads | Alert if < 90% |
| Average download time | Per-lecture duration | Alert if > 2x normal |
| Memory usage | Process RAM | Alert if > 1GB |
| Error rate | API errors/hour | Alert if > 10/hour |
| Active connections | WebSocket clients | Monitor for anomalies |

**Metrics exposed via OpenTelemetry:**
- `impartus_downloads_total` - Total download count
- `impartus_download_duration_seconds` - Download duration histogram
- `impartus_download_errors_total` - Error count by type
- `impartus_api_requests_total` - API request count by endpoint
- `impartus_active_jobs` - Currently active download jobs

### Log Analysis

```bash
# Count errors by type
grep -i error api.log | cut -d: -f3 | sort | uniq -c

# Recent WebSocket errors
grep "WebSocket" api.log | tail -20

# Failed downloads
grep -i "failed" api.log | tail -50
```

---

## Contact & Escalation

| Role | Contact | Escalation Time |
|------|---------|-----------------|
| Maintainer | @rabesss | Immediate |
| Impartus Support | Institution IT | 1-2 hours |

---

## Related Documentation

- [API Reference](./api-reference.md)
- [Architecture](./architecture.md)
- [Error Codes](./error-codes.md)
- [WebSocket Events](./websocket-events.md)
