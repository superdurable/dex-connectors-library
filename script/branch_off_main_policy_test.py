# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT

from __future__ import annotations

import importlib.util
import unittest
from pathlib import Path
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
MODULE_PATH = ROOT / "script" / "branch_off_main_policy.py"
SPEC = importlib.util.spec_from_file_location("branch_off_main_policy", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
POLICY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(POLICY)


class ParseCreateTest(unittest.TestCase):
    def test_parses_supported_branch_creation_commands(self) -> None:
        cases = {
            "git switch -c feature origin/main": ("feature", "origin/main"),
            "git checkout -b fix/headers HEAD": ("fix/headers", "HEAD"),
            "git branch docs origin/main": ("docs", "origin/main"),
            "git -C repo switch --create tooling origin/main": (
                "tooling",
                "origin/main",
            ),
        }
        for command, expected in cases.items():
            with self.subTest(command=command):
                self.assertEqual(POLICY.parse_create(command), expected)

    def test_ignores_non_creation_and_protected_branch_commands(self) -> None:
        commands = (
            "git switch feature",
            "git branch --delete feature",
            "git branch --show-current",
            "git switch -c main origin/main",
        )
        for command in commands:
            with self.subTest(command=command):
                self.assertIsNone(POLICY.parse_create(command))


class ValidateStartRefTest(unittest.TestCase):
    @mock.patch.object(POLICY, "fetch_main")
    @mock.patch.object(POLICY, "git_output")
    def test_accepts_origin_main(
        self,
        git_output: mock.Mock,
        fetch_main: mock.Mock,
    ) -> None:
        git_output.side_effect = ["abc123", "abc123"]

        allowed, message = POLICY.validate_start_ref("origin/main", ROOT)

        self.assertTrue(allowed)
        self.assertEqual(message, "")
        fetch_main.assert_called_once_with(ROOT)

    @mock.patch.object(POLICY, "fetch_main")
    @mock.patch.object(POLICY, "git_output")
    def test_rejects_stale_start_ref(
        self,
        git_output: mock.Mock,
        fetch_main: mock.Mock,
    ) -> None:
        git_output.side_effect = ["new-main", "stale-head"]

        allowed, message = POLICY.validate_start_ref("HEAD", ROOT)

        self.assertFalse(allowed)
        self.assertIn("is not origin/main", message)
        self.assertIn("git switch -c <branch> origin/main", message)
        fetch_main.assert_called_once_with(ROOT)


if __name__ == "__main__":
    unittest.main()
