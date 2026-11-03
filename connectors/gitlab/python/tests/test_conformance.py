# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay shared GitLab provider scenarios against the Python package."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.gitlab as gitlab

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/gitlab/scenarios.json")
CONNECTION = sdk.ConnectionRef("gitlab", "gitlab")
ADAPTER = conformance.PythonOperationAdapter(
    "gitlab", CONNECTION, gitlab,
    lambda client, _: gitlab.Client(
        gitlab.Config(base_url="http://127.0.0.1"),
        conformance.static_credentials(CONNECTION, gitlab.Credentials(sdk.SecretString("test-accesstoken"))),
        client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match Go-recorded requests, branches, retries, and safe output."""
    await conformance.replay(scenario, ADAPTER.invoke)
