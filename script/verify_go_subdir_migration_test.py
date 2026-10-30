# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

import unittest

from script.verify_go_subdir_migration import git_blob_hash, rewrite_go_commands, rewrite_readme_links, scrub_manifest_version


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

    def test_go_command_rewrite(self) -> None:
        self.assertEqual(rewrite_go_commands("cd connectors/acme\ncd connectors/acme/python\n", "connectors/acme"),
                         "cd connectors/acme/go\ncd connectors/acme/python\n")


if __name__ == "__main__":
    unittest.main()
