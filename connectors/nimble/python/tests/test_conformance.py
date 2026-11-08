# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Nimble provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.nimble as nimble

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/nimble/scenarios.json")
CONNECTION = sdk.ConnectionRef("nimble", "nimble")
TEST_CREDENTIALS = nimble.Credentials(sdk.SecretString("test-token"))
ADAPTER = conformance.PythonOperationAdapter(
    "nimble",
    CONNECTION,
    nimble,
    lambda http_client, recorder: nimble.Client(
        nimble.Config(base_url="http://127.0.0.1:8848/api/v1"),
        conformance.static_credentials(CONNECTION, TEST_CREDENTIALS)
        if recorder.scenario["name"] != "listContactIDs-defect"
        else conformance.static_credentials(
            sdk.ConnectionRef("nimble", "missing"), TEST_CREDENTIALS
        ),
        http_client,
    ),
    operation_types={"listContactIDs": nimble.ListContactIDsOperation},
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/api/v1")
