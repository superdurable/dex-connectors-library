# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded KuaidiBird parcel scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.kuaidibird as kuaidibird

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/kuaidibird/scenarios.json")
CONNECTION = sdk.ConnectionRef("kuaidibird", "kuaidibird")
ADAPTER = conformance.PythonOperationAdapter(
    "kuaidibird",
    CONNECTION,
    kuaidibird,
    lambda http_client, recorder: kuaidibird.Client(
        kuaidibird.Config(base_url="https://poll.kuaidibird.com"),
        conformance.static_credentials(
            CONNECTION,
            kuaidibird.Credentials(sdk.SecretString("1237100"), sdk.SecretString("test-api-key")),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact Go requests, outcomes, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
