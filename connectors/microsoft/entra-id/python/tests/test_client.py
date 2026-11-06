# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Graph request safety and malformed provider response checks."""

import json

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

from dex_connectors.microsoft_entra_id import Client, Config, Credentials, GetUserInput
from dex_connectors.microsoft_entra_id.client import convert_user, odata_query, path_escape, query_escape

USER_ID = "11111111-2222-4333-8444-555555555555"
CONNECTION = ConnectionRef("microsoft", "entra-test")
CREDENTIALS = StaticCredentialProvider({CONNECTION: Credentials(access_token=SecretString("inert-token"))})


def test_path_and_query_escape_match_go() -> None:
    """Keep Graph IDs and OData values byte-for-byte compatible with Go."""
    assert path_escape("$a!b'c()*+,;?/@example.com") == "$a%21b%27c%28%29%2A+%2C%3B%3F%2F@example.com"
    assert query_escape("a b~*+") == "a%20b~%2A%2B"


@pytest.mark.parametrize(
    "values, expected",
    [
        ({"$filter": "displayName eq 'a b~*'", "$select": "id,userPrincipalName"},
         "$filter=displayName%20eq%20%27a%20b~%2A%27&$select=id%2CuserPrincipalName"),
        ({"$select": "a b~*+!'()"}, "$select=a%20b~%2A%2B%21%27%28%29"),
    ],
)
def test_odata_query_matches_go(values: dict[str, str], expected: str) -> None:
    """Keep the Go connector's literal OData keys and percent-encoded spaces."""
    assert odata_query(values) == expected


def test_graph_user_wrong_typed_fields_are_not_coerced() -> None:
    """A Graph schema mismatch selects invalidResponse rather than a false account."""
    for bad_field, bad_value in (
        ("displayName", 7),
        ("accountEnabled", "true"),
        ("onPremisesExtensionAttributes", {"extensionAttribute15": 7}),
    ):
        source = {"id": USER_ID, "userPrincipalName": "ada@contoso.com", "accountEnabled": True}
        source[bad_field] = bad_value
        with pytest.raises(ValueError):
            convert_user(source)


@pytest.mark.asyncio
async def test_error_status_survives_small_success_body_limit() -> None:
    """A large 429 error body still retries by status with the provider delay."""

    def answer(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            429,
            headers={"Retry-After": "7"},
            content=json.dumps({"error": {"code": "TooManyRequests", "padding": "x" * 100}}),
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(
            Config(max_response_bytes=2),
            CREDENTIALS,
            http_client,
            local_provider_url="http://127.0.0.1:44888",
        )
        call = Call(CallID("test-call"), CONNECTION, OperationRef("microsoft-entra-id", "getUser"))
        attempt = await client.get_user().invoke(call, GetUserInput(USER_ID))
    assert attempt.kind == "retry"
    assert attempt.failure is not None
    assert attempt.failure.kind.value == "RATE_LIMIT"
    assert attempt.retry_after_seconds == 7
