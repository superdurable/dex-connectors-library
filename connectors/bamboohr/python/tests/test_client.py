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


@pytest.mark.parametrize(
    ("api_base_url", "is_allowed"),
    [
        ("http://localhost:23500/api/v1", True),
        ("http://127.1.2.3:23500/api/v1", True),
        ("http://[::1]:23500/api/v1", True),
        ("http://example.com/api/v1", False),
    ],
)
def test_client_base_url_matches_go_loopback_rules(api_base_url: str, is_allowed: bool) -> None:
    """Only HTTPS and local loopback HTTP endpoints can receive credentials."""
    credentials = StaticCredentialProvider({CONNECTION: Credentials(SecretString("inert-key"))})
    if is_allowed:
        Client(Config("acme"), credentials, api_base_url=api_base_url)
    else:
        with pytest.raises(ValueError, match="HTTPS"):
            Client(Config("acme"), credentials, api_base_url=api_base_url)


@pytest.mark.parametrize(
    ("error_body", "expected_code"),
    [
        ({"error": {"code": "PRIMARY"}, "code": "FALLBACK"}, "PRIMARY"),
        ({"error": {"code": "123"}, "code": "FALLBACK"}, "FALLBACK"),
    ],
)
def test_error_code_uses_first_usable_pointer(
    error_body: dict[str, object], expected_code: str
) -> None:
    """Each JSON pointer is checked independently in Go pointer order."""
    credentials = StaticCredentialProvider({CONNECTION: Credentials(SecretString("inert-key"))})
    client = Client(Config("acme"), credentials)
    response = httpx.Response(400, json=error_body)
    exchange = client._classify_status("getEmployee", 400, response.headers, response.content, "inert-key")
    assert exchange.failure is not None
    assert exchange.failure.message.endswith(f" [{expected_code}]")


@pytest.mark.asyncio
async def test_oversized_error_body_does_not_extract_partial_code() -> None:
    """Go drops an oversized body before classifying its non-success status."""
    credentials = StaticCredentialProvider({CONNECTION: Credentials(SecretString("inert-key"))})

    def answer(request: httpx.Request) -> httpx.Response:
        return httpx.Response(400, json={"code": "EARLY", "padding": "x" * 100})

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(Config("acme", max_response_bytes=32), credentials, http_client)
        call = Call(
            CallID("9d50715c-760e-4867-9fcb-711fd893ce88"),
            CONNECTION,
            OperationRef("bamboohr", "getEmployee"),
        )
        exchange = await client.exchange(
            call, Credentials(SecretString("inert-key")), "getEmployee", "GET", "/employees/123"
        )
    assert exchange.failure is not None
    assert exchange.failure.message == "BambooHR rejected the request (HTTP 400)"


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
