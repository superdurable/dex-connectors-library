# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Gorgias requests and operation outcomes."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.gorgias as gorgias

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay, static_credentials  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/gorgias/scenarios.json")
CONNECTION = sdk.ConnectionRef("gorgias", "gorgias-test")
ADAPTER = PythonOperationAdapter(
    "gorgias",
    CONNECTION,
    gorgias,
    lambda http_client, _: gorgias.Client(
        gorgias.Config(domain="acme"),
        static_credentials(
            CONNECTION,
            gorgias.Credentials("agent@acme.example.com", sdk.SecretString("gorgiasTestKey0123456789abcdef")),
        ),
        http_client,
        api_base_url="http://127.0.0.1:22001/api",
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Require the Python package to match Go's recorded provider contract."""
    await replay(scenario, ADAPTER.invoke, "/api")
