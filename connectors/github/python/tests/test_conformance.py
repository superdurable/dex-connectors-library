# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded GitHub provider scenarios."""

import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.github as github

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/github/scenarios.json")
CONNECTION = sdk.ConnectionRef("github", "github")


class _RejectedCredentialSource:
    async def resolve(self, call: sdk.Call) -> github.Credentials:
        return github.Credentials(
            "", sdk.SecretString(""), sdk.SecretString("rejected-token"), sdk.SecretString("")
        )

    async def resolve_after_rejection(self, call: sdk.Call) -> github.Credentials:
        return github.Credentials(
            "", sdk.SecretString(""), sdk.SecretString("replacement-token"), sdk.SecretString("")
        )


def _client(http_client: object, recorder: object) -> github.Client:
    token = "" if recorder.scenario["name"] == "getAuthenticatedProfile-defect" else "manifest-access-token"
    credentials = github.Credentials(
        "manifest-client-id",
        sdk.SecretString("manifest-client-secret"),
        sdk.SecretString(token),
        sdk.SecretString(""),
    )
    return github.Client(
        github.Config(
            base_url="http://127.0.0.1:12345",
            max_response_bytes=100 if "-oversized" in recorder.scenario["name"] else 2097152,
        ),
        _RejectedCredentialSource()
        if recorder.scenario["name"].endswith("-refresh-after-rejection")
        else sdk.StaticCredentialProvider({CONNECTION: credentials}),
        http_client,
        lambda: datetime(2026, 1, 2, 3, 4, 5, tzinfo=UTC),
    )


ADAPTER = PythonOperationAdapter("github", CONNECTION, github, _client)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check provider requests and Query decisions against Go recordings."""
    await replay(scenario, ADAPTER.invoke)
