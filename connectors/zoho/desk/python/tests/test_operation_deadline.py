# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check the shared Desk operation deadline before a provider send."""

import asyncio

import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, OperationRef, SecretString

from dex_connectors.zoho_desk.client import Client, Config, Credentials


class NeverResolvedCredentials:
    async def resolve(self, call: Call) -> Credentials:
        raise AssertionError(f"unexpected credential resolution for {call.id}")


@pytest.mark.asyncio
async def test_expired_operation_deadline_does_not_send() -> None:
    client = Client(Config(org_id="2389290"), NeverResolvedCredentials())
    call = Call(
        CallID("deadline-test"), ConnectionRef("zoho", "desk"), OperationRef("zoho-desk", "getTicket")
    )
    client._operation_deadlines[str(call.id)] = asyncio.get_running_loop().time() - 1
    credentials = Credentials(access_token=SecretString("synthetic-access"))
    result = await client.exchange(
        call, "getTicket", credentials, "https://desk.zoho.com/api/v1", "GET", "/tickets/1"
    )
    assert result.outcome == "not_sent"
    assert result.status == 0
    await client.aclose()
