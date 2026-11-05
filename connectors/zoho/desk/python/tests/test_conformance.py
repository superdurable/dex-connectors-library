# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Zoho Desk requests and Dex outcomes."""

import copy
import sys
from pathlib import Path
from typing import Any

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.zoho_desk as desk

ROOT = Path(__file__).parents[5]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
from connector_scenarios import PythonOperationAdapter, load_scenarios, replay  # type: ignore[import-not-found]  # noqa: E402

SCENARIOS = load_scenarios(ROOT / "conformance/connectors/zoho/desk/scenarios.json")
CONNECTION = sdk.ConnectionRef("zoho", "zoho-desk-test")
CREDENTIALS = desk.Credentials(
    auth_method_id="zoho-us-oauth",
    oauth_client_id="1000.GMB0YULZHJK411248S8I5GZ4CHUEX0",
    oauth_client_secret=sdk.SecretString("zoho-client-secret"),
    access_token=sdk.SecretString("1000.zohoDeskTestAccessToken0123456789"),
    refresh_token=sdk.SecretString("1000.zohoDeskTestRefreshToken"),
)
ADAPTER = PythonOperationAdapter(
    "zoho-desk",
    CONNECTION,
    desk,
    lambda http_client, _: desk.Client(
        desk.Config(org_id="2389290"),
        sdk.StaticCredentialProvider({CONNECTION: CREDENTIALS}),
        http_client,
        "http://127.0.0.1:46301/api/v1",
    ),
    with_context=True,
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Check provider exchanges, retries, branches, receipts, and heartbeat writes."""

    async def invoke(scenario_value: dict[str, Any], recorder: Any, context: Any) -> object:
        if scenario_value["operation"] == "createTicket" and isinstance(
            scenario_value["input"].get("contact"), dict
        ):
            scenario_value = copy.deepcopy(scenario_value)
            scenario_value["input"]["contact"] = desk.TicketContactInput(
                **{
                    "email": scenario_value["input"]["contact"].get("email", ""),
                    "first_name": scenario_value["input"]["contact"].get("firstName", ""),
                    "last_name": scenario_value["input"]["contact"].get("lastName", ""),
                }
            )
        return await ADAPTER.invoke(scenario_value, recorder, context)

    await replay(scenario, invoke, "/api/v1")
