# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded hackernews provider exchanges."""

import importlib
import sys
from datetime import datetime
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.hackernews as connector

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/hackernews/scenarios.json")
CONNECTION = sdk.ConnectionRef("hacker-news", "public")
ADAPTER = conformance.PythonOperationAdapter(
    "hacker-news-daily",
    CONNECTION,
    connector,
    lambda http_client, recorder: connector.Client(
        connector.Config(base_url="http://127.0.0.1:12345"),
        http_client,
    ),
    operation_types={"listStoryIDs": connector.ListStoryIDsOperation},
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object], monkeypatch: pytest.MonkeyPatch) -> None:
    """Compare provider requests and SDK outcomes with Go."""

    fixed_clock = datetime.fromisoformat(
        str(scenario.get("fixedClock", "2026-10-26T12:00:00Z")).replace("Z", "+00:00")
    )

    class FixedDatetime(datetime):
        @classmethod
        def now(cls, tz=None):  # type: ignore[override]
            return fixed_clock.astimezone(tz) if tz else fixed_clock.replace(tzinfo=None)

    client_module = importlib.import_module("dex_connectors.hackernews.client")
    monkeypatch.setattr(client_module, "datetime", FixedDatetime)
    await conformance.replay(scenario, ADAPTER.invoke)
