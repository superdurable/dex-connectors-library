# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded signed Tuya requests and decisions."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.tuya as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/tuya/scenarios.json")
CONNECTION = sdk.ConnectionRef("tuya", "tuya")


def make_client(http_client: object, recorder: object) -> connector.Client:
    """Inject the recorded nonce sequence and fixed test clock for exact HMAC replay."""
    scenario = recorder.scenario  # type: ignore[attr-defined]
    nonces = iter(
        bytes.fromhex(exchange["requestHeaders"]["Nonce"][0])
        for exchange in scenario["exchanges"]
        if "Nonce" in exchange.get("requestHeaders", {})
    )
    return connector.Client(
        connector.Config(max_response_bytes=5 if scenario["name"] == "getDeviceDetails-oversize" else 65536),
        conformance.static_credentials(
            CONNECTION,
            connector.Credentials(
                sdk.SecretString("sample-client-id"), sdk.SecretString("sample-client-secret")
            ),
        ),
        http_client,  # type: ignore[arg-type]
        clock=lambda: datetime(2026, 1, 2, 3, 4, 5, tzinfo=UTC),
        nonce_source=lambda: next(nonces),
    )


ADAPTER = conformance.PythonOperationAdapter("tuya", CONNECTION, connector, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare complete signed requests and SDK-encoded outcomes with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
