# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Bocha search requests and outcomes."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.bocha as bocha

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/bocha/scenarios.json")
CONNECTION = sdk.ConnectionRef("bocha", "bocha")


def make_client(client: object, recorder: conformance.ExchangeRecorder) -> bocha.Client:
    """Build the provider client with the scenario's limit and synthetic Key."""
    max_response_bytes = 16 if recorder.scenario["name"] == "searchWeb-oversized" else 1048576
    api_key = "" if recorder.scenario["name"] == "searchWeb-defect" else "test-bocha-key"
    return bocha.Client(
        bocha.Config(max_response_bytes=max_response_bytes),
        conformance.static_credentials(CONNECTION, bocha.Credentials(sdk.SecretString(api_key))),
        client,
    )


ADAPTER = conformance.PythonOperationAdapter("bocha", CONNECTION, bocha, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare the provider request and SDK result with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
