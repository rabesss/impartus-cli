#!/usr/bin/env python3
"""Compare fixed, verified work using one harness and policy for both commits."""
from __future__ import annotations

import argparse
import hashlib
import io
import json
import math
import os
from pathlib import Path
import platform
import re
import signal
import statistics
import subprocess
import sys
import tarfile
import tempfile

CORE = ("scripts/performance/run.py", "scripts/performance/budgets.json", "internal/downloader/performance_benchmark_test.go")
HARNESS = (*CORE, "ui/scripts/benchmark.ts")
SIZES = (65536, 1048576)
UI_CASES = {"medium-catalog": (80, 24), "wide-catalog": (140, 32), "wide-activity": (140, 32)}
DOWNLOAD = {"schemaVersion": 1, "scenario": "download-work-v1", "keyRequests": 1, "chunkRequests": 12,
            "maxConcurrentRequests": 3, "chunkBodyBytes": 960, "plaintextBytes": 768,
            "plaintextSha256": "2c75f98fc42eca1d24a0687835a0df3d9f3c3add922f6077b7982ff41caa4194", "encryptedTempFiles": 0}


def strict_json(text):
    def pairs(items):
        result = {}
        for key, value in items:
            if key in result:
                raise ValueError(f"duplicate JSON key: {key}")
            result[key] = value
        return result
    def bad_constant(value):
        raise ValueError(f"invalid JSON number: {value}")
    if len(text.encode()) > 1_000_000:
        raise ValueError("measurement exceeds 1 MB")
    return json.loads(text, object_pairs_hook=pairs, parse_constant=bad_constant)


def number(value, label, integer=False):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value < 0:
        raise ValueError(f"invalid metric: {label}")
    if integer and int(value) != value:
        raise ValueError(f"noninteger metric: {label}")
    return value


def parse_go(text):
    results = {}
    for line in text.splitlines():
        if not line.startswith("BenchmarkPerformanceDecrypt/"):
            continue
        parts = line.split()
        match = re.fullmatch(r"BenchmarkPerformanceDecrypt/bytes-(\d+)-\d+", parts[0])
        if not match or len(parts) != 12 or parts[1] != "10":
            raise ValueError("unexpected decryption case or iteration count")
        size = int(match[1])
        if size not in SIZES or size in results:
            raise ValueError("unknown or duplicate decryption case")
        values = {parts[i + 1]: number(float(parts[i]), parts[i + 1]) for i in range(2, len(parts), 2)}
        if set(values) != {"ns/op", "MB/s", "B/op", "allocs/op", "plaintext-bytes/op"} or values["plaintext-bytes/op"] != size:
            raise ValueError("decryption omitted or changed work")
        results[size] = values
    if set(results) != set(SIZES):
        raise ValueError("missing decryption cases")
    return results


def validate_download(value):
    if not isinstance(value, dict) or set(value) != set(DOWNLOAD):
        raise ValueError("invalid download result schema")
    for key, expected in DOWNLOAD.items():
        if isinstance(expected, int):
            number(value[key], key, integer=True)
        if value[key] != expected:
            raise ValueError(f"download work changed: {key}")
    return value


def validate_ui(value):
    if not isinstance(value, dict) or set(value) != {"schemaVersion", "scenario", "courseCount", "iterations", "results"}:
        raise ValueError("invalid UI result schema")
    for key, expected in {"schemaVersion": 1, "courseCount": 5000, "iterations": 200}.items():
        number(value[key], key, integer=True)
        if value[key] != expected:
            raise ValueError(f"UI fixture changed: {key}")
    if value["scenario"] != "ui-navigation-v1" or not isinstance(value["results"], list) or len(value["results"]) != 3:
        raise ValueError("UI fixture or case count changed")
    results = {}
    metrics = {"coldFrameMilliseconds", "p50InputToFrameMilliseconds", "p95InputToFrameMilliseconds", "p99InputToFrameMilliseconds", "residentSetBytes"}
    for case in value["results"]:
        if not isinstance(case, dict) or set(case) != metrics | {"label", "width", "height", "selectedCourseIndex", "finalFrameSha256"}:
            raise ValueError("invalid UI case schema")
        label = case["label"]
        if not isinstance(label, str) or label not in UI_CASES or label in results:
            raise ValueError("unknown or duplicate UI case")
        for key in metrics | {"width", "height", "selectedCourseIndex"}:
            number(case[key], key, integer=key in {"width", "height", "selectedCourseIndex", "residentSetBytes"})
        if (case["width"], case["height"]) != UI_CASES[label] or case["selectedCourseIndex"] != 200:
            raise ValueError("UI omitted or changed navigation work")
        if not isinstance(case["finalFrameSha256"], str) or not re.fullmatch(r"[0-9a-f]{64}", case["finalFrameSha256"]):
            raise ValueError("invalid UI frame fingerprint")
        if not case["p50InputToFrameMilliseconds"] <= case["p95InputToFrameMilliseconds"] <= case["p99InputToFrameMilliseconds"]:
            raise ValueError("invalid UI percentiles")
        results[label] = case
    return results


def clean_environment():
    keys = {"PATH", "HOME", "LANG", "LC_ALL", "GOPATH", "GOMODCACHE", "GOCACHE", "GOTOOLCHAIN", "TMPDIR", "BUN_INSTALL_CACHE_DIR", "SSL_CERT_FILE", "SSL_CERT_DIR"}
    env = {key: value for key, value in os.environ.items() if key in keys}
    env.update(GOMAXPROCS="2", GOTOOLCHAIN="local", GOWORK="off", PYTHONDONTWRITEBYTECODE="1")
    return env


def command(args, cwd, env, timeout=300):
    proc = subprocess.Popen(args, cwd=cwd, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, start_new_session=True)
    try:
        out, err = proc.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        try:
            out, err = proc.communicate(timeout=1)
        except subprocess.TimeoutExpired as drain:
            # A detached child may retain the pipes after its parent was killed.
            out, err = drain.output or "", drain.stderr or ""
            proc.stdout.close()
            proc.stderr.close()
            proc.wait(timeout=1)
        if isinstance(out, bytes):
            out = out.decode(errors="replace")
        if isinstance(err, bytes):
            err = err.decode(errors="replace")
        raise RuntimeError(f"command timed out: {args[0]}\n{(out + err)[-4000:]}") from None
    if proc.returncode:
        raise RuntimeError(f"command failed ({proc.returncode}): {args[0]}\n{(out + err)[-4000:]}")
    return out


def resolve(source, sha, env):
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise ValueError("source references must be full commit SHAs")
    resolved = command(["git", "rev-parse", "--verify", sha + "^{commit}"], source, env, 30).strip()
    if resolved != sha:
        raise ValueError("source reference changed")
    return resolved


def archive(source, sha, target, env):
    data = subprocess.run(["git", "archive", sha], cwd=source, env=env, capture_output=True, check=True, timeout=60).stdout
    target.mkdir()
    with tarfile.open(fileobj=io.BytesIO(data)) as contents:
        contents.extractall(target, filter="data")


def harness_mode(present):
    if all(present):
        return "base"
    if not any(present):
        return "bootstrap"
    raise ValueError("partial baseline harness; bootstrap is not allowed")


def validate_policy(policy):
    keys = {"schemaVersion", "scenario", "goVersion", "bunVersion", "decryptionBytesPerOperationMax", "decryptionAllocationsPerOperationMax"}
    if not isinstance(policy, dict) or set(policy) != keys:
        raise ValueError("invalid budget policy schema")
    number(policy["schemaVersion"], "schemaVersion", integer=True)
    if policy["schemaVersion"] != 1 or policy["scenario"] != "impartus-performance-v1":
        raise ValueError("unknown budget scenario")
    for key in ("goVersion", "bunVersion"):
        if not isinstance(policy[key], str) or not re.fullmatch(r"\d+\.\d+\.\d+", policy[key]):
            raise ValueError(f"invalid pinned version: {key}")
    for key in ("decryptionBytesPerOperationMax", "decryptionAllocationsPerOperationMax"):
        if number(policy[key], key, integer=True) == 0:
            raise ValueError("budget must be positive")
    return policy


def configuration(args):
    """Resolve CI tool versions from immutable base policy before installing tools."""
    env, source = clean_environment(), args.source.resolve()
    base = resolve(source, args.base, env)
    candidate = resolve(source, args.candidate, env)
    present = [subprocess.run(["git", "cat-file", "-e", f"{base}:{path}"], cwd=source, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30).returncode == 0 for path in CORE]
    mode = harness_mode(present)
    trusted = base if mode == "base" else candidate
    policy = validate_policy(strict_json(command(["git", "show", f"{trusted}:{CORE[1]}"], source, env, 30)))
    with args.ci_config.open("a") as stream:
        stream.write(f"go-version={policy['goVersion']}\nbun-version={policy['bunVersion']}\n")
    return 0


def budget_violations(samples, policy):
    failures = []
    for sample in samples:
        for size, values in sample["decryption"].items():
            for metric, maximum in [("B/op", policy["decryptionBytesPerOperationMax"]), ("allocs/op", policy["decryptionAllocationsPerOperationMax"])]:
                if values[metric] > maximum:
                    failures.append(f"{size} bytes: {metric}={values[metric]:g} exceeds {maximum}")
    return failures


def markdown(report):
    lines = ["## Performance regression checks", "", f"Status: **{report['status']}**", "", "Timing and RSS are advisory; RSS is measured after each case, not peak memory."]
    for key in ("base", "candidate", "policyProvenance"):
        if key in report:
            lines.append(f"- {key}: `{report[key]}`")
    if report.get("harnessChanges"):
        lines.append("- This candidate changes measurement tooling/policy. Base tooling remains authoritative after bootstrap.")
    for role, samples in report.get("samples", {}).items():
        if not samples:
            continue
        lines += ["", f"### {role.capitalize()}", "", "| Plaintext bytes | Median B/op | Max allocs/op | Median ms/op |", "| ---: | ---: | ---: | ---: |"]
        for size in SIZES:
            values = [sample["decryption"][size] for sample in samples]
            lines.append(f"| {size} | {statistics.median(v['B/op'] for v in values):g} | {max(v['allocs/op'] for v in values):g} | {statistics.median(v['ns/op'] for v in values)/1e6:.3f} |")
        lines += ["", "| UI case | Median input p95 (ms) | Median RSS (MiB) |", "| --- | ---: | ---: |"]
        for label in UI_CASES:
            values = [sample["ui"][label] for sample in samples]
            lines.append(f"| {label} | {statistics.median(v['p95InputToFrameMilliseconds'] for v in values):.2f} | {statistics.median(v['residentSetBytes'] for v in values)/2**20:.1f} |")
        lines += ["", "Download oracle: 1 key request, 12 chunk requests, 3 concurrent requests, 960 chunk-body bytes, exact ordered plaintext, no encrypted temporary files."]
    if report.get("frameComparison") is False:
        lines += ["", "UI output fingerprints differ; UI timing comparison is unavailable."]
    for error in report.get("errors", []) + report.get("violations", []):
        lines += ["", "```text", error.replace("```", "'''")[:4000], "```"]
    return "\n".join(lines) + "\n"


def run(args):
    source, output = args.source.resolve(), args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    env = clean_environment()
    report = {"schemaVersion": 1, "scenario": "impartus-performance-v1", "status": "unavailable", "samples": {"base": [], "candidate": []}, "errors": []}
    exit_code = 2
    try:
        report["base"] = resolve(source, args.base, env)
        report["candidate"] = resolve(source, args.candidate, env)
        report["requestedHead"] = args.requested_head or args.candidate
        if not re.fullmatch(r"[0-9a-f]{40}", report["requestedHead"]):
            raise ValueError("requested head must be a full commit SHA")
        go_version = command(["go", "version"], source, env, 30).strip()
        bun_version = command(["bun", "--version"], source, env, 30).strip()
        report["environment"] = {"go": go_version, "bun": bun_version, "system": platform.system(), "machine": platform.machine(), "gomaxprocs": 2}
        with tempfile.TemporaryDirectory(prefix="impartus-performance-", dir=env.get("TMPDIR")) as temp:
            root = Path(temp)
            trees = {role: root / role for role in ("base", "candidate")}
            for role, tree in trees.items():
                archive(source, report[role], tree, env)
            mode = harness_mode([(trees["base"] / path).is_file() for path in CORE])
            trusted = trees[mode if mode == "base" else "candidate"]
            report["policyProvenance"] = mode
            policy = validate_policy(strict_json((trusted / CORE[1]).read_text()))
            if not go_version.startswith(f"go version go{policy['goVersion']} ") and not go_version.startswith(f"go version go{policy['goVersion']}-"):
                raise ValueError("Go version does not match calibrated policy")
            if bun_version != policy["bunVersion"] or platform.system() != "Linux" or platform.machine() not in {"x86_64", "amd64"}:
                raise ValueError("this policy requires pinned Bun on Linux amd64")
            report["policy"] = policy
            report["harnessDigests"] = {path: hashlib.sha256((trusted / path).read_bytes()).hexdigest() for path in HARNESS}
            report["harnessChanges"] = [path for path in HARNESS if (trees["candidate"] / path).read_bytes() != (trusted / path).read_bytes()]
            for tree in trees.values():
                for path in (CORE[2], HARNESS[3]):
                    (tree / path).parent.mkdir(parents=True, exist_ok=True)
                    (tree / path).write_bytes((trusted / path).read_bytes())
                command(["bun", "install", "--frozen-lockfile", "--ignore-scripts"], tree / "ui", env, 180)
            for repetition in range(3):
                for role in (("base", "candidate") if repetition % 2 == 0 else ("candidate", "base")):
                    tree = trees[role]
                    download_path = root / f"{role}-{repetition}-download.json"
                    measured_env = dict(env, IMPARTUS_PERF_DOWNLOAD_REPORT=str(download_path))
                    try:
                        raw = command(["go", "test", "./internal/downloader", "-run", "^TestPerformanceDownloadWork$", "-bench", "^BenchmarkPerformanceDecrypt$", "-benchtime=10x", "-count=1", "-benchmem", "-timeout=30s"], tree, measured_env)
                        decrypt = parse_go(raw)
                        download = validate_download(strict_json(download_path.read_text()))
                        ui = validate_ui(strict_json(command(["bun", "run", "scripts/benchmark.ts"], tree / "ui", env, 60)))
                        report["samples"][role].append({"decryption": decrypt, "download": download, "ui": ui})
                    except (ValueError, RuntimeError, OSError) as exc:
                        report["errors"].append(f"{role} sample {repetition + 1}: {exc}")
            if report["errors"]:
                if len(report["samples"]["base"]) == 3:
                    report["status"] = "failed"
                    report["candidateCheckFailures"] = list(report["errors"])
                    exit_code = 1
                raise ValueError("measurement incomplete; no comparative verdict")
            fingerprints = {role: {tuple(sample["ui"][label]["finalFrameSha256"] for label in UI_CASES) for sample in report["samples"][role]} for role in trees}
            if any(len(values) != 1 for values in fingerprints.values()):
                raise ValueError("UI output is nondeterministic within one source")
            report["frameComparison"] = fingerprints["base"] == fingerprints["candidate"]
            report["violations"] = budget_violations(report["samples"]["candidate"], policy)
            report["status"] = "failed" if report["violations"] else "passed"
            exit_code = 1 if report["violations"] else 0
    except (ValueError, RuntimeError, OSError, subprocess.SubprocessError, tarfile.TarError) as exc:
        report["errors"].append(str(exc))
    (output / "results.json").write_text(json.dumps(report, indent=2, allow_nan=False) + "\n")
    summary = markdown(report)
    (output / "summary.md").write_text(summary)
    if args.summary:
        with args.summary.open("a") as stream:
            stream.write(summary)
    print(summary)
    return exit_code


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, default=Path.cwd())
    parser.add_argument("--base", required=True)
    parser.add_argument("--candidate", required=True)
    parser.add_argument("--requested-head")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--summary", type=Path)
    parser.add_argument("--ci-config", type=Path, help="write trusted pinned tool versions as GitHub step outputs")
    args = parser.parse_args()
    if args.ci_config:
        try:
            sys.exit(configuration(args))
        except (ValueError, RuntimeError, OSError, subprocess.SubprocessError) as exc:
            args.output.mkdir(parents=True, exist_ok=True)
            (args.output / "results.json").write_text(json.dumps({"schemaVersion": 1, "status": "unavailable", "errors": [str(exc)]}) + "\n")
            print(str(exc), file=sys.stderr)
            sys.exit(2)
    sys.exit(run(args))
