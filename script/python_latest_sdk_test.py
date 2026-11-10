# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check which Python connector packages run against the SDK source and how their environment is built."""

from __future__ import annotations

import importlib.util
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("python_latest_sdk.py")
SPEC = importlib.util.spec_from_file_location("python_latest_sdk", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
latest_sdk = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = latest_sdk
SPEC.loader.exec_module(latest_sdk)


class RecordingRunner:
    def __init__(self, failing_command: str = "") -> None:
        self.commands: list[list[str]] = []
        self.environments: list[dict[str, str] | None] = []
        self.failing_command = failing_command

    def __call__(self, command: list[str], env: dict[str, str] | None = None, check: bool = False) -> subprocess.CompletedProcess:
        self.commands.append(command)
        self.environments.append(env)
        if command[:2] == ["uv", "build"]:
            output_directory = Path(command[command.index("--out-dir") + 1])
            (output_directory / "dex_connectors_sdk-0.8.0-py3-none-any.whl").write_bytes(b"wheel")
        return subprocess.CompletedProcess(command, 1 if self.failing_command in command else 0)


class PythonLatestSDKTest(unittest.TestCase):
    def setUp(self) -> None:
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name)
        self.sdk = self.root / "sdkpython"
        self.sdk.mkdir()
        (self.sdk / "pyproject.toml").write_text('[project]\nname = "dex-connectors-sdk"\nversion = "0.8.0"\n', encoding="utf-8")

    def tearDown(self) -> None:
        self.directory.cleanup()

    def write_package(self, name: str, requirement: str) -> Path:
        package = self.root / name / "python"
        package.mkdir(parents=True)
        (package / "pyproject.toml").write_text(
            f'[project]\nname = "dex-connector-{name}"\nversion = "0.1.0"\ndependencies = ["{requirement}"]\n', encoding="utf-8")
        return package

    def test_only_floor_packages_admitting_the_sdk_source_are_selected(self) -> None:
        floor = self.write_package("floor", "dex-connectors-sdk>=0.7.0,<1")
        exact = self.write_package("exact", "dex-connectors-sdk==0.1.0")
        self.assertEqual(latest_sdk.select_floor_packages([floor, exact], "0.8.0"), [floor])
        self.assertEqual(latest_sdk.select_floor_packages([floor], "1.0.0"), [])
        with self.assertRaisesRegex(ValueError, "newer than the SDK source"):
            latest_sdk.select_floor_packages([floor], "0.6.9")
        unbounded = self.write_package("unbounded", "dex-connectors-sdk>=0.7.0")
        with self.assertRaisesRegex(ValueError, "must require dex-connectors-sdk"):
            latest_sdk.select_floor_packages([unbounded], "0.8.0")

    def test_floor_package_replaces_the_locked_sdk_in_a_separate_environment(self) -> None:
        floor = self.write_package("floor", "dex-connectors-sdk>=0.7.0,<1")
        exact = self.write_package("exact", "dex-connectors-sdk==0.1.0")
        runner = RecordingRunner()
        self.assertEqual(latest_sdk.main(["--sdk-directory", str(self.sdk), str(floor), str(exact)], run=runner), 0)
        self.assertEqual(runner.commands[0][:3], ["uv", "build", "--wheel"])
        package_commands = runner.commands[1:]
        self.assertEqual(package_commands[0], ["uv", "sync", "--locked", "--project", str(floor)])
        environment_directory = runner.environments[1]["UV_PROJECT_ENVIRONMENT"]
        self.assertNotEqual(Path(environment_directory), floor / ".venv")
        self.assertEqual({environment["UV_PROJECT_ENVIRONMENT"] for environment in runner.environments[1:]}, {environment_directory})
        install = package_commands[1]
        self.assertEqual(install[:7], ["uv", "pip", "install", "--python", environment_directory, "--reinstall-package", "dex-connectors-sdk"])
        self.assertTrue(install[7].endswith("dex_connectors_sdk-0.8.0-py3-none-any.whl"))
        self.assertIn("'0.8.0'", package_commands[2][-1])
        self.assertEqual(package_commands[3][-3:], ["mypy", "--strict", str(floor / "src")])
        self.assertEqual(package_commands[4][-2:], ["pytest", str(floor / "tests")])
        self.assertTrue(all(command[:3] == ["uv", "run", "--no-sync"] for command in package_commands[2:]))
        self.assertFalse(any(str(exact) in " ".join(command) for command in runner.commands))

    def test_a_failing_package_fails_the_check_and_no_floor_package_builds_nothing(self) -> None:
        floor = self.write_package("floor", "dex-connectors-sdk>=0.7.0,<1")
        self.assertEqual(latest_sdk.main(["--sdk-directory", str(self.sdk), str(floor)], run=RecordingRunner("pytest")), 1)
        exact = self.write_package("exact", "dex-connectors-sdk==0.1.0")
        runner = RecordingRunner()
        self.assertEqual(latest_sdk.main(["--sdk-directory", str(self.sdk), str(exact)], run=runner), 0)
        self.assertEqual(runner.commands, [])


if __name__ == "__main__":
    unittest.main()
