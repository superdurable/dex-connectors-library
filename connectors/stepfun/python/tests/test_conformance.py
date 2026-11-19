# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded StepFun model-list scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.stepfun as stepfun

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/stepfun/scenarios.json")
CONNECTION = sdk.ConnectionRef("stepfun", "stepfun")
ADAPTER = conformance.PythonOperationAdapter(
    "stepfun",
    CONNECTION,
    stepfun,
    lambda http_client, recorder: stepfun.Client(
        stepfun.Config(),
        conformance.static_credentials(CONNECTION, stepfun.Credentials(sdk.SecretString("secret-api-key")))
        if recorder.scenario["name"] != "listModels-defect"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare model-list requests, outputs, and decisions with Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
