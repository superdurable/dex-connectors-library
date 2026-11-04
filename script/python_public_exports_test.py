# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check published Python symbols and mock helpers remain pickleable."""

import importlib
import pickle
import sys
from pathlib import Path


def check_exports(package_directory: Path) -> None:
    package_names = [
        path.name
        for path in (package_directory / "src" / "dex_connectors").iterdir()
        if path.is_dir() and (path / "__init__.py").exists()
    ]
    if len(package_names) != 1:
        raise AssertionError(
            f"expected one Python connector package in {package_directory}"
        )
    package_name = package_names[0]
    package_path = f"dex_connectors.{package_name}"
    package = importlib.import_module(package_path)
    mock_names = [
        path.name
        for path in Path(package.__file__).parent.iterdir()
        if path.is_dir()
        and path.name.endswith("mock")
        and (path / "__init__.py").exists()
    ]
    if len(mock_names) != 1:
        raise AssertionError(f"expected one mock module in {package_path}")
    mock = importlib.import_module(f"{package_path}.{mock_names[0]}")
    for module in (package, mock):
        for name in module.__all__:
            value = getattr(module, name)
            restored = pickle.loads(pickle.dumps(value))
            if restored != value:
                raise AssertionError(f"{module.__name__}.{name} changed after pickling")
            if not callable(value):
                continue
            if value.__name__ != name or value.__qualname__ != name:
                raise AssertionError(
                    f"{module.__name__}.{name} has a different name or qualname"
                )
            owner = importlib.import_module(value.__module__)
            if getattr(owner, value.__qualname__, None) is not value:
                raise AssertionError(
                    f"{module.__name__}.{name} has an unresolvable module path"
                )


if __name__ == "__main__":
    check_exports(Path(sys.argv[1]))
