# Performance regression checks

These checks run in the existing CI jobs. There is no separate performance
workflow, and timings never fail CI.

## Guarded behavior

- `internal/downloader/decrypt_alloc_test.go` decrypts a 1 MiB chunk through
  the decryption step of both download paths. It fails if decryption allocates
  a copy of the chunk on each call. The in-memory pipeline path decrypts in
  place, so it must allocate under 128 KiB per chunk;
  measured, it allocates about 1.3 KiB. The file-backed path may allocate one
  read buffer plus that slack. The guards run in the `Test` job and skip `-race`
  builds.
- `TestDownloadPlaylistDefaultPipelineBoundsRequestsAndPreservesOrder` in
  `internal/downloader/download_speed_test.go` requires a two-view playlist to
  request its key once and each chunk once. The first three chunk requests must
  overlap, and chunks must come back in order.
- `ui/scripts/benchmark.ts` presses the down arrow 200 times through 5,000
  courses in compact, medium and wide layouts. It fails if any press doesn't
  move and render the selection, or if the activity case doesn't render its
  activity dock. The OpenTUI workflow runs it on Ubuntu and logs the frame
  timings.

Wall-clock speed, real network throughput, allocations while a chunk downloads,
FFmpeg and playback are not guarded.

## Run locally

```sh
go test -run '^$' -bench 'DecryptChunk|DownloadPlaylistLatency' -benchmem ./internal/downloader/
cd ui && bun run benchmark
```

To change an allocation ceiling, edit the constant in the test and put the
before and after measurements in the PR description.
