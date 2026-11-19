# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded OPPO auth and quota requests."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.oppo as oppo

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/oppo/scenarios.json")
CONNECTION = sdk.ConnectionRef("oppo", "oppo")


def make_client(client: object, recorder: conformance.ExchangeRecorder) -> oppo.Client:
    """Use the Go test clock so the auth signature is exact."""
    app_key = "" if recorder.scenario["name"] == "getPushQuota-defect" else "test-app-key"
    credentials = oppo.Credentials(sdk.SecretString(app_key), sdk.SecretString("test-master-secret"))
    fixed = datetime.fromtimestamp(1731510000, UTC)
    return oppo.Client(
        oppo.Config(), conformance.static_credentials(CONNECTION, credentials), client, lambda: fixed
    )


ADAPTER = conformance.PythonOperationAdapter("oppo", CONNECTION, oppo, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare both HTTP exchanges and SDK result with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
