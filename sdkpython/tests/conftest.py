# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Shared fixture path for Go-produced cross-language conformance records."""

from pathlib import Path

import pytest


@pytest.fixture(scope="session")
def conformance_dir() -> Path:
    """Require the checked-in Go conformance fixtures."""
    path = Path(__file__).resolve().parents[2] / "conformance"
    if not path.is_dir():
        pytest.fail("shared Go conformance fixtures are missing")
    return path
