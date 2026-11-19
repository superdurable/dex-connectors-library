# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Dimleap model catalog scenarios through Python."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.dimleap as dimleap

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/dimleap/scenarios.json")
CONNECTION = sdk.ConnectionRef("dimleap", "dimleap")


def new_client(http_client: object, recorder: object) -> dimleap.Client:
    """Supply an inert Key and the scenario-specific response bound."""
    name = recorder.scenario["name"]
    is_missing = name == "listModels-missing-credentials"
    limit = 16 if name == "listModels-oversize" else 1_048_576
    return dimleap.Client(
        dimleap.Config(max_response_bytes=limit),
        conformance.static_credentials(
            CONNECTION,
            dimleap.Credentials(sdk.SecretString("" if is_missing else "sk-test-secret")),
        ),
        http_client,
    )


ADAPTER = conformance.PythonOperationAdapter("dimleap", CONNECTION, dimleap, new_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check request, output, branches, Retry, and request counts against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
