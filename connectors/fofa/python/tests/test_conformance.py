# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded fofa requests and outcomes."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.fofa as fofa

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/fofa/scenarios.json")
CONNECTION = sdk.ConnectionRef("fofa", "fofa")
ADAPTER = conformance.PythonOperationAdapter(
    "fofa",
    CONNECTION,
    fofa,
    lambda http_client, recorder: fofa.Client(
        fofa.Config(),
        conformance.static_credentials(CONNECTION, fofa.Credentials(sdk.SecretString("test-secret"))),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact Go requests, branches, outputs, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
