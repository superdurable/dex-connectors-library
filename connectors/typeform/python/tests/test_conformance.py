# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Typeform provider scenarios through the Python operations."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.typeform as typeform

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/typeform/scenarios.json")
CONNECTION = sdk.ConnectionRef("typeform", "forms")
ADAPTER = conformance.PythonOperationAdapter(
    "typeform",
    CONNECTION,
    typeform,
    lambda http_client, _: typeform.Client(
        typeform.Config(),
        conformance.static_credentials(
            CONNECTION,
            typeform.Credentials(
                access_token=sdk.SecretString("SENTINEL-TYPEFORM-ACCESS-TOKEN"),
                webhook_secret=sdk.SecretString("SENTINEL-TYPEFORM-WEBHOOK-SECRET"),
            ),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go provider requests, branches, retries, and exact outputs."""
    await conformance.replay(scenario, ADAPTER.invoke)
