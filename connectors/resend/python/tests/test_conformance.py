# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Resend HTTP behavior through the Python package."""

import sys
from pathlib import Path

import dex_connectors.resend as resend
import dex_connectors.sdk as sdk
import pytest

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/resend/scenarios.json")
CONNECTION = sdk.ConnectionRef("resend", "resend")
ADAPTER = PythonOperationAdapter(
    "resend", CONNECTION, resend,
    lambda http_client, _: resend.Client(
        resend.Config(base_url="http://127.0.0.1"),
        sdk.StaticCredentialProvider({CONNECTION: resend.Credentials(sdk.SecretString("test-secret-token"))}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go requests, retries, branches, and exact SDK output."""
    await replay(scenario, ADAPTER.invoke)
