# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Moka provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.moka as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/moka/scenarios.json")
CONNECTION = sdk.ConnectionRef("moka", "moka")


def create_client(http_client: object, recorder: conformance.ExchangeRecorder) -> connector.Client:
    """Build a credential-safe client for each Go recording."""
    credential = connector.Credentials(sdk.SecretString("secret-api-key"))
    if (
        recorder.scenario["name"] == "listJobFunctions-defect"
        and recorder.scenario["expected"]["failureKind"] == "AUTHENTICATION"
    ):
        credential = connector.Credentials(sdk.SecretString(""))
    return connector.Client(
        connector.Config(org_id="orgid-test"),
        conformance.static_credentials(CONNECTION, credential),
        http_client,
    )


ADAPTER = conformance.PythonOperationAdapter("moka", CONNECTION, connector, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
