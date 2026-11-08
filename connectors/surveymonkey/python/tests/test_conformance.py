# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay the Go-recorded SurveyMonkey provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.surveymonkey as surveymonkey

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/surveymonkey/scenarios.json")
CONNECTION = sdk.ConnectionRef("surveymonkey", "surveymonkey")
ADAPTER = conformance.PythonOperationAdapter(
    "surveymonkey",
    CONNECTION,
    surveymonkey,
    lambda http_client, _: surveymonkey.Client(
        surveymonkey.Config(base_url="http://127.0.0.1:8848/v3"),
        conformance.static_credentials(CONNECTION, surveymonkey.Credentials(sdk.SecretString("test-token"))),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/v3")
