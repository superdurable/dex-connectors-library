# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Vercel provider exchanges through Python queries."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.vercel as vercel

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/vercel/scenarios.json")
CONNECTION = sdk.ConnectionRef("vercel", "vercel")
ADAPTER = PythonOperationAdapter(
    "vercel",
    CONNECTION,
    vercel,
    lambda http_client, recorder: vercel.Client(
        vercel.Config(base_url="http://127.0.0.1:12345"),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: vercel.Credentials(
                    sdk.SecretString(
                        "" if recorder.scenario["name"] == "listDeployments-defect" else "test-accesstoken"
                    )
                )
            }
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare each request, branch, retry, and exact SDK output to Go."""
    await replay(scenario, ADAPTER.invoke)
