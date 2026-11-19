# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded Mureka task scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.kunlun as kunlun

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/kunlun/scenarios.json")
CONNECTION = sdk.ConnectionRef("kunlun", "kunlun")
ADAPTER = conformance.PythonOperationAdapter(
    "kunlun",
    CONNECTION,
    kunlun,
    lambda http_client, recorder: kunlun.Client(
        kunlun.Config(),
        conformance.static_credentials(
            CONNECTION,
            kunlun.Credentials(sdk.SecretString("secret-api-key")),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact Go requests, outcomes, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
