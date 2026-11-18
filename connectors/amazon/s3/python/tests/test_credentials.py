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
)
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
)
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError

from dex_connectors.amazon_s3 import Client, Config, Credentials
from dex_connectors.amazon_s3.models import ListObjectsInput, PutObjectInput


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "is_terminal"),
    [
        (ReauthorizationRequiredError(), True),
        (ObjectNotFoundError(), True),
        (ConnectionNotSelectedError(), True),
        (CredentialBrokerRejectedError(), True),
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
