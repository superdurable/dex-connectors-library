# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Classify oversized provider bodies only after throttling and outage statuses."""

import pytest
from dex_connectors.sdk import FailureKind

from dex_connectors.linkedin_ads.client import Exchange
from dex_connectors.linkedin_ads.operations import _read_failure


@pytest.mark.parametrize(
    ("status", "kind"), [(429, FailureKind.RATE_LIMIT), (503, FailureKind.AVAILABILITY)]
)
def test_oversized_throttle_and_outage_bodies_retry(status: int, kind: FailureKind) -> None:
    attempt = _read_failure(Exchange(status, b"", 0, "too_large"))
    assert attempt is not None
    assert attempt.kind == "retry"
    assert attempt.failure is not None and attempt.failure.kind == kind


def test_oversized_success_body_selects_invalid_response() -> None:
    attempt = _read_failure(Exchange(200, b"", 0, "too_large"))
    assert attempt is not None
    assert attempt.kind == "branch"
    assert attempt.branch == "invalidResponse"
