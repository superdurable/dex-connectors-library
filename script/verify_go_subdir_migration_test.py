# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from script import verify_go_subdir_migration as verifier
from script.verify_go_subdir_migration import connector_file_set_errors, expected_connector_files
from script.verify_go_subdir_migration import git_blob_hash, rewrite_readme_links, scrub_manifest_version


class MigrationVerifierTest(unittest.TestCase):
    def test_git_blob_hash(self) -> None:
        self.assertEqual(git_blob_hash(b"hello\n"), "ce013625030ba8dba906f756967f9e9ca394464a")

    def test_manifest_version_only(self) -> None:
        self.assertEqual(scrub_manifest_version(b"metadata:\n  version: v0.1.0\n"),
                         scrub_manifest_version(b"metadata:\n  version: v0.2.0\n"))

    def test_readme_links_only_for_moved_targets(self) -> None:
        contents = "[example](examples/one/README.md) [manifest](connector.yaml)"
        self.assertEqual(rewrite_readme_links(contents, "connectors/acme", {"connectors/acme/examples/one/README.md"}),
                         "[example](go/examples/one/README.md) [manifest](connector.yaml)")

    def test_file_set_rejects_unexpected_and_deleted_files(self) -> None:
        baseline = {"connectors/acme/logo.svg": "a", "connectors/acme/chat/connector.yaml": "b",
                    "connectors/acme/chat/client.go": "c", "connectors/acme/chat/python/package.json": "d"}
        expected = expected_connector_files(baseline, ["connectors/acme/chat"])
        self.assertEqual(expected, {"connectors/acme/logo.svg", "connectors/acme/chat/connector.yaml",
                                    "connectors/acme/chat/go/client.go", "connectors/acme/chat/python/package.json"})
        current = expected - {"connectors/acme/chat/go/client.go"} | {"connectors/acme/chat/go/extra_new.go"}
        self.assertEqual(connector_file_set_errors(expected, current), [
            "connectors/acme/chat/go/extra_new.go: unexpected new connector file",
            "connectors/acme/chat/go/client.go: expected connector file is missing",
        ])

    def test_changed_generated_file_requires_generator_check(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            previous_directory = Path.cwd()
            os.chdir(temporary)
            try:
                manifest = Path("connectors/acme/connector.yaml")
                manifest.parent.mkdir(parents=True)
                contents = b"metadata:\n  version: v0.2.0\n"
                manifest.write_bytes(contents)
                generated = Path("connectors/acme/go/zz_generated_connector.go")
                generated.parent.mkdir()
                generated.write_bytes(b"tampered")
                baseline = {str(manifest): git_blob_hash(contents),
                            "connectors/acme/zz_generated_connector.go": git_blob_hash(b"generated")}
                failed_check = subprocess.CompletedProcess([], 1, stdout="", stderr="generated file differs")
                with mock.patch.object(verifier, "git", return_value=b"generated"), \
                        mock.patch.object(verifier.subprocess, "run", return_value=failed_check) as run:
                    moved, errors = verifier.verify_connector("connectors/acme", "baseline", baseline)
                self.assertEqual(moved, 1)
                self.assertTrue(any("generate --check failed" in error for error in errors))
                self.assertIn("--check", run.call_args.args[0])
            finally:
                os.chdir(previous_directory)

if __name__ == "__main__":
    unittest.main()
