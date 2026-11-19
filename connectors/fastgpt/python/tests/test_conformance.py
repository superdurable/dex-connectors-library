# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded FastGPT provider scenarios through the Python Query."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.fastgpt as fastgpt

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/fastgpt/scenarios.json")
CONNECTION = sdk.ConnectionRef("fastgpt", "fastgpt")
ADAPTER = conformance.PythonOperationAdapter(
    "fastgpt",
    CONNECTION,
    fastgpt,
    lambda http_client, recorder: fastgpt.Client(
        fastgpt.Config(
            base_url="http://127.0.0.1:12345/fastgpt",
            max_response_bytes=8 if recorder.scenario["name"] == "getDatasetDetails-oversize" else 1048576,
        ),
        conformance.static_credentials(CONNECTION, fastgpt.Credentials(sdk.SecretString("private-test-key")))
        if recorder.scenario["name"] != "getDatasetDetails-missing-key"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact requests, outputs, branches, and retry decisions against Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke, path_prefix="/fastgpt")
