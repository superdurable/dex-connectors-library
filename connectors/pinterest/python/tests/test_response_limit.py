# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Oversized Pinterest bodies keep their HTTP status classification."""

from uuid import uuid4

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_query,
)

import dex_connectors.pinterest as pinterest
from dex_connectors.pinterest._generated.definitions import LIST_PINS_DEFINITION

CONNECTION = ConnectionRef("pinterest", "pinterest")
OVERSIZED_BODY = b'{"code":8,"message":"response body larger than the configured limit"}'


def make_client(status: int) -> pinterest.Client:
    return pinterest.Client(
        pinterest.Config(base_url="http://localhost:8848", max_response_bytes=16),
        StaticCredentialProvider(
            {
                CONNECTION: pinterest.Credentials(
                    "client-id",
                    SecretString("client-secret"),
                    SecretString("access-token"),
                    SecretString("refresh-token"),
                )
            }
        ),
        httpx.AsyncClient(
            transport=httpx.MockTransport(lambda _: httpx.Response(status, content=OVERSIZED_BODY))
        ),
    )


def list_pins_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_PINS_DEFINITION.operation)


@pytest.mark.parametrize(("status", "kind"), [(429, FailureKind.RATE_LIMIT), (503, FailureKind.AVAILABILITY)])
async def test_oversized_retry_status_body_retries(status: int, kind: FailureKind) -> None:
    """A 429 or 5xx body over the limit still retries with the status failure kind."""
    client = make_client(status)
    with pytest.raises(RetryError) as raised:
        await run_query(client.list_pins(), list_pins_call(), pinterest.ListPinsInput())
    assert raised.value.failure.kind == kind


async def test_oversized_success_body_is_invalid_response() -> None:
    """A 200 body over the limit selects invalidResponse."""
    result = await run_query(make_client(200).list_pins(), list_pins_call(), pinterest.ListPinsInput())
    assert result.branch == "invalidResponse"
    assert result.failure is not None
    assert result.failure.kind == FailureKind.RESPONSE_TOO_LARGE
