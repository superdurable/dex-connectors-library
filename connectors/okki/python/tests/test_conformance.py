# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Okki provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.okki as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/okki/scenarios.json")
CONNECTION = sdk.ConnectionRef("okki", "okki")


def create_client(http_client: object, recorder: conformance.ExchangeRecorder) -> connector.Client:
    """Build a credential-safe client for each Go recording."""
    credential = connector.Credentials(
        "test-client-id", sdk.SecretString("test-client-secret"), sdk.SecretString("jwt123")
    )
    if (
        recorder.scenario["name"] == "listCustomerFields-defect"
        and recorder.scenario["expected"]["failureKind"] == "AUTHENTICATION"
    ):
        credential = connector.Credentials("test-client-id", sdk.SecretString(""), sdk.SecretString("jwt123"))
    return connector.Client(
        connector.Config(), conformance.static_credentials(CONNECTION, credential), http_client
    )


ADAPTER = conformance.PythonOperationAdapter("okki", CONNECTION, connector, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
