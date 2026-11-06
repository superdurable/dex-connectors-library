# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Outlook Mail provider exchanges."""

import sys
from datetime import datetime
from pathlib import Path
from typing import Any

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.microsoft_outlook_mail as outlook_mail

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import ExchangeRecorder, PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/microsoft/outlook-mail/scenarios.json")
CONNECTION = sdk.ConnectionRef("microsoft", "outlook-mail-test")


def create_client(http_client: httpx.AsyncClient, recorder: ExchangeRecorder) -> outlook_mail.Client:
    """Construct the connector with inert credentials and the replay transport."""
    fixed = recorder.scenario.get("fixedClock", "1970-01-01T00:00:01Z")
    clock = datetime.fromisoformat(fixed.replace("Z", "+00:00"))
    return outlook_mail.Client(
        outlook_mail.Config(),
        sdk.StaticCredentialProvider(
            {
                CONNECTION: outlook_mail.Credentials(
                    auth_method_id="microsoft-oauth",
                    access_token=sdk.SecretString("OUTLOOK_CONFORMANCE_TOKEN"),
                )
            }
        ),
        http_client,
        now=lambda: clock,
    )


ADAPTER = PythonOperationAdapter("outlook-mail", CONNECTION, outlook_mail, create_client, with_context=True)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, Any]) -> None:
    """Compare every request, branch, output, retry, and checkpoint to Go."""
    await replay(scenario, ADAPTER.invoke, "/v1.0")
