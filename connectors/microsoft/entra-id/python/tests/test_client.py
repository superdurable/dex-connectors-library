# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Graph request safety and malformed provider response checks."""

import json
from types import SimpleNamespace
from typing import Any, cast

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    IdempotencyKey,
    OperationRef,
    ReauthorizationRequiredError,
    SecretString,
    StaticCredentialProvider,
)
from dex_connectors.sdk.projectconfig import ConnectionKey, CredentialMaterial, Scope
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    RejectedCredentialNotRefreshedError,
)
from dex_connectors.sdk.projectconfig.provider import CredentialProvider
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError

from dex_connectors.microsoft_entra_id import (
    AddUserToGroupInput,
    Client,
    Config,
    CreateUserInput,
    Credentials,
    GetUserInput,
)
from dex_connectors.microsoft_entra_id._generated.connection import (
    CredentialMaterialError,
    decode_credentials,
)
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


class FailingCredentials:
    """A credential provider that always fails with one error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        """Raise the configured error."""
        raise self.error


TERMINAL_CREDENTIAL_ERRORS: tuple[Exception, ...] = (
    ReauthorizationRequiredError(),
    ObjectNotFoundError(),
    ConnectionNotSelectedError(),
    CredentialBrokerRejectedError(),
    CredentialMaterialError("stored credential is malformed"),
)
TRANSIENT_CREDENTIAL_ERRORS: tuple[Exception, ...] = (
    CredentialBrokerUnavailableError(),
    RejectedCredentialNotRefreshedError(),
    ValueError("connection is not configured"),
    TimeoutError(),
    RuntimeError("unlisted"),
)


@pytest.mark.asyncio
@pytest.mark.parametrize("error", TERMINAL_CREDENTIAL_ERRORS + TRANSIENT_CREDENTIAL_ERRORS)
async def test_every_operation_classifies_credential_failures_the_same_way(error: Exception) -> None:
    """Only the four terminal classes select defect; every other failure retries without a request."""

    def answer(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request is expected")

    expected = "branch" if error in TERMINAL_CREDENTIAL_ERRORS else "retry"
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(Config(), FailingCredentials(error), http_client, local_provider_url="http://127.0.0.1:44888")
        query_call = Call(CallID("query-call"), CONNECTION, OperationRef("microsoft-entra-id", "getUser"))
        read = await client.get_user().invoke(query_call, GetUserInput(USER_ID))
        add_operation = OperationRef("microsoft-entra-id", "addUserToGroup")
        mutation_call = Call(CallID("mutation-call"), CONNECTION, add_operation)
        added = await client.add_user_to_group().invoke(mutation_call, AddUserToGroupInput(USER_ID, USER_ID))
    assert (read.kind, added.kind) == (expected, expected)


class MalformedMaterialStore:
    """Serve stored material whose fields the connector does not declare."""

    scope = Scope("project-1", "live")

    def _shared_connection(self, key: ConnectionKey) -> None:
        return None

    async def read_credential_material(self, key: ConnectionKey) -> tuple[CredentialMaterial, Any]:
        """Return malformed material through the SDK's trusted read."""
        material = CredentialMaterial({"unexpected": "value"}, None, "microsoft-oauth")
        return material, SimpleNamespace(expires_at=None)


@pytest.mark.asyncio
async def test_malformed_stored_credentials_are_terminal_behind_the_sdk_wrapper() -> None:
    """The released SDK provider hides the decoder error; the write still selects defect."""
    key = ConnectionKey("microsoft-entra-id", "entra-test")
    provider = CredentialProvider(cast(Any, MalformedMaterialStore()), key, decode_credentials)
    client = Client(Config(), provider, local_provider_url="http://127.0.0.1:44888")
    call = Call(CallID("mutation-call"), CONNECTION, OperationRef("microsoft-entra-id", "addUserToGroup"))
    added = await client.add_user_to_group().invoke(call, AddUserToGroupInput(USER_ID, USER_ID))
    assert added.kind == "branch" and added.branch == "defect"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("attempt", "branch", "methods"), [(1, "providerRejected", ["POST"]), (2, "created", ["POST", "GET"])]
)
async def test_create_user_reads_back_a_refusal_only_after_an_earlier_attempt(
    attempt: int, branch: str, methods: list[str]
) -> None:
    """A later attempt's refusal reads the name back and finds this Step's account."""
    seen: list[str] = []
    account = {
        "id": USER_ID,
        "userPrincipalName": "ada.lovelace@contoso.com",
        "accountEnabled": True,
        "onPremisesExtensionAttributes": {"extensionAttribute15": "dex-creation-key:hire-42"},
    }

    def answer(request: httpx.Request) -> httpx.Response:
        seen.append(request.method)
        if request.method == "POST":
            return httpx.Response(403, json={"error": {"code": "Authorization_RequestDenied"}})
        return httpx.Response(200, json=account)

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(Config(), CREDENTIALS, http_client, local_provider_url="http://127.0.0.1:44888")
        call = Call(
            CallID("create-call"),
            CONNECTION,
            OperationRef("microsoft-entra-id", "createUser"),
            IdempotencyKey("hire-42"),
            cast(Any, SimpleNamespace(attempt=attempt, first_attempt_at=None)),
        )
        create_input = CreateUserInput(
            user_principal_name="ada.lovelace@contoso.com", given_name="Ada", surname="Lovelace"
        )
        result = await client.create_user().invoke(call, create_input)
    assert result.kind == "branch" and result.branch == branch
    assert seen == methods


@pytest.mark.asyncio
async def test_invalid_credential_material_is_a_defect() -> None:
    """A header-unsafe token fails local checks, so it never retries."""

    def answer(request: httpx.Request) -> httpx.Response:
        raise AssertionError("no request is expected")

    unsafe = StaticCredentialProvider({CONNECTION: Credentials(access_token=SecretString("bad\ntoken"))})
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(Config(), unsafe, http_client, local_provider_url="http://127.0.0.1:44888")
        call = Call(CallID("mutation-call"), CONNECTION, OperationRef("microsoft-entra-id", "addUserToGroup"))
        added = await client.add_user_to_group().invoke(call, AddUserToGroupInput(USER_ID, USER_ID))
    assert added.kind == "branch" and added.branch == "defect"
