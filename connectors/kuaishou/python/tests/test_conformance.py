# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay exact Go-recorded Kuaishou video scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.kuaishou as kuaishou

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/kuaishou/scenarios.json")
CONNECTION = sdk.ConnectionRef("kuaishou", "kuaishou")
ADAPTER = conformance.PythonOperationAdapter(
    "kuaishou",
    CONNECTION,
    kuaishou,
    lambda http_client, recorder: kuaishou.Client(
        kuaishou.Config(),
        conformance.static_credentials(
            CONNECTION,
            kuaishou.Credentials("app123", sdk.SecretString("token456")),
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact Go requests, outcomes, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
