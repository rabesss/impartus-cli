<!-- START doctoc generated TOC please keep comment here to allow auto update -->
**Table of Contents**  *generated automatically*

<!---toc start-->

* [Runbooks - Incident Response Playbooks](#runbooks---incident-response-playbooks)
  * [Service Health Checks](#service-health-checks)
    * [Check API Server Status](#check-api-server-status)
    * [Check Download Jobs](#check-download-jobs)
  * [Common Issues & Troubleshooting](#common-issues--troubleshooting)
    * [Issue: "ffmpeg not found" Error](#issue-ffmpeg-not-found-error)
    * [Issue: Authentication Failed (401)](#issue-authentication-failed-401)
    * [Issue: Download Timeout](#issue-download-timeout)
    * [Issue: WebSocket Connection Drops](#issue-websocket-connection-drops)
    * [Issue: Rate Limiting / IP Ban](#issue-rate-limiting--ip-ban)
  * [Incident Response Procedures](#incident-response-procedures)
    * [P1: Complete Service Outage](#p1-complete-service-outage)
    * [P2: Download Failures](#p2-download-failures)
    * [P3: Performance Degradation](#p3-performance-degradation)
  * [Rollback Procedures](#rollback-procedures)
    * [Binary Rollback](#binary-rollback)
    * [Config Rollback](#config-rollback)
    * [Database/State Recovery](#databasestate-recovery)
  * [Monitoring & Alerts](#monitoring--alerts)
    * [Operational Signals](#operational-signals)
    * [Deployment Verification](#deployment-verification)
    * [Log Analysis](#log-analysis)
  * [Contact & Escalation](#contact--escalation)
  * [Related Documentation](#related-documentation)

<!---toc end-->
<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Runbooks - Incident Response Playbooks

This document provides operational runbooks for common incidents and troubleshooting scenarios.

---

## Service Health Checks

### Check API Server Liveness

```bash
curl -s http://localhost:8080/api/v1/health/live
```

Use the liveness path for process and container liveness checks. It performs no dependency checks.

### Check Dependency Readiness

```bash
curl -s http://localhost:8080/api/v1/health/ready
```

Expected response (structured envelope with sub-checks):
```json
{
  "success": true,
  "data": {
    "status": "ok",
    "config": {
      "status": "ok"
    },
    "upstream": {
      "status": "reachable"
    },
    "ffmpeg": {
      "status": "available"
    }
  },
  "error": null,
  "meta": {
    "command": "health",
    "mode": "api"
  }
}
```

If any component is not configured or unavailable, the overall `status` becomes `degraded` and the relevant sub-check shows `not_configured`, `unreachable`, or `not_found`.
The unauthenticated response intentionally reports only aggregate configuration status; inspect the local configuration or server logs to diagnose a `misconfigured` result.
Readiness results, including degraded results, are cached for 15 seconds and may be that old. `/api/v1/health` remains a compatibility alias for readiness. Both readiness paths return HTTP 200 when degraded, so automation must inspect `data.status`.

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
5. If you see `RATE_LIMITED` (429), wait for `retryAfter` seconds (default 60) before retrying login

**Note:** The API server caches upstream Impartus login tokens for ~23 hours. Stale-cache issues after credential rotation are rare; restarting `impartus serve` clears the cache.

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
5. Verify liveness: `curl http://localhost:8080/api/v1/health/live`
6. Verify dependency readiness: `curl http://localhost:8080/api/v1/health/ready`

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
4. Inspect the configured temp directory for abandoned workspaces. Each active
   download owns and removes a unique child workspace automatically, so routine
   cleanup is not required. Never delete the base temp directory or its children
   while downloads are active. Manual removal should be reserved for emergency
   recovery after confirming no download process is running.

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

Jobs are persisted to `.jobs.json` on disk. On restart:
- Completed, failed, and canceled jobs are restored with their preserved state
- Running/pending jobs are marked as failed (non-resumable) since downloads cannot continue after server restart
- Metadata for the newest 1000 terminal jobs is retained; active jobs are not pruned while running, but interrupted jobs become failed before restart retention is applied
- Retention affects metadata only and never deletes downloaded media
- Graceful shutdown flushes pending coalesced persistence writes before the store closes

`DELETE /jobs/{id}` cancels an active job. It is not a job-history deletion endpoint.

---

## Monitoring & Alerts

### Operational Signals

Current operational visibility is intentionally simple and built from live features:

| Signal | Source | Purpose |
|--------|--------|---------|
| Liveness endpoint | `GET /api/v1/health/live` | Verify that the API process can serve requests without probing dependencies |
| Readiness endpoint | `GET /api/v1/health/ready` | Verify cached config, upstream reachability, and FFmpeg status |
| Request correlation | `X-Request-ID` header | Trace a request across handler logs |
| API logs | `api.log` | Inspect server/runtime failures after deploys or incidents |
| CI/CD status | GitHub Actions | Confirm build/test state before and after rollouts |
| Coverage reports | GitHub Actions artifacts | Watch for regressions in exercised code paths |

There is no built-in metrics export or webhook alerting in the current codebase. Sentry integration is configured via `.github/workflows/sentry-issues.yml` for daily issue sync and error metrics reporting.

### Deployment Verification

```bash
# Verify process liveness
curl -s http://localhost:8080/api/v1/health/live | jq

# Verify dependency readiness (inspect data.status; HTTP remains 200 when degraded)
curl -s http://localhost:8080/api/v1/health/ready | jq

# Check recent logs for errors
tail -50 api.log | grep -i error

# Verify CLI access against the configured upstream
./impartus courses --json | jq
```

Expected health response shape:

```json
{
  "success": true,
  "data": {
    "status": "ok",
    "config": {
      "status": "ok"
    },
    "upstream": {
      "status": "reachable"
    },
    "ffmpeg": {
      "status": "available"
    }
  },
  "error": null,
  "meta": {
    "command": "health",
    "mode": "api"
  }
}
```

If any sub-check fails, the top-level `data.status` becomes `degraded`.
Configuration health remains aggregate because this endpoint is unauthenticated; use local configuration and logs for field-level diagnosis.

### Log Analysis

```bash
# Count recent error-like log lines
grep -i error api.log | tail -20

# Inspect recent failures
grep -i "failed" api.log | tail -20

# Fall back to the raw tail when a pattern is not obvious
tail -50 api.log
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
