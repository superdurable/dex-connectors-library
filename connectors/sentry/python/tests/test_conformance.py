# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Sentry provider scenarios through the Python package."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.sentry as sentry

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/sentry/scenarios.json")
CONNECTION = sdk.ConnectionRef("sentry", "sentry")
ADAPTER = conformance.PythonOperationAdapter(
    "sentry", CONNECTION, sentry,
    lambda client, _: sentry.Client(
        sentry.Config(base_url="http://127.0.0.1:8848"),
        conformance.static_credentials(CONNECTION, sentry.Credentials(sdk.SecretString("test-secret-token"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check provider requests and outcomes against the Go recording."""
    await conformance.replay(scenario, ADAPTER.invoke)
