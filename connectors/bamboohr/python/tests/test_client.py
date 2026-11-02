# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check BambooHR client construction and credential-safe response handling."""

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.bamboohr import Client, Config, Credentials, GetEmployeeInput

CONNECTION = ConnectionRef("bamboohr", "bamboohr-test")


def test_client_rejects_unsafe_company_domain() -> None:
    """The domain must not redirect a credentialed request to another host."""
    credentials = StaticCredentialProvider({CONNECTION: Credentials(SecretString("inert-key"))})
    with pytest.raises(ValueError, match="companyDomain"):
        Client(Config("acme.example.com"), credentials)


@pytest.mark.asyncio
async def test_reflected_key_selects_invalid_response_without_persisting_key() -> None:
    """A provider echo must never enter a Dex result or failure message."""
    key = "inert-test-key"
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, json={"id": "123", "firstName": key})

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        credentials = StaticCredentialProvider({CONNECTION: Credentials(SecretString(key))})
        client = Client(Config("acme"), credentials, http_client, "http://127.0.0.1:22180/api/v1")
        call = Call(
            CallID("472485d6-a578-50af-a4d2-124cf93b188f"),
            CONNECTION,
            OperationRef("bamboohr", "getEmployee"),
        )
        result = await client.get_employee().invoke(call, GetEmployeeInput("123", ("firstName",)))
        assert len(requests) == 1
        assert result.branch == "invalidResponse"
        assert result.failure is not None and key not in result.failure.message
        assert result.value is not None and key not in repr(result.value)
