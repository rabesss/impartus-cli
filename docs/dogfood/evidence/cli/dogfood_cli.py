#!/usr/bin/env python3
"""Exercise impartus CLI interactions and record structured results.

Does not print secret values. Redacts known secret strings from captured output.
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
from pathlib import Path
from typing import Any

BINARY = Path("/workspace/impartus")
OUT_DIR = Path("/opt/cursor/artifacts/cli-dogfood")
RUNS_DIR = OUT_DIR / "runs"
SUMMARY_PATH = OUT_DIR / "summary.json"

SECRETS = [
    os.environ.get("IMPARTUS_PASSWORD", ""),
    os.environ.get("IMPARTUS_USERNAME", ""),
    os.environ.get("IMPARTUS_BASE_URL", ""),
]
SECRETS = [s for s in SECRETS if s]


def redact(text: str) -> str:
    out = text
    for secret in SECRETS:
        if secret:
            out = out.replace(secret, "[REDACTED]")
    out = re.sub(r"https?://[^\s\"']+", "[REDACTED_URL]", out)
    return out


def run_case(name: str, args: list[str], timeout: float = 20.0, env: dict[str, str] | None = None) -> dict[str, Any]:
    cmd_env = os.environ.copy()
    if env:
        cmd_env.update(env)
    # Isolate token cache and library state from the workspace unless a case opts in.
    cmd_env.setdefault("XDG_STATE_HOME", str(OUT_DIR / "state"))
    cmd_env.setdefault("IMPARTUS_TOKEN_CACHE", str(OUT_DIR / "token-cache"))
    proc = subprocess.run(
        [str(BINARY), *args],
        capture_output=True,
        text=True,
        timeout=timeout,
        env=cmd_env,
        cwd="/workspace",
    )
    stdout = redact(proc.stdout)
    stderr = redact(proc.stderr)
    result = {
        "name": name,
        "args": args,
        "exit": proc.returncode,
        "stdout": stdout,
        "stderr": stderr,
        "stdout_len": len(proc.stdout),
        "stderr_len": len(proc.stderr),
        "stdout_is_json": is_json(proc.stdout),
        "stderr_is_json": is_json(proc.stderr),
        "stdout_json": try_json(proc.stdout),
        "stderr_json": try_json(proc.stderr),
    }
    run_path = RUNS_DIR / f"{name}.json"
    run_path.write_text(json.dumps(redact_obj(result), indent=2) + "\n")
    return result


def is_json(text: str) -> bool:
    text = text.strip()
    if not text:
        return False
    try:
        json.loads(text)
        return True
    except json.JSONDecodeError:
        return False


def try_json(text: str) -> Any:
    text = text.strip()
    if not text:
        return None
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return None


def redact_obj(obj: Any) -> Any:
    dumped = json.dumps(obj)
    return json.loads(redact(dumped))


def envelope_ok(payload: Any, command: str | None = None, success: bool = True) -> list[str]:
    problems: list[str] = []
    if not isinstance(payload, dict):
        return ["not an object"]
    for key in ("success", "data", "error", "meta"):
        if key not in payload:
            problems.append(f"missing {key}")
    if payload.get("success") is not success:
        problems.append(f"success={payload.get('success')} want {success}")
    meta = payload.get("meta") or {}
    if meta.get("mode") != "json":
        problems.append(f"meta.mode={meta.get('mode')}")
    if command is not None and meta.get("command") != command:
        problems.append(f"meta.command={meta.get('command')} want {command}")
    if success:
        if payload.get("error") is not None:
            problems.append("error should be null")
    else:
        if payload.get("data") not in (None, payload.get("data")) and payload.get("data") is not None:
            # doctor/watch may attach data on failure
            pass
        err = payload.get("error")
        if not isinstance(err, dict) or not err.get("message"):
            problems.append("error.message missing")
    return problems


CASES: list[tuple[str, list[str]]] = [
    ("no_args", []),
    ("help", ["help"]),
    ("help_flag", ["--help"]),
    ("help_short", ["-h"]),
    ("help_download_subcommand_style", ["help", "download"]),
    ("help_then_download", ["--help", "download"]),
    ("download_help", ["download", "--help"]),
    ("download_help_short", ["download", "-h"]),
    ("download_help_json", ["download", "--help", "--json"]),
    ("download_json_help", ["download", "--json", "--help"]),
    ("download_help_invalid_flag", ["download", "--start", "bad", "--help"]),
    ("courses_help", ["courses", "--help"]),
    ("lectures_help", ["lectures", "--help"]),
    ("play_help", ["play", "--help"]),
    ("doctor_help", ["doctor", "--help"]),
    ("library_help", ["library", "--help"]),
    ("library_list_help", ["library", "list", "--help"]),
    ("library_show_help", ["library", "show", "--help"]),
    ("library_verify_help", ["library", "verify", "--help"]),
    ("watch_help", ["watch", "--help"]),
    ("serve_help", ["serve", "--help"]),
    ("tui_help", ["tui", "--help"]),
    ("version_help", ["version", "--help"]),
    ("version", ["version"]),
    ("version_json", ["version", "--json"]),
    ("json_version", ["--json", "version"]),
    ("dash_v", ["-v"]),
    ("dash_dash_version", ["--version"]),
    ("root_json", ["--json"]),
    ("root_json_help", ["--json", "--help"]),
    ("help_json", ["help", "--json"]),
    ("unknown", ["bogus"]),
    ("unknown_help", ["bogus", "--help"]),
    ("unknown_help_json", ["bogus", "--help", "--json"]),
    ("unknown_json", ["bogus", "--json"]),
    ("json_equals_true", ["--json=true"]),
    ("courses_sentinel_help", ["courses", "--", "--help"]),
    ("download_json_after_sentinel", ["download", "--subject", "1", "--session", "2", "--", "--json"]),
    ("download_missing", ["download"]),
    ("download_missing_json", ["download", "--json"]),
    ("download_unknown_flag", ["download", "--subject", "1", "--session", "2", "--nope"]),
    ("download_bad_quality", ["download", "-s", "1", "-S", "2", "--quality", "1080"]),
    ("download_json_and_events", ["download", "--json", "--events", "-s", "1", "-S", "2"]),
    ("download_ttid_conflict", ["download", "-s", "1", "-S", "2", "--ttid", "3", "--start", "1"]),
    ("lectures_missing", ["lectures"]),
    ("lectures_missing_json", ["lectures", "--json"]),
    ("play_json", ["play", "--json"]),
    ("tui_json", ["tui", "--json"]),
    ("tui_no_tty", ["tui"]),
    ("doctor", ["doctor"]),
    ("doctor_json", ["doctor", "--json"]),
    ("doctor_extra", ["doctor", "extra"]),
    ("library_no_subcommand", ["library"]),
    ("library_list", ["library", "list"]),
    ("library_list_json", ["library", "list", "--json"]),
    ("library_unknown", ["library", "vrfy"]),
    ("library_unknown_help", ["library", "vrfy", "--help"]),
    ("watch_no_targets", ["watch", "--once", "--dry-run"]),
    ("watch_no_targets_json", ["watch", "--once", "--dry-run", "--json"]),
    ("watch_incomplete", ["watch", "--subject", "1"]),
    ("watch_json_events", ["watch", "--json", "--events", "-s", "1", "-S", "2"]),
    ("serve_json", ["serve", "--json"]),
    ("serve_bad_port", ["serve", "--port", "0"]),
    ("serve_bad_port_json", ["serve", "--port", "0", "--json"]),
    ("play_no_mpv_direct", ["play", "-s", "1", "-S", "2", "--lecture", "1"]),
    ("version_positional", ["version", "extra"]),
    ("courses_positional", ["courses", "extra"]),
]


AUTH_CASES: list[tuple[str, list[str]]] = [
    ("courses_human", ["courses"]),
    ("courses_json", ["courses", "--json"]),
]


def main() -> int:
    RUNS_DIR.mkdir(parents=True, exist_ok=True)
    (OUT_DIR / "state").mkdir(parents=True, exist_ok=True)
    if not BINARY.exists():
        print("missing binary", file=sys.stderr)
        return 2

    results = []
    for name, args in CASES:
        try:
            results.append(run_case(name, args))
        except subprocess.TimeoutExpired:
            results.append({"name": name, "args": args, "exit": -1, "error": "timeout"})

    # Authenticated catalog. Keep timeout bounded.
    for name, args in AUTH_CASES:
        try:
            results.append(run_case(name, args, timeout=60.0))
        except subprocess.TimeoutExpired:
            results.append({"name": name, "args": args, "exit": -1, "error": "timeout"})

    # If courses JSON succeeded, pick the first course and list lectures.
    courses_json = next((r for r in results if r.get("name") == "courses_json"), None)
    subject = session = None
    if courses_json and courses_json.get("exit") == 0:
        payload = courses_json.get("stdout_json") or {}
        data = payload.get("data") if isinstance(payload, dict) else None
        if isinstance(data, list) and data:
            first = data[0]
            subject = first.get("subjectId") or first.get("SubjectId")
            session = first.get("sessionId") or first.get("SessionId")
            # Persist a redacted course shape sample without dumping the catalog.
            sample = {k: first.get(k) for k in list(first)[:12]} if isinstance(first, dict) else first
            (OUT_DIR / "course_sample.json").write_text(redact(json.dumps(sample, indent=2)) + "\n")

    if subject and session:
        lecture_cases = [
            ("lectures_json", ["lectures", "-s", str(subject), "-S", str(session), "--json"]),
            ("lectures_human", ["lectures", "-s", str(subject), "-S", str(session)]),
            ("watch_dry_run_json", ["watch", "--once", "--dry-run", "--json", "-s", str(subject), "-S", str(session)]),
            ("watch_dry_run_events", ["watch", "--once", "--dry-run", "--events", "-s", str(subject), "-S", str(session)]),
            ("download_range_invalid", ["download", "-s", str(subject), "-S", str(session), "--start", "0", "--json"]),
        ]
        for name, args in lecture_cases:
            try:
                results.append(run_case(name, args, timeout=90.0))
            except subprocess.TimeoutExpired:
                results.append({"name": name, "args": args, "exit": -1, "error": "timeout"})

        lectures_json = next((r for r in results if r.get("name") == "lectures_json"), None)
        ttid = None
        if lectures_json and lectures_json.get("exit") == 0:
            payload = lectures_json.get("stdout_json") or {}
            data = payload.get("data") if isinstance(payload, dict) else None
            if isinstance(data, list) and data:
                first = data[0]
                ttid = first.get("ttid") or first.get("ttId") or first.get("TTID")
                sample = {k: first.get(k) for k in list(first)[:16]} if isinstance(first, dict) else first
                (OUT_DIR / "lecture_sample.json").write_text(redact(json.dumps(sample, indent=2)) + "\n")
                (OUT_DIR / "catalog_counts.json").write_text(
                    json.dumps({"courses": "see courses_json", "lecture_count": len(data), "subject": subject, "session": session}, indent=2)
                    + "\n"
                )
        if ttid:
            try:
                results.append(
                    run_case(
                        "download_ttid_audio_json",
                        [
                            "download",
                            "-s",
                            str(subject),
                            "-S",
                            str(session),
                            "--ttid",
                            str(ttid),
                            "--audio-only",
                            "--format",
                            "mp3",
                            "--quality",
                            "144",
                            "--views",
                            "left",
                            "--json",
                            "-o",
                            str(OUT_DIR / "downloads"),
                        ],
                        timeout=180.0,
                    )
                )
            except subprocess.TimeoutExpired:
                results.append({"name": "download_ttid_audio_json", "exit": -1, "error": "timeout"})

    compact = []
    for r in results:
        compact.append(
            {
                "name": r.get("name"),
                "args": r.get("args"),
                "exit": r.get("exit"),
                "stdout_preview": (r.get("stdout") or "")[:400],
                "stderr_preview": (r.get("stderr") or "")[:400],
                "stdout_is_json": r.get("stdout_is_json"),
                "stderr_is_json": r.get("stderr_is_json"),
                "stdout_json_meta": (r.get("stdout_json") or {}).get("meta") if isinstance(r.get("stdout_json"), dict) else None,
                "stderr_json_meta": (r.get("stderr_json") or {}).get("meta") if isinstance(r.get("stderr_json"), dict) else None,
                "error": r.get("error"),
            }
        )
    SUMMARY_PATH.write_text(json.dumps(compact, indent=2) + "\n")
    print(f"wrote {len(results)} cases to {SUMMARY_PATH}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
