# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Prove that Go downloads survive transient proxy failures and still fail on permanent ones."""

import base64
import hashlib
import http.server
import io
import os
import subprocess
import tempfile
import threading
import unittest
import zipfile
from pathlib import Path

from script.go_download_retry import (
    GO_DOWNLOAD_ATTEMPTS,
    go_package_prewarm_command,
    is_transient_go_download_failure,
    prewarm_go_packages,
    run_go_download_with_retries,
)

FIXTURE_MODULE = "example.com/fixture"
FIXTURE_VERSION = "v1.0.0"
FIXTURE_GO_MOD = f"module {FIXTURE_MODULE}\n\ngo 1.24\n".encode()
FIXTURE_SOURCE = b'package fixture\n\nconst Name = "fixture"\n'
STREAM_ERROR = ('go: github.com/superdurable/dex/sdk-go@v1.25.0: read "https://proxy.golang.org/github.com/'
                'superdurable/dex/sdk-go/@v/v1.25.0.zip": stream error: stream ID 1; INTERNAL_ERROR; received from peer\n')
TOOLCHAIN_STREAM_ERROR = ('go: download go1.26.0: golang.org/toolchain@v0.0.1-go1.26.0.linux-amd64: read '
                          '"https://proxy.golang.org/golang.org/toolchain/@v/v0.0.1-go1.26.0.linux-amd64.zip": '
                          'stream error: stream ID 1; INTERNAL_ERROR; received from peer\n')
STORAGE_ERROR = ('go: github.com/superdurable/dex/sdk-go@v1.25.0: Get "https://storage.googleapis.com/'
                 'proxy-golang-org-prod/abc": dial tcp 142.250.0.1:443: i/o timeout\n')


def go_sum_hash(files: dict[str, bytes]) -> str:
    """Returns Go's h1: directory hash of the named files."""
    summary = "".join(f"{hashlib.sha256(files[name]).hexdigest()}  {name}\n" for name in sorted(files))
    return "h1:" + base64.b64encode(hashlib.sha256(summary.encode()).digest()).decode()


class ScriptedGoRunner:
    """Returns scripted results in order and records each command it was asked to run."""

    def __init__(self, results: list[tuple[int, str]]) -> None:
        self.results = list(results)
        self.commands: list[tuple[str, ...]] = []

    def __call__(self, arguments, directory, environment) -> subprocess.CompletedProcess[str]:
        self.commands.append(tuple(arguments))
        returncode, stderr = self.results.pop(0)
        return subprocess.CompletedProcess(list(arguments), returncode, stdout="", stderr=stderr)


class TransientFailureClassificationTest(unittest.TestCase):
    def test_reported_ci_network_failures_are_transient(self) -> None:
        for output in (STREAM_ERROR, TOOLCHAIN_STREAM_ERROR, STORAGE_ERROR,
                       "go: reading https://proxy.golang.org/x/@v/list: 502 Bad Gateway\n",
                       "go: example.com/x@v1.0.0: unexpected EOF\n"):
            with self.subTest(output=output):
                self.assertTrue(is_transient_go_download_failure(output))

    def test_permanent_and_unrecognized_failures_are_not_transient(self) -> None:
        for output in ('go: example.com/x@v9.9.9: reading https://proxy.golang.org/example.com/x/@v/v9.9.9.info: 404 Not Found\n',
                       "verifying example.com/x@v1.0.0: checksum mismatch\nSECURITY ERROR\n",
                       'go: example.com/x@v1.0.0: invalid version: unknown revision v1.0.0\n',
                       'go: Get "https://proxy.golang.org/x/@v/list": tls: failed to verify certificate: x509: '
                       'certificate signed by unknown authority\n',
                       'go: Get "https://proxy.golang.org/x/@v/list": unrecognized failure\n',
                       "go: go.mod requires go >= 1.99 (running go 1.24; GOTOOLCHAIN=local)\n",
                       "build failed\n"):
            with self.subTest(output=output):
                self.assertFalse(is_transient_go_download_failure(output))


class RetryLoopTest(unittest.TestCase):
    def test_transient_failures_retry_with_doubling_delay_then_succeed(self) -> None:
        runner = ScriptedGoRunner([(1, STREAM_ERROR), (1, STORAGE_ERROR), (0, "")])
        delays: list[float] = []
        result = run_go_download_with_retries(("go", "mod", "tidy"), Path("."), {}, sleep=delays.append, runner=runner)
        self.assertEqual(0, result.returncode)
        self.assertEqual([5.0, 10.0], delays)
        self.assertEqual(3, len(runner.commands))

    def test_persistent_transient_failure_returns_the_original_error_after_bounded_attempts(self) -> None:
        runner = ScriptedGoRunner([(1, STREAM_ERROR)] * GO_DOWNLOAD_ATTEMPTS)
        result = run_go_download_with_retries(("go", "mod", "tidy"), Path("."), {}, sleep=lambda _: None, runner=runner)
        self.assertEqual(1, result.returncode)
        self.assertEqual(STREAM_ERROR, result.stderr)
        self.assertEqual(GO_DOWNLOAD_ATTEMPTS, len(runner.commands))

    def test_permanent_failure_is_returned_on_the_first_attempt(self) -> None:
        checksum = "verifying example.com/x@v1.0.0: checksum mismatch\nSECURITY ERROR\n"
        runner = ScriptedGoRunner([(1, checksum)])
        delays: list[float] = []
        result = run_go_download_with_retries(("go", "mod", "tidy"), Path("."), {}, sleep=delays.append, runner=runner)
        self.assertEqual((1, checksum), (result.returncode, result.stderr))
        self.assertEqual([], delays)


class PrewarmCommandTest(unittest.TestCase):
    def test_prewarm_loads_only_the_packages_the_later_command_loads(self) -> None:
        self.assertEqual(["go", "list", "-deps", "-test", "./..."], go_package_prewarm_command(["./..."], "", True))
        self.assertEqual(["go", "list", "-deps", "-test", "-tags=integration", "./..."],
                         go_package_prewarm_command(["./..."], "integration", True))
        self.assertEqual(["go", "list", "-deps", "./cmd/connectorctl"], go_package_prewarm_command(["./cmd/connectorctl"], "", False))


class FaultInjectingProxy:
    """Serves one fixture module as a GOPROXY whose first zip requests fail with zip_status."""

    def __init__(self, failing_zip_requests: int, zip_status: int = 502) -> None:
        self.failing_zip_requests = failing_zip_requests
        self.zip_status = zip_status
        self.zip_requests = 0
        prefix = f"{FIXTURE_MODULE}@{FIXTURE_VERSION}/"
        archive = io.BytesIO()
        with zipfile.ZipFile(archive, "w") as module_zip:
            module_zip.writestr(prefix + "go.mod", FIXTURE_GO_MOD)
            module_zip.writestr(prefix + "fixture.go", FIXTURE_SOURCE)
        version_path = f"/{FIXTURE_MODULE}/@v/{FIXTURE_VERSION}"
        self.files = {
            version_path + ".info": f'{{"Version":"{FIXTURE_VERSION}"}}'.encode(),
            version_path + ".mod": FIXTURE_GO_MOD,
            version_path + ".zip": archive.getvalue(),
        }
        proxy = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self) -> None:
                if self.path.endswith(".zip"):
                    proxy.zip_requests += 1
                    if proxy.zip_requests <= proxy.failing_zip_requests:
                        self.send_error(proxy.zip_status)
                        return
                body = proxy.files.get(self.path)
                if body is None:
                    self.send_error(404)
                    return
                self.send_response(200)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *_arguments) -> None:
                pass

        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self) -> "FaultInjectingProxy":
        self.thread.start()
        return self

    def __exit__(self, *_exception) -> None:
        self.server.shutdown()
        self.server.server_close()

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{self.server.server_address[1]}"


class FaultInjectingGoProxyTest(unittest.TestCase):
    def setUp(self) -> None:
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.module = self.root / "consumer"
        self.module.mkdir()
        self.go_mod = f"module example.com/consumer\n\ngo 1.24\n\nrequire {FIXTURE_MODULE} {FIXTURE_VERSION}\n"
        (self.module / "go.mod").write_text(self.go_mod)
        zip_hash = go_sum_hash({f"{FIXTURE_MODULE}@{FIXTURE_VERSION}/go.mod": FIXTURE_GO_MOD,
                                f"{FIXTURE_MODULE}@{FIXTURE_VERSION}/fixture.go": FIXTURE_SOURCE})
        self.go_sum = (f"{FIXTURE_MODULE} {FIXTURE_VERSION} {zip_hash}\n"
                       f"{FIXTURE_MODULE} {FIXTURE_VERSION}/go.mod {go_sum_hash({'go.mod': FIXTURE_GO_MOD})}\n")
        (self.module / "go.sum").write_text(self.go_sum)
        (self.module / "main.go").write_text(f'package main\n\nimport "{FIXTURE_MODULE}"\n\nfunc main() {{ println(fixture.Name) }}\n')

    def environment(self, proxy: FaultInjectingProxy) -> dict[str, str]:
        return {**os.environ, "GOWORK": "off", "GOPROXY": proxy.url, "GOSUMDB": "off", "GOTOOLCHAIN": "local",
                "GOFLAGS": "-modcacherw", "GOMODCACHE": str(self.root / "module-cache"), "GOPRIVATE": ""}

    def test_transient_proxy_failure_is_retried_and_tests_then_run_offline(self) -> None:
        with FaultInjectingProxy(failing_zip_requests=1) as proxy:
            delays: list[float] = []
            result = prewarm_go_packages(self.module, self.environment(proxy), sleep=delays.append)
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(2, proxy.zip_requests)
        self.assertEqual([5.0], delays)
        self.assertEqual((self.go_mod, self.go_sum), ((self.module / "go.mod").read_text(), (self.module / "go.sum").read_text()))
        offline = subprocess.run(["go", "vet", "./..."], cwd=self.module, env={**self.environment(proxy), "GOPROXY": "off"},
                                 text=True, capture_output=True, check=False)
        self.assertEqual(0, offline.returncode, offline.stderr)

    def test_persistent_proxy_failure_fails_with_the_original_error(self) -> None:
        with FaultInjectingProxy(failing_zip_requests=GO_DOWNLOAD_ATTEMPTS) as proxy:
            result = prewarm_go_packages(self.module, self.environment(proxy), sleep=lambda _: None)
        self.assertNotEqual(0, result.returncode)
        self.assertIn("502 Bad Gateway", result.stderr)
        self.assertEqual(GO_DOWNLOAD_ATTEMPTS, proxy.zip_requests)

    def test_missing_module_version_fails_without_retry(self) -> None:
        with FaultInjectingProxy(failing_zip_requests=1, zip_status=404) as proxy:
            result = prewarm_go_packages(self.module, self.environment(proxy), sleep=lambda _: self.fail("retried"))
        self.assertNotEqual(0, result.returncode)
        self.assertIn("404 Not Found", result.stderr)
        self.assertEqual(1, proxy.zip_requests)

    def test_missing_go_sum_entry_fails_like_the_test_and_leaves_go_sum_unchanged(self) -> None:
        trimmed = self.go_sum.splitlines(keepends=True)[1]
        (self.module / "go.sum").write_text(trimmed)
        with FaultInjectingProxy(failing_zip_requests=0) as proxy:
            result = prewarm_go_packages(self.module, self.environment(proxy), sleep=lambda _: self.fail("retried"))
        self.assertNotEqual(0, result.returncode)
        self.assertIn("missing go.sum entry", result.stderr)
        self.assertEqual(trimmed, (self.module / "go.sum").read_text())


if __name__ == "__main__":
    unittest.main()
