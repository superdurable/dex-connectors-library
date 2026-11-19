# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Bmob requests and outcomes through the Python client."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.bmob as bmob

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/bmob/scenarios.json")
CONNECTION = sdk.ConnectionRef("bmob", "bmob")


def make_client(client: object, recorder: conformance.ExchangeRecorder) -> bmob.Client:
    """Build one provider client with the scenario's response limit and credentials."""
    max_response_bytes = 16 if recorder.scenario["name"] == "countClassObjects-oversized" else 65536
    application_id = "" if recorder.scenario["name"] == "countClassObjects-defect" else "test-bmob-app"
    return bmob.Client(
        bmob.Config(base_url="http://127.0.0.1:8848", max_response_bytes=max_response_bytes),
        conformance.static_credentials(
            CONNECTION,
            bmob.Credentials(sdk.SecretString(application_id), sdk.SecretString("test-bmob-rest-key")),
        ),
        client,
    )


ADAPTER = conformance.PythonOperationAdapter("bmob", CONNECTION, bmob, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact provider requests and SDK results with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
