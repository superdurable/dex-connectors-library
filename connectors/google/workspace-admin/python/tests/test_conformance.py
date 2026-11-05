# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Google Workspace Admin provider decisions."""

import secrets
import sys
from datetime import UTC, datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.google_workspace_admin as workspace_admin

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import (  # noqa: E402
    HeartbeatContext,
    PythonOperationAdapter,
    load_scenarios,
    replay,
    static_credentials,
)

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/google/workspace-admin/scenarios.json")
CONNECTION = sdk.ConnectionRef("google", "google-workspace-admin-test")
ADAPTER = PythonOperationAdapter(
    "google-workspace-admin",
    CONNECTION,
    workspace_admin,
    lambda client, _: workspace_admin.Client(
        workspace_admin.Config(endpoint="http://127.0.0.1", max_response_bytes=512),
        static_credentials(
            CONNECTION, workspace_admin.Credentials(access_token=sdk.SecretString("directory-token"))
        ),
        client,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object], monkeypatch: pytest.MonkeyPatch) -> None:
    """Compare requests and SDK-coded decisions with the Go recording."""
    monkeypatch.setattr(secrets, "token_bytes", lambda count: b"B" * count)
    monkeypatch.setattr(
        HeartbeatContext, "first_attempt_at", property(lambda self: datetime.now(UTC)), raising=False
    )
    await replay(scenario, ADAPTER.invoke, "/admin/directory/v1")
