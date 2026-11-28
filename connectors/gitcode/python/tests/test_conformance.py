# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded GitCode repository scenarios through the Python Query."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.gitcode as gitcode

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/gitcode/scenarios.json")
CONNECTION = sdk.ConnectionRef("gitcode", "gitcode")
ADAPTER = conformance.PythonOperationAdapter(
    "gitcode",
    CONNECTION,
    gitcode,
    lambda http_client, recorder: gitcode.Client(
        gitcode.Config(
            max_response_bytes=16
            if recorder.scenario["name"] in ("getRepository-oversized", "getRepository-missing-smallLimit")
            else 262144
        ),
        conformance.static_credentials(
            CONNECTION, gitcode.Credentials(sdk.SecretString("test-personal-access-token"))
        ),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact requests, branches, retry decisions, and output against Go recordings."""
    await conformance.replay(scenario, ADAPTER.invoke)
