# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Halo article scenarios through the Python Query."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.fit2cloud as fit2cloud

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/fit2cloud/scenarios.json")
CONNECTION = sdk.ConnectionRef("fit2cloud", "fit2cloud")
ADAPTER = conformance.PythonOperationAdapter(
    "fit2cloud",
    CONNECTION,
    fit2cloud,
    lambda http_client, recorder: fit2cloud.Client(
        fit2cloud.Config(
            "https://halo.example.test",
            max_response_bytes=16 if recorder.scenario["name"] == "listPosts-oversized" else 2097152,
        ),
        conformance.static_credentials(
            CONNECTION, fit2cloud.Credentials(sdk.SecretString("pat_private-test-key"))
        )
        if recorder.scenario["name"] != "listPosts-defect"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact requests, branches, retry decisions, and output against Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
