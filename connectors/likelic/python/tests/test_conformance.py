# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded Likelic room scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.likelic as likelic

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/likelic/scenarios.json")
CONNECTION = sdk.ConnectionRef("likelic", "likelic")
ADAPTER = conformance.PythonOperationAdapter(
    "likelic",
    CONNECTION,
    likelic,
    lambda http_client, recorder: likelic.Client(
        likelic.Config(),
        conformance.static_credentials(
            CONNECTION,
            likelic.Credentials(
                "demo_company", sdk.SecretString("sk-0123456789abcdef0123456789abcdef0123456789abcdef")
            ),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact Go requests, outcomes, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
