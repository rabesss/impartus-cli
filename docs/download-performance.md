# Download performance tuning

Impartus CLI downloads and decrypts HLS chunks through a bounded pipeline. The
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

| Situation | Download workers | Decrypt workers | Rate limit |
| --- | ---: | ---: | ---: |
| Stable connection; maximize throughput | 12 | 4 | 100 |
| Shared Wi-Fi or intermittent upstream | 6 | 2 | 10 |
| Troubleshooting repeated upstream failures | 3 | 2 | 5-10 |

Keep `enablePipeline` set to `true` for all three profiles. Setting it to
`false` selects the legacy serial path, so the worker settings have no effect
and downloads are usually substantially slower.

`numWorkers` controls how many lectures may be processed concurrently. The
per-lecture download and decrypt workers above control media chunks within each
lecture. When downloading several lectures at once, lower `numWorkers` before
raising per-lecture worker counts.

## Tuning safely

1. Test one representative lecture at a time.
2. Change only one setting between runs.
3. Compare completed downloads, not partial transfer speed.
4. Validate the output with `ffprobe` or by playing it before keeping a new
   profile.
5. Never commit `config.json`; it contains credentials.

If errors mention HTTP status `516`, `ETIMEDOUT`, or repeated chunk retries,
the Impartus media proxy is timing out upstream. Wait and retry with the
balanced profile. More local workers cannot make an unavailable upstream
server faster and can make that failure mode more frequent.

The pipeline preserves chunk order, bounds in-flight memory, removes temporary
encrypted chunks, and supports cooperative cancellation. If a pipeline-enabled
download still fails consistently at the balanced or troubleshooting profile,
capture the sanitized error and open an issue rather than permanently falling
back to the serial path.
