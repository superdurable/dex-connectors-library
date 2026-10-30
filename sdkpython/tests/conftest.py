# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Shared fixture path for Go-produced cross-language conformance records."""

import os
from pathlib import Path

import pytest


@pytest.fixture(scope="session")
def conformance_dir() -> Path:
    """Locate the shared root fixtures or a branch checkout during development."""
    override = os.environ.get("CONFORMANCE_FIXTURE_DIR")
    path = Path(override) if override else Path(__file__).resolve().parents[2] / "conformance"
    if not path.is_dir():
        pytest.skip("shared conformance fixtures have not merged into main yet")
    return path
