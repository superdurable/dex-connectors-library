# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Test Python connector packages with an SDK floor against the repository's newest SDK; exact pins are skipped."""

from __future__ import annotations

import argparse
import importlib.util
import os
import subprocess
import sys
import tempfile
import tomllib
from collections.abc import Callable, Sequence
from pathlib import Path

REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
DEFAULT_SDK_DIRECTORY = REPOSITORY_ROOT / "sdkpython"


def load_component_release():
    if "component_release" in sys.modules:
        return sys.modules["component_release"]
    specification = importlib.util.spec_from_file_location(
        "component_release", REPOSITORY_ROOT / "script" / "release" / "component_release.py")
    assert specification is not None and specification.loader is not None
    module = importlib.util.module_from_spec(specification)
    sys.modules[specification.name] = module
    specification.loader.exec_module(module)
    return module


component_release = load_component_release()
CommandRunner = Callable[..., subprocess.CompletedProcess]


def read_sdk_source_version(sdk_directory: Path) -> str:
    return tomllib.loads((sdk_directory / "pyproject.toml").read_text(encoding="utf-8"))["project"]["version"]


def read_package_sdk_requirement(package: Path):
    pyproject = tomllib.loads((package / "pyproject.toml").read_text(encoding="utf-8"))
    return component_release.parse_python_sdk_requirement(
        str(package), component_release.python_package_dependencies(pyproject))


def select_floor_packages(packages: Sequence[Path], sdk_version: str) -> list[Path]:
    """Return the packages whose floor range admits sdk_version, printing why each other package is skipped."""
    sdk_version_parts = tuple(int(part) for part in sdk_version.split("."))
    selected = []
    for package in packages:
        requirement = read_package_sdk_requirement(package)
        if not requirement.is_floor:
            print(f"skip {package}: legacy exact pin {component_release.PYTHON_SDK_DISTRIBUTION}=={requirement.version}", flush=True)
        elif sdk_version_parts[0] >= 1:
            print(f"skip {package}: SDK source {sdk_version} is outside the declared range <1", flush=True)
        elif tuple(int(part) for part in requirement.version.split(".")) > sdk_version_parts:
            raise ValueError(f"{package} floor {requirement.version} is newer than the SDK source {sdk_version}")
        else:
            selected.append(package)
    return selected


def build_sdk_wheel(sdk_directory: Path, output_directory: Path, run: CommandRunner) -> Path:
    run(["uv", "build", "--wheel", "--out-dir", str(output_directory), str(sdk_directory)], check=True)
    wheels = sorted(output_directory.glob("*.whl"))
    if len(wheels) != 1:
        raise ValueError(f"expected one SDK wheel in {output_directory}, found {len(wheels)}")
    return wheels[0]


def check_package_with_sdk_wheel(package: Path, sdk_wheel: Path, sdk_version: str, run: CommandRunner) -> bool:
    """Type-check and test package in a temporary environment where sdk_wheel replaces the locked SDK."""
    with tempfile.TemporaryDirectory(prefix="dex-latest-python-sdk-") as environment_directory:
        # A separate environment keeps the package's .venv on its locked floor for the regular test run.
        environment = dict(os.environ, UV_PROJECT_ENVIRONMENT=environment_directory)
        project = ["--project", str(package)]
        sdk = component_release.PYTHON_SDK_DISTRIBUTION
        commands = [
            ["uv", "sync", "--locked", *project],
            ["uv", "pip", "install", "--python", environment_directory, "--reinstall-package", sdk, str(sdk_wheel)],
            ["uv", "run", "--no-sync", *project, "python", "-c",
             f"import importlib.metadata, sys; installed = importlib.metadata.version({sdk!r}); "
             f"sys.exit(0 if installed == {sdk_version!r} else f'installed {sdk} {{installed}}, expected {sdk_version}')"],
            ["uv", "run", "--no-sync", *project, "mypy", "--strict", str(package / "src")],
            ["uv", "run", "--no-sync", *project, "pytest", str(package / "tests")],
        ]
        for command in commands:
            if run(command, env=environment, check=False).returncode != 0:
                print(f"FAIL {package}: {' '.join(command)}", file=sys.stderr, flush=True)
                return False
    print(f"ok {package} with {sdk} {sdk_version}", flush=True)
    return True


def main(arguments: Sequence[str] | None = None, run: CommandRunner = subprocess.run) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sdk-directory", type=Path, default=DEFAULT_SDK_DIRECTORY)
    parser.add_argument("packages", nargs="*", type=Path, help="Python connector package directories")
    options = parser.parse_args(arguments)
    sdk_version = read_sdk_source_version(options.sdk_directory)
    packages = select_floor_packages(options.packages, sdk_version)
    if not packages:
        print("No selected Python connector package requires an SDK floor.", flush=True)
        return 0
    with tempfile.TemporaryDirectory(prefix="dex-python-sdk-wheel-") as wheel_directory:
        sdk_wheel = build_sdk_wheel(options.sdk_directory, Path(wheel_directory), run)
        failed = [package for package in packages if not check_package_with_sdk_wheel(package, sdk_wheel, sdk_version, run)]
    if failed:
        print(f"{len(failed)} package(s) fail with SDK source {sdk_version}: " + ", ".join(map(str, failed)),
              file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except ValueError as error:
        print(f"error: {error}", file=sys.stderr, flush=True)
        sys.exit(1)
