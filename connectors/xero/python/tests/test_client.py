# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Xero transport boundaries that do not require a Worker."""

from typing import NoReturn

import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, FailureKind, IdempotencyKey, OperationRef
from dex_connectors.sdk.credentials import ReauthorizationRequiredError
from dex_connectors.sdk.projectconfig.provider import RefreshFailedError

from dex_connectors.xero import Client, Config
from dex_connectors.xero.client import _error_summary, go_path_escape, go_query
from dex_connectors.xero.operations import _single, _wire_decimal


class _FailedCredentials:
    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, _call: Call) -> NoReturn:
        raise self.error


def _call() -> Call:
    return Call(
        CallID("test-call"),
        ConnectionRef("xero", "books"),
        OperationRef("xero", "listContacts"),
        IdempotencyKey(""),
    )


def test_go_url_escaping_preserves_only_go_safe_characters() -> None:
    assert go_path_escape("A!$&+'()*,;?/~") == "A%21$&+%27%28%29%2A%2C%3B%3F%2F~"
    assert go_query({"z": "a b~*", "a": "x/y"}) == "a=x%2Fy&z=a+b~%2A"


def test_decimal_wire_literals_keep_negative_zero_and_reject_exponents() -> None:
    assert _wire_decimal(_single(b'{"Invoices":[{"Total":-0}]}', "Invoices"), "Total") == "-0"
    with pytest.raises(ValueError, match="Total"):
        _wire_decimal(_single(b'{"Invoices":[{"Total":0.0000001}]}', "Invoices"), "Total")


def test_error_summary_uses_only_safe_tokens_and_bounded_validation_count() -> None:
    body = (b'{"Type":"ValidationException","ErrorNumber":42,"Detail":"BadRequest",'
            b'"Elements":[{"ValidationErrors":[{},{}]}]}')
    assert _error_summary(body, "secret") == (
        " [ValidationException; error 42; BadRequest; 2 validation errors]"
    )


def test_local_provider_url_rejects_non_loopback_host() -> None:
    with pytest.raises(ValueError):
        Client(Config(), _FailedCredentials(ValueError()), local_provider_url="http://example.com")


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "error,outcome,kind",
    [
        (ReauthorizationRequiredError(), "rejected", FailureKind.AUTHENTICATION),
        (RefreshFailedError("unavailable"), "retry", FailureKind.AVAILABILITY),
        (ValueError("broken"), "defect", FailureKind.AUTHENTICATION),
    ],
)
async def test_credential_errors_keep_terminal_and_transient_outcomes(
    error: Exception, outcome: str, kind: FailureKind
) -> None:
    client = Client(Config(), _FailedCredentials(error))
    try:
        exchange = await client.exchange(_call(), "listContacts", "GET", "/Contacts")
        assert exchange.outcome == outcome
        assert exchange.error is not None and exchange.error.kind == kind
    finally:
        await client.aclose()


@pytest.mark.asyncio
async def test_oauth_selects_tenant_from_connections_once() -> None:
    """Use the named organisation for later Accounting calls without repeating discovery."""
    import httpx
    from dex_connectors.sdk import SecretString, StaticCredentialProvider

    from dex_connectors.xero import Credentials, ListContactsInput

    reference = ConnectionRef("xero", "books")
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path == "/connections":
            return httpx.Response(
                200,
                json=[
                    {
                        "tenantId": "70784a63-d24b-46a9-a4db-0e70a274b056",
                        "tenantType": "ORGANISATION",
                        "tenantName": "Demo Company",
                    }
                ],
            )
        return httpx.Response(200, json={"Contacts": []})

    credentials = Credentials(auth_method_id="xero-oauth", access_token=SecretString("inert-token"))
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as transport:
        client = Client(
            Config(organisation="Demo Company"),
            StaticCredentialProvider({reference: credentials}),
            transport,
            local_provider_url="http://127.0.0.1",
        )
        for _ in range(2):
            result = await client.list_contacts().invoke(_call(), ListContactsInput())
            assert result.branch == "listed"
    assert [request.url.path for request in requests] == [
        "/connections",
        "/api.xro/2.0/Contacts",
        "/api.xro/2.0/Contacts",
    ]
    assert requests[1].headers["Xero-Tenant-Id"] == "70784a63-d24b-46a9-a4db-0e70a274b056"
    assert requests[2].headers["Xero-Tenant-Id"] == requests[1].headers["Xero-Tenant-Id"]


@pytest.mark.parametrize(
    "address,accepted",
    [
        ("é@example.com", True),
        ("a@例え.テスト", True),
        ("a@[127.0.0.1]", True),
        ("a@[IPv6:2001:db8::1]", False),
        ("a..b@example.com", False),
        ("a@exa..mple.com", False),
        ("a@example.com.", False),
    ],
)
def test_bare_email_matches_go_mail_address_edges(address: str, accepted: bool) -> None:
    """Keep Unicode and IPv4 literals while rejecting malformed dot atoms."""
    from dex_connectors.xero.operations import _valid_bare_email

    assert _valid_bare_email(address) is accepted
