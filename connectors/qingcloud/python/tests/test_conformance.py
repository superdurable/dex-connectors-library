# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded Qingcloud requests and outcomes."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.qingcloud as provider

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/qingcloud/scenarios.json")
CONNECTION = sdk.ConnectionRef("qingcloud", "qingcloud")


def create_client(http_client: object, recorder: conformance.ExchangeRecorder) -> provider.Client:
    """Bind inert fixture credentials, including the missing-credential case."""
    import httpx

    assert isinstance(http_client, httpx.AsyncClient)
    is_defect = recorder.scenario["name"].endswith("-defect")
    credentials = provider.Credentials(
        "" if is_defect else "REDACTED_QINGCLOUD_ACCESS_KEY",
        sdk.SecretString("REDACTED_QINGCLOUD_SECRET"),
    )
    from datetime import datetime

    fixed_clock = datetime.fromisoformat(recorder.scenario["fixedClock"].replace("Z", "+00:00"))
    return provider.Client(
        provider.Config(),
        conformance.static_credentials(CONNECTION, credentials),
        http_client,
        clock=lambda: fixed_clock,
    )


ADAPTER = conformance.PythonOperationAdapter("qingcloud", CONNECTION, provider, create_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match every Go request, output, branch, and retry decision exactly."""
    await conformance.replay(scenario, ADAPTER.invoke)
