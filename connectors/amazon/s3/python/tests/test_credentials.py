# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Classify credential failures as terminal or transient before any request."""

from datetime import UTC, datetime

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    IdempotencyKey,
    OperationRef,
    ReauthorizationRequiredError,
    SecretString,
)
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
)
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError

from dex_connectors.amazon_s3 import Client, Config, Credentials
from dex_connectors.amazon_s3._generated.connection import CredentialMaterialError, _ProjectCredentialSource
from dex_connectors.amazon_s3.models import ListObjectsInput, PutObjectInput

CALL = Call(
    CallID("5908a179-2147-5839-9c64-529cd36f77d7"),
    ConnectionRef("amazon-s3", "s3-test"),
    OperationRef("amazon-s3", "putObject"),
    IdempotencyKey("stable-key"),
)
CONFIG = Config(endpoint="http://127.0.0.1:12345", default_bucket="acme-reports", region="eu-west-1")


def _sdk_wrapped_material_error() -> ValueError:
    """Build the ValueError that the SDK project provider raises over a decoder failure."""
    try:
        try:
            raise CredentialMaterialError("Amazon S3 credential access_key_id is required")
        except CredentialMaterialError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error
    raise AssertionError("unreachable")


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "is_terminal"),
    [
        (ReauthorizationRequiredError(), True),
        (ObjectNotFoundError(), True),
        (ConnectionNotSelectedError(), True),
        (CredentialBrokerRejectedError(), True),
        (CredentialMaterialError("Amazon S3 credential access_key_id is required"), True),
        (_sdk_wrapped_material_error(), True),
        (CredentialBrokerUnavailableError(), False),
        (ValueError("connection is not configured"), False),
        (RuntimeError("unlisted"), False),
    ],
)
async def test_credential_failures_select_defect_only_when_terminal(
    error: Exception, is_terminal: bool
) -> None:
    requests: list[httpx.Request] = []

    class FailingCredentials:
        async def resolve(self, call: Call) -> Credentials:
            raise error

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200)

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(
            Config(endpoint="http://127.0.0.1:12345", default_bucket="acme-reports", region="eu-west-1"),
            FailingCredentials(),
            http_client,
            lambda: datetime(2026, 10, 25, 9, tzinfo=UTC),
        )
        call = Call(
            CallID("5908a179-2147-5839-9c64-529cd36f77d7"),
            ConnectionRef("amazon-s3", "s3-test"),
            OperationRef("amazon-s3", "putObject"),
            IdempotencyKey("stable-key"),
        )
        put = await client.put_object().invoke(
            call, PutObjectInput(key="report.txt", content_type="text/plain", text_content="hello")
        )
        listed = await client.list_objects().invoke(call, ListObjectsInput())
    for attempt in (put, listed):
        if is_terminal:
            assert attempt.branch == "defect"
        else:
            assert attempt.kind == "retry"
            assert attempt.failure is not None and attempt.failure.kind == FailureKind.AVAILABILITY
    assert requests == []


@pytest.mark.asyncio
async def test_malformed_stored_credentials_from_the_project_source_are_terminal() -> None:
    class MalformedProject:
        async def resolve_credential(self, key: object, call_id: str) -> object:
            return {"access_key_id": 42, "secret_access_key": "secret"}

    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200)

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(
            CONFIG,
            _ProjectCredentialSource(MalformedProject(), object()),  # type: ignore[arg-type]
            http_client,
            lambda: datetime(2026, 10, 25, 9, tzinfo=UTC),
        )
        put = await client.put_object().invoke(
            CALL, PutObjectInput(key="report.txt", content_type="text/plain", text_content="hello")
        )
    assert put.branch == "defect"
    assert requests == []


@pytest.mark.asyncio
async def test_start_session_keeps_its_released_none_result_for_transient_failures() -> None:
    class UnavailableCredentials:
        async def resolve(self, call: Call) -> Credentials:
            raise CredentialBrokerUnavailableError()

    async with httpx.AsyncClient() as http_client:
        client = Client(CONFIG, UnavailableCredentials(), http_client)
        assert await client.start_session(CALL) is None


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [405, 409, 425, 451])
async def test_put_object_retries_unlisted_statuses_as_unknown(status: int) -> None:
    class StaticCredentials:
        async def resolve(self, call: Call) -> Credentials:
            return Credentials(SecretString("AKIAEXAMPLE"), SecretString("secret"), SecretString(""))

    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda request: httpx.Response(status))
    ) as http_client:
        client = Client(
            CONFIG, StaticCredentials(), http_client, lambda: datetime(2026, 10, 25, 9, tzinfo=UTC)
        )
        put = await client.put_object().invoke(
            CALL, PutObjectInput(key="report.txt", content_type="text/plain", text_content="hello")
        )
    assert put.kind == "retry"
