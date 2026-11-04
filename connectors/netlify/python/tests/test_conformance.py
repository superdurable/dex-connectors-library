# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Netlify provider decisions."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.netlify as netlify

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/netlify/scenarios.json")
CONNECTION = sdk.ConnectionRef("netlify", "netlify")
ADAPTER = conformance.PythonOperationAdapter(
    "netlify", CONNECTION, netlify,
    lambda client, recorder: netlify.Client(
        netlify.Config(base_url="http://127.0.0.1:12345"),
        ConformanceCredentials(recorder.scenario["name"] == "sites-missing-credentials"),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go requests and outcomes for both queries."""
    await conformance.replay(scenario, ADAPTER.invoke)


class ConformanceCredentials:
    def __init__(self, unavailable: bool) -> None:
        self.unavailable = unavailable

    async def resolve(self, call: sdk.Call) -> netlify.Credentials:
        if self.unavailable:
            raise LookupError("credentials unavailable")
        return netlify.Credentials(sdk.SecretString("test-accesstoken"))
