# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded signed Cloopen account requests through Python."""

import sys
from datetime import datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.cloopen as cloopen

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/cloopen/scenarios.json")
CONNECTION = sdk.ConnectionRef("cloopen", "cloopen")


def new_client(http_client: object, recorder: object) -> cloopen.Client:
    """Supply inert credentials and the Go fixture's exact UTC signing time."""
    scenario = recorder.scenario
    name = scenario["name"]
    is_missing = name == "getBalance-missing-credentials"
    limit = 4 if name == "getBalance-oversize" else 65_536
    fixed_clock = datetime.fromisoformat(scenario["fixedClock"].replace("Z", "+00:00"))
    return cloopen.Client(
        cloopen.Config(base_url="http://127.0.0.1:8848", max_response_bytes=limit),
        conformance.static_credentials(
            CONNECTION,
            cloopen.Credentials(
                sdk.SecretString("" if is_missing else "abcdefghijklmnopqrstuvwxyz012345"),
                sdk.SecretString("" if is_missing else "012345abcdefghijklmnopqrstuvwxyz"),
            ),
        ),
        http_client,
        clock=lambda: fixed_clock,
    )


ADAPTER = conformance.PythonOperationAdapter("cloopen", CONNECTION, cloopen, new_client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check exact signature, headers, branches, Retry, and output against Go."""
    await conformance.replay(scenario, ADAPTER.invoke)
