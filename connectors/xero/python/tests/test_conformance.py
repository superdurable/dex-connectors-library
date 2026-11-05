# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Xero provider requests and Dex outcomes."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.xero as xero

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/xero/scenarios.json")
CONNECTION = sdk.ConnectionRef("xero", "xero-test")
ADAPTER = PythonOperationAdapter(
    "xero",
    CONNECTION,
    xero,
    lambda client, recorder: xero.Client(
        xero.Config(),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: xero.Credentials(
                    auth_method_id="custom-connection",
                    client_id="0F1E2D3C4B5A69788796A5B4C3D2E1F0",
                    client_secret=sdk.SecretString("client-secret"),
                    access_token=sdk.SecretString("xero-test-access-token-0123456789"),
                )
            }
        ),
        client,
        local_provider_url="http://127.0.0.1",
        now=lambda: datetime(2026, 10, 1, tzinfo=UTC),
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go's request, exact model output, branch, retry, and uncertainty."""
    await replay(scenario, ADAPTER.invoke, "/api.xro/2.0")
