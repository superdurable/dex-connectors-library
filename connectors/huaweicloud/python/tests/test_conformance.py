# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Replay Go-recorded Huawei Cloud IAM provider scenarios."""

import sys
from pathlib import Path

import dex_connectors.sdk as sdk
import pytest

import dex_connectors.huaweicloud as huaweicloud

ROOT = Path(__file__).parents[4]
sys.path.insert(0, str(ROOT / "conformance/runners/python"))
import connector_scenarios as conformance  # noqa: E402

SCENARIOS = conformance.load_scenarios(ROOT / "conformance/connectors/huaweicloud/scenarios.json")
CONNECTION = sdk.ConnectionRef("huaweicloud", "huaweicloud")
ADAPTER = conformance.PythonOperationAdapter(
    "huaweicloud",
    CONNECTION,
    huaweicloud,
    lambda http_client, recorder: huaweicloud.Client(
        huaweicloud.Config(
            max_response_bytes=16
            if recorder.scenario["name"] == "listAccessibleProjects-oversizedAuth"
            else 1048576
        ),
        conformance.static_credentials(
            CONNECTION,
            huaweicloud.Credentials(
                sdk.SecretString("example-account"),
                sdk.SecretString("example-user"),
                sdk.SecretString("synthetic-test-password"),
            ),
        )
        if recorder.scenario["name"] != "listAccessibleProjects-defect"
        else sdk.StaticCredentialProvider({}),
        http_client,
    ),
)


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["name"])
async def test_connector_conformance(scenario: dict[str, object]) -> None:
    """Match exact Go requests, branches, outputs, and retry decisions."""
    await conformance.replay(scenario, ADAPTER.invoke)
