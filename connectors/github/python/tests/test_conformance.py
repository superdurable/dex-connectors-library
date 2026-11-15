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

# listIssues and createIssue are Go only; replay just the operations this package implements.
PYTHON_OPERATIONS = {
    "getAuthenticatedProfile", "listPublicRepositories", "listMergedPullRequests", "listPullRequestFiles",
    "listReleases", "listCommits",
}
SCENARIOS = [
    scenario
    for scenario in load_scenarios(ROOT / "conformance/connectors/github/scenarios.json")
    if scenario["operation"] in PYTHON_OPERATIONS
]
CONNECTION = sdk.ConnectionRef("github", "github")


class _RejectedCredentialSource:
    async def resolve(self, call: sdk.Call) -> github.Credentials:
        return github.Credentials(
            oauth_client_id="",
            oauth_client_secret=sdk.SecretString(""),
            access_token=sdk.SecretString("rejected-token"),
            refresh_token=sdk.SecretString(""),
        )

    async def resolve_after_rejection(self, call: sdk.Call) -> github.Credentials:
        return github.Credentials(
            oauth_client_id="",
            oauth_client_secret=sdk.SecretString(""),
            access_token=sdk.SecretString("replacement-token"),
            refresh_token=sdk.SecretString(""),
        )


def _client(http_client: object, recorder: object) -> github.Client:
    token = "" if recorder.scenario["name"] == "getAuthenticatedProfile-defect" else "manifest-access-token"
    credentials = github.Credentials(
        oauth_client_id="manifest-client-id",
        oauth_client_secret=sdk.SecretString("manifest-client-secret"),
        access_token=sdk.SecretString(token),
        refresh_token=sdk.SecretString(""),
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
