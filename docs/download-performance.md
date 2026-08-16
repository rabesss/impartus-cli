# Download performance tuning

The `impartus download` command processes selected lectures one at a time, but
downloads and decrypts each lecture's HLS chunks through a bounded pipeline.
API jobs can process several lectures concurrently as described below. The
shipped defaults favor maximum throughput on stable connections:

```json
{
  "enablePipeline": true,
  "downloadWorkersPerLecture": 12,
  "decryptWorkersPerLecture": 4,
  "rateLimit": 100
}
```

Start with those defaults. If the upstream media proxy becomes unreliable, use
the balanced profile below before disabling the pipeline:

```json
{
  "enablePipeline": true,
  "downloadWorkersPerLecture": 6,
  "decryptWorkersPerLecture": 2,
  "rateLimit": 10
}
```

The balanced profile keeps concurrent download and decryption enabled while
reducing pressure on the upstream server. On one real Impartus connection it
captured most of the available throughput with lower memory use and fewer
requests in flight. That measurement is connection-specific, so it is guidance
rather than a universal fastest setting.

## Choosing a profile

| Situation | Download workers | Decrypt workers | Rate limit (requests/s) |
| --- | ---: | ---: | ---: |
| Stable connection; maximize throughput | 12 | 4 | 100 |
| Shared Wi-Fi or intermittent upstream | 6 | 2 | 10 |
| Troubleshooting repeated upstream failures | 3 | 2 | 5 |

`rateLimit` is the sustained number of media requests permitted per second; the
token bucket can allow a temporary burst. Start the troubleshooting profile at
5 requests per second, then tune within 5–10 only after successful downloads.

Keep `enablePipeline` set to `true` for all three profiles. Setting it to
`false` selects the legacy path and disables the per-chunk download and decrypt
worker pools. `decryptWorkersPerLecture` then has no effect, and
`downloadWorkersPerLecture` no longer controls chunk concurrency. API jobs
still use `downloadWorkersPerLecture` in their playlist-slot safety cap:
`min(numWorkers, max(1, 24 / downloadWorkersPerLecture))`. It can therefore
limit concurrent API lecture processing even when the pipeline is disabled.

The `impartus download` loop processes lectures serially. For API jobs,
`numWorkers` controls how many lectures may be processed concurrently, subject
to the playlist-slot cap above. The per-lecture download and decrypt workers
control media chunks within each lecture only when the pipeline is enabled.
When an API job downloads several lectures at once, lower `numWorkers` before
raising per-lecture worker counts.

## Tuning safely

1. Test one representative lecture at a time.
2. Change only one setting between runs.
3. Compare completed downloads, not partial transfer speed.
4. Validate the output with `ffprobe` or by playing it before keeping a new
   profile.
5. Never commit `config.json`; it contains credentials.

Repeated chunk retries or an HTTP status such as `516` shows that requests are
failing, but does not by itself prove an upstream timeout. Explicit Go timeout
diagnostics include `context deadline exceeded`, `dial tcp ...: i/o timeout`,
`net/http: TLS handshake timeout`, and `net/http: request canceled
(Client.Timeout exceeded while awaiting headers)`. For transient failures, wait
and retry with the balanced profile. More local workers cannot make an
unavailable upstream server faster and can make failures more frequent.

The pipeline preserves chunk order, bounds in-flight memory, removes temporary
encrypted chunks, and supports cooperative cancellation. If a pipeline-enabled
download still fails consistently at the balanced or troubleshooting profile,
capture the sanitized error and open an issue rather than permanently falling
back to the serial path.
