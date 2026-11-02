# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check local credential, address, and payload safety before provider dispatch."""

from dataclasses import replace

import dex_connectors.sdk as sdk
import pytest

from dex_connectors.reamaze import Client, Config, Credentials, SearchConversationsInput
from dex_connectors.reamaze.client import (
    CredentialsUnusableError,
    conversation_path,
    is_bare_email_address,
)
from dex_connectors.reamaze.operations import _search_query

CONNECTION = sdk.ConnectionRef("reamaze", "test")


@pytest.mark.parametrize(
    ("address", "accepted"),
    [
        ("agent@example.com", True),
        ("agent+desk@example.com", True),
        ("Jane <agent@example.com>", False),
        ("agent@example.com,other@example.com", False),
        ("agent@example.com:bad", False),
        ("not-an-email", False),
    ],
)
def test_bare_email_matches_basic_auth_constraints(address: str, accepted: bool) -> None:
    """Reject display names and delimiters that would change HTTP Basic identity."""
    assert is_bare_email_address(address) is accepted


def test_conversation_path_escapes_and_rejects_no_slugs() -> None:
    """Keep path escaping compatible with Go's url.PathEscape."""
    assert conversation_path("double-charge") == "/conversations/double-charge"
    assert conversation_path("a/b") == "/conversations/a%2Fb"


def test_search_query_keeps_provider_defaults_and_optional_values() -> None:
    """Send a first page even when every filter is blank."""
    assert _search_query(SearchConversationsInput()) == {"page": "1"}
    assert _search_query(SearchConversationsInput(page=2, tags=("billing",))) == {
        "page": "2",
        "tag": "billing",
    }


@pytest.mark.asyncio
async def test_unusable_credentials_fail_before_request() -> None:
    """A malformed token requires connection repair and remains out of errors."""
    credentials = Credentials("agent@example.com", sdk.SecretString("bad token"))
    client = Client(Config("acme"), sdk.StaticCredentialProvider({CONNECTION: credentials}))
    call = sdk.Call(
        sdk.CallID("55555555-5555-4555-8555-555555555555"),
        CONNECTION,
        sdk.OperationRef("reamaze", "searchConversations"),
        sdk.IdempotencyKey(""),
    )
    try:
        with pytest.raises(CredentialsUnusableError, match="connection credentials are unavailable"):
            await client.resolve_credentials(call)
    finally:
        await client.aclose()


def test_brand_validation_and_response_limit() -> None:
    """Reject local configuration errors before a provider client is used."""
    provider = sdk.StaticCredentialProvider(
        {CONNECTION: Credentials("agent@example.com", sdk.SecretString("token"))}
    )
    with pytest.raises(ValueError, match="brand"):
        Client(Config("BadBrand"), provider)
    with pytest.raises(ValueError, match="response limit"):
        Client(replace(Config("acme"), max_response_bytes=-1), provider)


@pytest.mark.asyncio
async def test_query_distinguishes_transient_and_unusable_credentials() -> None:
    """Retry storage lookup errors and terminate a malformed saved credential."""

    class FailingCredentials:
        async def resolve(self, call: sdk.Call) -> Credentials:
            raise OSError("transient credential store outage")

    call = sdk.Call(
        sdk.CallID("55555555-5555-4555-8555-555555555555"),
        CONNECTION,
        sdk.OperationRef("reamaze", "searchConversations"),
        sdk.IdempotencyKey(""),
    )
    transient_client = Client(Config("acme"), FailingCredentials())
    unusable_client = Client(
        Config("acme"),
        sdk.StaticCredentialProvider(
            {CONNECTION: Credentials("agent@example.com", sdk.SecretString("bad token"))}
        ),
    )
    try:
        transient = await transient_client.search_conversations().invoke(call, SearchConversationsInput())
        assert transient.kind == "retry"
        assert transient.failure is not None
        assert transient.failure.kind == sdk.FailureKind.AVAILABILITY
        unusable = await unusable_client.search_conversations().invoke(call, SearchConversationsInput())
        assert unusable.branch == "defect"
        assert unusable.failure is not None
        assert unusable.failure.kind == sdk.FailureKind.AUTHENTICATION
    finally:
        await transient_client.aclose()
        await unusable_client.aclose()
