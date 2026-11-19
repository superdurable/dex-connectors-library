# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Caoliao QR-code list requests and outcomes."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.caoliao as caoliao

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/caoliao/scenarios.json")
CONNECTION = sdk.ConnectionRef("caoliao", "caoliao")


def make_client(client: object, recorder: conformance.ExchangeRecorder) -> caoliao.Client:
    """Build a provider client with the recorded response limit and synthetic Key."""
    max_response_bytes = 16 if recorder.scenario["name"] == "listQRCodes-oversized" else 2097152
    api_key = "" if recorder.scenario["name"] == "listQRCodes-defect" else "caoliao-test-secret"
    return caoliao.Client(
        caoliao.Config(max_response_bytes=max_response_bytes),
        conformance.static_credentials(CONNECTION, caoliao.Credentials(sdk.SecretString(api_key))),
        client,
    )


ADAPTER = conformance.PythonOperationAdapter("caoliao", CONNECTION, caoliao, make_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare exact provider requests and SDK outcomes with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
