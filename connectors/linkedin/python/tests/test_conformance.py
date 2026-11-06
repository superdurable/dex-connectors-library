# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded LinkedIn UserInfo scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.linkedin as linkedin

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/linkedin/scenarios.json")
CONNECTION = sdk.ConnectionRef("linkedin", "signup")
ADAPTER = PythonOperationAdapter(
    "linkedin",
    CONNECTION,
    linkedin,
    lambda client, recorder: linkedin.Client(
        linkedin.Config(user_info_url="http://127.0.0.1/v2/userinfo"),
        sdk.StaticCredentialProvider(
            {}
            if recorder.scenario["name"] == "missing-credential"
            else {
                CONNECTION: linkedin.Credentials(
                    "", sdk.SecretString(""), sdk.SecretString("one-use-token"), sdk.SecretString("")
                )
            }
        ),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare the request and encoded Query attempt against Go."""
    await replay(scenario, ADAPTER.invoke, "")
