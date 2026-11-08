#!/usr/bin/env bash
# Smoke-test a built impartus image that is already in the local Docker daemon:
# the CLI entrypoint, then each ffmpeg path internal/downloader/ffmpeg.go uses
# (HLS remux, two-view join, audio extraction), decoding every output.
#
# Usage: scripts/container-smoke.sh <image-ref> <platform>
#
# The Packages workflow also runs this against historical release tags. Older
# images run as a non-root user without a writable work directory, so ffmpeg runs in
# a world-writable tmpfs instead.
set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: $0 <image-ref> <platform>" >&2
	exit 2
fi
image=$1
platform=$2

echo "==> impartus --help ($platform)"
help_output=$(docker run --rm --platform "$platform" "$image" --help)
if ! grep -q '^Usage:' <<<"$help_output"; then
	echo "impartus --help did not print usage:" >&2
	echo "$help_output" >&2
	exit 1
fi

# Runs inside the image as its default user. Arguments mirror ffmpeg.go.
read -r -d '' ffmpeg_checks <<'EOF' || true
set -eu
ff() { ffmpeg -nostdin -hide_banner -loglevel error -y "$@"; }

echo "--> encode HLS source (libx264 + aac)"
ff -f lavfi -i testsrc=duration=2:size=320x240:rate=25 \
	-f lavfi -i sine=frequency=440:duration=2 \
	-c:v libx264 -pix_fmt yuv420p -c:a aac -shortest \
	-f hls -hls_time 1 -hls_playlist_type vod \
	-hls_segment_filename 'chunk%03d.ts' source.m3u8

echo "--> JoinChunksFromM3U8: remux playlist to mp4"
ff -i source.m3u8 -c copy -f mp4 joined.mp4.part
mv joined.mp4.part joined.mp4

echo "--> JoinViews: join two views into matroska"
ff -i joined.mp4 -i joined.mp4 -map 0 -map 1 -c copy -f matroska views.mkv.part
mv views.mkv.part views.mkv

echo "--> extract audio: mp3, m4a, opus"
ff -i source.m3u8 -vn -acodec libmp3lame -ab 192k -f mp3 audio.mp3
ff -i source.m3u8 -vn -acodec aac -ab 192k -f ipod audio.m4a
ff -i source.m3u8 -vn -acodec libopus -ab 192k -f opus audio.opus

for output in joined.mp4 views.mkv audio.mp3 audio.m4a audio.opus; do
	echo "--> decode $output"
	ffmpeg -nostdin -hide_banner -v error -xerror -i "$output" -f null -
done

video=$(ffprobe -v error -select_streams v -show_entries stream=index -of csv=p=0 views.mkv | wc -l)
audio=$(ffprobe -v error -select_streams a -show_entries stream=index -of csv=p=0 views.mkv | wc -l)
if [ "$video" -ne 2 ] || [ "$audio" -ne 2 ]; then
	echo "views.mkv has $video video and $audio audio streams, want 2 and 2" >&2
	exit 1
fi
EOF

echo "==> ffmpeg encode/remux/decode ($platform)"
docker run --rm --platform "$platform" \
	--tmpfs /smoke:rw,exec,mode=1777 --workdir /smoke \
	--entrypoint sh "$image" -c "$ffmpeg_checks"

echo "==> container smoke test passed ($platform)"
