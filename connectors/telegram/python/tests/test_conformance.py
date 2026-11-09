# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Telegram requests, branches, retries, and checkpoints."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.telegram as telegram

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/telegram/scenarios.json")
CONNECTION = sdk.ConnectionRef("telegram", "telegram")
ADAPTER = PythonOperationAdapter(
    "telegram",
    CONNECTION,
    telegram,
    lambda http_client, _: telegram.Client(
        telegram.Config(base_url="http://127.0.0.1:12345"),
        sdk.StaticCredentialProvider(
            {CONNECTION: telegram.Credentials(sdk.SecretString("123456:fake_test_token"))}
        ),
        http_client,
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Compare Python provider requests and Dex decisions with exact Go recordings."""
    await replay(scenario, ADAPTER.invoke)
