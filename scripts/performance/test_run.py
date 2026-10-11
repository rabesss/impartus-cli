import copy
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("performance_run", Path(__file__).with_name("run.py"))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


def go_output(bytes_per_op=1040):
    return "\n".join(f"BenchmarkPerformanceDecrypt/bytes-{size}-2 10 1234 ns/op 100 MB/s {bytes_per_op} B/op 3 allocs/op {size} plaintext-bytes/op" for size in runner.SIZES)


def ui_output():
    return {"schemaVersion": 1, "scenario": "ui-navigation-v1", "courseCount": 5000, "iterations": 200,
            "results": [{"label": label, "width": width, "height": height, "selectedCourseIndex": 200,
                         "finalFrameSha256": "a" * 64, "coldFrameMilliseconds": 3, "p50InputToFrameMilliseconds": 1,
                         "p95InputToFrameMilliseconds": 2, "p99InputToFrameMilliseconds": 3, "residentSetBytes": 1000}
                        for label, (width, height) in runner.UI_CASES.items()]}


class MeasurementTests(unittest.TestCase):
    def test_allocation_gate_rejects_extra_copy_without_timing_gate(self):
        policy = {"decryptionBytesPerOperationMax": 2048, "decryptionAllocationsPerOperationMax": 5}
        self.assertEqual(runner.budget_violations([{"decryption": runner.parse_go(go_output())}], policy), [])
        self.assertEqual(len(runner.budget_violations([{"decryption": runner.parse_go(go_output(1057838))}], policy)), 2)

    def test_missing_duplicate_unknown_or_reduced_work_is_rejected(self):
        lines = go_output().splitlines()
        for value in [lines[0], "\n".join([*lines, lines[0]]), go_output().replace("65536-2", "4-2"), go_output().replace("65536 plaintext", "4 plaintext"), go_output().replace(" 10 ", " 1 ")]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                runner.parse_go(value)

    def test_nonfinite_metrics_and_nonstandard_json_are_rejected(self):
        for value in [go_output().replace("1234 ns/op", "nan ns/op"), go_output().replace("1234 ns/op", "-1 ns/op")]:
            with self.assertRaises(ValueError):
                runner.parse_go(value)
        for value in ['{"a":1,"a":2}', '{"a":NaN}', '{"a":Infinity}']:
            with self.assertRaises(ValueError):
                runner.strict_json(value)

    def test_download_requires_exact_complete_work(self):
        runner.validate_download(copy.deepcopy(runner.DOWNLOAD))
        for key, value in {"keyRequests": 2, "chunkRequests": 11, "maxConcurrentRequests": 2, "plaintextSha256": "a" * 64,
                           "encryptedTempFiles": 1, "schemaVersion": True, "chunkBodyBytes": 959}.items():
            changed = dict(runner.DOWNLOAD, **{key: value})
            with self.subTest(key=key), self.assertRaises(ValueError):
                runner.validate_download(changed)

    def test_ui_rejects_omitted_navigation_and_wrong_fixtures(self):
        runner.validate_ui(ui_output())
        for key, value in {"selectedCourseIndex": 0, "width": 81, "finalFrameSha256": "bad", "p95InputToFrameMilliseconds": float("nan")}.items():
            changed = ui_output()
            changed["results"][0][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                runner.validate_ui(changed)
        changed = ui_output()
        changed["results"][1] = changed["results"][0]
        with self.assertRaises(ValueError):
            runner.validate_ui(changed)

    def test_partial_baseline_cannot_bootstrap(self):
        self.assertEqual(runner.harness_mode([False, False, False]), "bootstrap")
        self.assertEqual(runner.harness_mode([True, True, True]), "base")
        for present in [[True, False, False], [False, True, True]]:
            with self.assertRaises(ValueError):
                runner.harness_mode(present)

    def test_policy_rejects_invalid_versions_and_limits(self):
        policy = json.loads(Path(__file__).with_name("budgets.json").read_text())
        runner.validate_policy(policy)
        for key, value in {"schemaVersion": True, "goVersion": "1.26.5\nextra=1", "decryptionBytesPerOperationMax": 10**400}.items():
            with self.subTest(key=key), self.assertRaises(ValueError):
                runner.validate_policy(dict(policy, **{key: value}))

    def test_only_trusted_go_test_entrypoint_is_retained(self):
        with tempfile.TemporaryDirectory() as directory:
            trusted, tree = Path(directory)/"trusted", Path(directory)/"candidate"
            for path in [runner.CORE[2], runner.HARNESS[3]]:
                p=trusted/path; p.parent.mkdir(parents=True, exist_ok=True); p.write_text("trusted fixture")
            target=tree/"internal/downloader"; target.mkdir(parents=True)
            (target/"forged_main_test.go").write_text("TestMain")
            (target/"test_init_test.go").write_text("init")
            (target/"production.go").write_text("production unchanged")
            (target/"performance_benchmark_test.go").symlink_to("production.go")
            removed=runner.overlay_harness(trusted,tree)
            self.assertEqual(removed,["forged_main_test.go","test_init_test.go"])
            self.assertEqual((target/"production.go").read_text(),"production unchanged")
            self.assertEqual((target/"performance_benchmark_test.go").read_text(),"trusted fixture")
            self.assertEqual([p.name for p in target.glob("*_test.go")],["performance_benchmark_test.go"])

    def test_harness_parent_symlink_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            trusted,tree,other=Path(directory)/"trusted",Path(directory)/"tree",Path(directory)/"other"
            (tree/"internal").mkdir(parents=True);other.mkdir()
            (tree/"internal/downloader").symlink_to(other,target_is_directory=True)
            with self.assertRaisesRegex(ValueError,"directories"):
                runner.overlay_harness(trusted,tree)

    def test_command_timeout_is_bounded(self):
        with tempfile.TemporaryDirectory() as directory, self.assertRaisesRegex(RuntimeError, "timed out"):
            runner.command([sys.executable, "-c", "import time; time.sleep(30)"], directory, runner.clean_environment(), timeout=0.05)

    def test_timeout_drain_is_bounded_even_if_child_retains_pipe(self):
        import subprocess
        from unittest.mock import MagicMock, patch
        proc = MagicMock()
        proc.communicate.side_effect = [subprocess.TimeoutExpired("fixture", 1), subprocess.TimeoutExpired("fixture", 1, output=b"partial")]
        with patch.object(runner.subprocess, "Popen", return_value=proc), patch.object(runner.os, "killpg"), self.assertRaisesRegex(RuntimeError, "command timed out"):
            runner.command(["fixture"], ".", {}, timeout=1)
        self.assertEqual(proc.communicate.call_count, 2)
        proc.stdout.close.assert_called_once()
        proc.stderr.close.assert_called_once()
        proc.wait.assert_called_once_with(timeout=1)

    def test_environment_does_not_inject_provider_credentials(self):
        from unittest.mock import patch
        with patch.dict("os.environ", {"OPENAI_API_KEY": "fake", "GEMINI_API_KEY": "fake", "GITHUB_TOKEN": "fake"}):
            env = runner.clean_environment()
        self.assertFalse({"OPENAI_API_KEY", "GEMINI_API_KEY", "GITHUB_TOKEN"} & env.keys())

    def test_candidate_work_failure_is_distinct_from_unavailable_baseline(self):
        from types import SimpleNamespace
        from unittest.mock import patch
        policy = json.loads(Path(__file__).with_name("budgets.json").read_text())
        def fake_archive(source, sha, target, env):
            target.mkdir()
            for path in runner.HARNESS:
                destination = target / path
                destination.parent.mkdir(parents=True, exist_ok=True)
                destination.write_text(json.dumps(policy) if path == runner.CORE[1] else "fixture")
        for broken in ("candidate", "base", "candidate-nondeterminism", "candidate-install", "base-install"):
            with tempfile.TemporaryDirectory() as directory:
                frame=0
                def fake_command(args, cwd, env, timeout=300):
                    nonlocal frame
                    if args == ["go", "version"]:
                        return "go version go1.26.5 linux/amd64"
                    if args == ["bun", "--version"]:
                        return "1.3.14"
                    if args[:2] == ["bun", "install"] and broken == Path(cwd).parent.name + "-install":
                        raise RuntimeError("incorrect lockfile")
                    if args[:2] == ["go", "test"]:
                        if Path(cwd).name == broken:
                            raise RuntimeError("incorrect plaintext")
                        Path(env["IMPARTUS_PERF_DOWNLOAD_REPORT"]).write_text(json.dumps(runner.DOWNLOAD))
                        return go_output()
                    if args == ["bun", "./scripts/benchmark.ts"]:
                        value=ui_output()
                        if broken=="candidate-nondeterminism" and Path(cwd).parent.name=="candidate":
                            frame+=1
                            value["results"][0]["finalFrameSha256"]=str(frame).zfill(64)
                        return json.dumps(value)
                    return ""
                args = SimpleNamespace(source=Path(directory), base="1"*40, candidate="2"*40,
                                       requested_head=None, output=Path(directory)/"results", summary=None)
                with patch.object(runner, "resolve", side_effect=lambda source, sha, env: sha), patch.object(runner, "archive", side_effect=fake_archive), patch.object(runner, "command", side_effect=fake_command), patch.object(runner.platform, "system", return_value="Linux"), patch.object(runner.platform, "machine", return_value="x86_64"), patch("builtins.print"):
                    code = runner.run(args)
                report = json.loads((args.output/"results.json").read_text())
                self.assertEqual((code, report["status"]), (2, "unavailable") if broken.startswith("base") else (1, "failed"))


if __name__ == "__main__":
    unittest.main()
