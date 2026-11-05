# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded Linear provider scenarios."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.linear as linear

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/linear/scenarios.json")
CONNECTION = sdk.ConnectionRef("linear", "linear-test")
CREDENTIAL = linear.Credentials(
    auth_method_id="personal-api-key",
    api_key=sdk.SecretString("lin_" + "api_" + "SENTINELKEY0123456789abcdefghijklmnopqrst"),
)
ADAPTER = PythonOperationAdapter(
    "linear",
    CONNECTION,
    linear,
    lambda client, _: linear.Client(
        linear.Config(),
        sdk.StaticCredentialProvider({CONNECTION: CREDENTIAL}),
        client,
        api_url="http://127.0.0.1/graphql",
        now=lambda: datetime(2026, 10, 30, tzinfo=UTC),
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go's exact requests, retries, branches, and encoded outputs."""
    await replay(scenario, ADAPTER.invoke)
