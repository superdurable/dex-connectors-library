# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded signed Dada status requests through Python."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.dada as dada

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/dada/scenarios.json")
CONNECTION = sdk.ConnectionRef("dada", "dada")
FIXED_CLOCK = datetime.fromtimestamp(1488363493, UTC)


def new_client(http_client: object, recorder: object) -> dada.Client:
    """Supply inert keys and the Go fixture's fixed Unix timestamp."""
    name = recorder.scenario["name"]
    is_missing = name == "queryOrderStatus-missing-credentials"
    limit = 5 if name == "queryOrderStatus-oversize" else 65_536
    return dada.Client(
        dada.Config(max_response_bytes=limit),
        conformance.static_credentials(
            CONNECTION,
            dada.Credentials(
                "" if is_missing else "123456",
                sdk.SecretString("" if is_missing else "abcdefg123"),
                "" if is_missing else "73753",
            ),
        ),
        http_client,
        clock=lambda: FIXED_CLOCK,
    )


ADAPTER = conformance.PythonOperationAdapter("dada", CONNECTION, dada, new_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check signing, outputs, branches, Retry, and request counts against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
