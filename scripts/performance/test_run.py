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
        for key, value in {"schemaVersion": True, "goVersion": "1.26.5\nextra=1", "decryptionBytesPerOperationMax": 0}.items():
            with self.subTest(key=key), self.assertRaises(ValueError):
                runner.validate_policy(dict(policy, **{key: value}))

    def test_command_timeout_is_bounded(self):
        with tempfile.TemporaryDirectory() as directory, self.assertRaisesRegex(RuntimeError, "timed out"):
            runner.command([sys.executable, "-c", "import time; time.sleep(30)"], directory, runner.clean_environment(), timeout=0.05)

    def test_environment_does_not_inject_provider_credentials(self):
        from unittest.mock import patch
        with patch.dict("os.environ", {"OPENAI_API_KEY": "fake", "GEMINI_API_KEY": "fake", "GITHUB_TOKEN": "fake"}):
            env = runner.clean_environment()
        self.assertFalse({"OPENAI_API_KEY", "GEMINI_API_KEY", "GITHUB_TOKEN"} & env.keys())


if __name__ == "__main__":
    unittest.main()
