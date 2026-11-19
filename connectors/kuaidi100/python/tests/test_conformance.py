# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded Kuaidi100 parcel scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.kuaidi100 as kuaidi100

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/kuaidi100/scenarios.json")
CONNECTION = sdk.ConnectionRef("kuaidi100", "kuaidi100")
ADAPTER = conformance.PythonOperationAdapter(
    "kuaidi100",
    CONNECTION,
    kuaidi100,
    lambda http_client, recorder: kuaidi100.Client(
        kuaidi100.Config(base_url="https://poll.kuaidi100.com"),
        conformance.static_credentials(
            CONNECTION,
            kuaidi100.Credentials(sdk.SecretString("test-customer"), sdk.SecretString("test-key")),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact Go requests, outcomes, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
