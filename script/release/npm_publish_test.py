# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

from __future__ import annotations

import base64
import hashlib
import importlib.util
import io
import json
import os
import subprocess
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest import mock

SCRIPT = Path(__file__).with_name("npm_publish.py")
sys.path.insert(0, str(SCRIPT.parent))
SPEC = importlib.util.spec_from_file_location("npm_publish", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
publisher = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = publisher
SPEC.loader.exec_module(publisher)


def npm_result(returncode: int, stderr: str = "", stdout: str = "") -> subprocess.CompletedProcess[str]:
    return subprocess.CompletedProcess(("npm",), returncode, stdout, stderr)


def dry_run_result(name: str = "@superdurable/connector-newsapi", version: str = "0.1.0") -> subprocess.CompletedProcess[str]:
    return npm_result(0, stdout=json.dumps({name: {"name": name, "version": version}}))


class NpmPublishTest(unittest.TestCase):
    def setUp(self) -> None:
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.tarball = Path(temporary.name) / "package.tgz"
        with tarfile.open(self.tarball, "w:gz") as archive:
            package_json = b'{"name":"@superdurable/connector-newsapi","version":"0.1.0"}'
            member = tarfile.TarInfo("package/package.json")
            member.size = len(package_json)
            archive.addfile(member, io.BytesIO(package_json))
        self.package = "@superdurable/connector-newsapi"
        self.version = "v0.1.0"
        self.integrity = "sha512-" + base64.b64encode(hashlib.sha512(self.tarball.read_bytes()).digest()).decode()

    def test_identical_existing_version_is_success(self) -> None:
        with mock.patch.object(publisher, "npm", return_value=npm_result(0, stdout=json.dumps(self.integrity))) as npm:
            publisher.publish(self.package, str(self.tarball), self.version)
        npm.assert_called_once()

    def test_different_existing_version_fails_without_token_issue(self) -> None:
        with mock.patch.object(publisher, "npm", return_value=npm_result(0, stdout='"sha512-other"')), mock.patch.object(
            publisher, "report_token_issue"
        ) as issue:
            with self.assertRaisesRegex(RuntimeError, "different integrity"):
                publisher.publish(self.package, str(self.tarball), self.version)
        issue.assert_not_called()

    def test_existing_package_uses_trusted_publishing(self) -> None:
        with mock.patch.object(publisher, "npm", side_effect=[npm_result(1, "E404"), npm_result(0), dry_run_result(), npm_result(0)]) as npm:
            publisher.publish(self.package, str(self.tarball), self.version)
        self.assertIn("--dry-run", npm.call_args_list[2].args)
        self.assertEqual(npm.call_args_list[3].args[1], str(self.tarball))
        self.assertNotIn("token", npm.call_args_list[3].kwargs)

    def test_first_package_uses_token_and_provenance(self) -> None:
        with mock.patch.dict(os.environ, {"NPM_TOKEN": "secret"}), mock.patch.object(
            publisher, "npm", side_effect=[npm_result(1, "E404"), npm_result(1, "E404"), dry_run_result(), npm_result(0)]
        ) as npm:
            publisher.publish(self.package, str(self.tarball), self.version)
        self.assertEqual(npm.call_args_list[3].kwargs["token"], "secret")
        self.assertIn("--provenance", npm.call_args_list[3].args)

    def test_first_sdk_package_uses_token_after_both_registry_404s(self) -> None:
        package = "@superdurable/connectors-sdk"
        with tarfile.open(self.tarball, "w:gz") as archive:
            package_json = b'{"name":"@superdurable/connectors-sdk","version":"0.1.0"}'
            member = tarfile.TarInfo("package/package.json")
            member.size = len(package_json)
            archive.addfile(member, io.BytesIO(package_json))
        with mock.patch.dict(os.environ, {"NPM_TOKEN": "secret"}), mock.patch.object(
            publisher, "npm", side_effect=[npm_result(1, "E404"), npm_result(1, "E404"),
                                          dry_run_result(package), npm_result(0)]
        ) as npm:
            publisher.publish(package, str(self.tarball), "v0.1.0")
        self.assertEqual(npm.call_args_list[0].args[:3], ("view", f"{package}@0.1.0", "dist.integrity"))
        self.assertEqual(npm.call_args_list[1].args[:2], ("view", package))
        self.assertEqual(npm.call_args_list[3].kwargs["token"], "secret")
        self.assertIn("--provenance", npm.call_args_list[3].args)

    def test_existing_package_falls_back_only_on_auth_failure(self) -> None:
        with mock.patch.dict(os.environ, {"NPM_TOKEN": "secret"}), mock.patch.object(
            publisher, "npm", side_effect=[npm_result(1, "E404"), npm_result(0), dry_run_result(), npm_result(1, "E401"), dry_run_result(), npm_result(0)]
        ) as npm:
            publisher.publish(self.package, str(self.tarball), self.version)
        self.assertEqual(npm.call_count, 6)

    def test_e403_republish_does_not_report_token_issue(self) -> None:
        with mock.patch.object(
            publisher, "npm", side_effect=[npm_result(1, "E404"), npm_result(0), dry_run_result(), npm_result(1, "E403 You cannot publish over previously published versions")]
        ), mock.patch.object(publisher, "report_token_issue") as issue:
            with self.assertRaisesRegex(RuntimeError, "non-authentication"):
                publisher.publish(self.package, str(self.tarball), self.version)
        issue.assert_not_called()

    def test_auth_failure_reports_rotation_without_token(self) -> None:
        with mock.patch.dict(os.environ, {"NPM_TOKEN": "secret"}), mock.patch.object(
            publisher, "npm", side_effect=[npm_result(1, "E404"), npm_result(1, "E404"), dry_run_result(), npm_result(1, "E401")]
        ), mock.patch.object(publisher, "report_token_issue") as issue:
            with self.assertRaisesRegex(RuntimeError, "rotate") as failure:
                publisher.publish(self.package, str(self.tarball), self.version)
        self.assertNotIn("secret", str(failure.exception))
        issue.assert_called_once()

    def test_candidate_requires_existing_package_and_uses_token(self) -> None:
        with mock.patch.dict(os.environ, {"NPM_TOKEN": "secret"}), mock.patch.object(
            publisher, "npm", side_effect=[npm_result(1, "E404"), npm_result(0), dry_run_result(), npm_result(0)]
        ) as npm:
            publisher.publish(self.package, str(self.tarball), self.version, "next")
        self.assertEqual(npm.call_args_list[3].kwargs["token"], "secret")
        self.assertNotIn("--provenance", npm.call_args_list[3].args)
        with mock.patch.object(publisher, "npm", side_effect=[npm_result(1, "E404"), npm_result(1, "E404")]):
            with self.assertRaisesRegex(RuntimeError, "placeholder"):
                publisher.publish(self.package, str(self.tarball), self.version, "next")

    def test_dry_run_rejects_identity_mismatch_before_real_publish(self) -> None:
        with mock.patch.object(publisher, "npm", side_effect=[npm_result(1, "E404"), npm_result(0),
                                                                dry_run_result("@superdurable/connectors-sdk", "9.9.9")]) as npm:
            with self.assertRaisesRegex(RuntimeError, "dry-run package identity"):
                publisher.publish(self.package, str(self.tarball), self.version)
        self.assertEqual(npm.call_count, 3)

    def test_unrelated_oidc_error_does_not_report_token_issue(self) -> None:
        self.assertFalse(publisher.is_auth_error(npm_result(1, "E403 provenance OIDC signature validation failed")))

    def test_npm_publish_otp_failure_is_authentication_error(self) -> None:
        self.assertTrue(publisher.is_auth_error(npm_result(1, "EOTP one-time password required")))

    def test_existing_release_label_is_not_modified(self) -> None:
        responses = [npm_result(0, stdout='[{"name":"area:release"}]'),
                     npm_result(0, stdout='[{"number":7,"title":"npm: NPM_TOKEN needs rotation"}]'),
                     npm_result(0), npm_result(0)]
        with mock.patch.dict(os.environ, {"GH_TOKEN": "github-token"}), mock.patch.object(
            publisher.subprocess, "run", side_effect=responses
        ) as run:
            publisher.report_token_issue("expiry is unknown")
        self.assertEqual([call.args[0][1:3] for call in run.call_args_list],
                         [("label", "list"), ("issue", "list"), ("issue", "edit"), ("issue", "comment")])

    def test_token_expiry_uses_variable_within_fourteen_days(self) -> None:
        with mock.patch.dict(os.environ, {"NPM_TOKEN": "secret", "NPM_TOKEN_EXPIRES_AT": "2020-01-01"}), mock.patch.object(
            publisher, "npm", return_value=npm_result(0)
        ) as npm, mock.patch.object(publisher, "report_token_issue") as issue:
            with self.assertRaisesRegex(RuntimeError, "expires within 14 days"):
                publisher.check_token()
        self.assertEqual(npm.call_count, 1)
        issue.assert_called_once()

    def test_multiple_tokens_selects_matching_prefix(self) -> None:
        tokens = [{"key": "other", "expires": "2020-01-01"}, {"key": "npm_abc", "expires": "2099-01-01"}]
        with mock.patch.dict(os.environ, {"NPM_TOKEN": "npm_abcdef", "NPM_TOKEN_EXPIRES_AT": ""}), mock.patch.object(
            publisher, "npm", side_effect=[npm_result(0), npm_result(0, stdout=json.dumps(tokens))]
        ), mock.patch.object(publisher, "report_token_issue") as issue:
            publisher.check_token()
        issue.assert_not_called()

    def test_token_list_failure_and_missing_variable_report_issue(self) -> None:
        with mock.patch.dict(os.environ, {"NPM_TOKEN": "secret", "NPM_TOKEN_EXPIRES_AT": ""}), mock.patch.object(
            publisher, "npm", side_effect=[npm_result(0), npm_result(1, "E403")]
        ), mock.patch.object(publisher, "report_token_issue") as issue:
            publisher.check_token()
        issue.assert_called_once()

    def test_missing_variable_without_matching_token_reports_issue(self) -> None:
        with mock.patch.dict(os.environ, {"NPM_TOKEN": "secret", "NPM_TOKEN_EXPIRES_AT": ""}), mock.patch.object(
            publisher, "npm", side_effect=[npm_result(0), npm_result(0, stdout='[{"key":"other","expires":"2099-01-01"}]')]
        ), mock.patch.object(publisher, "report_token_issue") as issue:
            publisher.check_token()
        issue.assert_called_once()


if __name__ == "__main__":
    unittest.main()
