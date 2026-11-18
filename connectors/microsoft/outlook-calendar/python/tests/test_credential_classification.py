# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Every Outlook Calendar operation classifies credential failures the same way."""

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    OperationRef,
    ReauthorizationRequiredError,
    SecretString,
    StaticCredentialProvider,
)
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    RejectedCredentialNotRefreshedError,
)
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError

from dex_connectors.microsoft_outlook_calendar import Config, Credentials
from dex_connectors.microsoft_outlook_calendar._generated.connection import (
    CredentialMaterialError,
    decode_credentials,
)
from dex_connectors.microsoft_outlook_calendar.client import Client
from dex_connectors.microsoft_outlook_calendar.models import GetEventInput, UpdateEventInput

CONNECTION = ConnectionRef("microsoft", "outlook-calendar-test")


def _sdk_decoding_failure() -> ValueError:
    """Reproduce the SDK refresh provider, which replaces the decoder error with a plain ValueError."""
    try:
        try:
            decode_credentials({"access_token": 7})
        except ValueError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error
    raise AssertionError("malformed material decoded")


TERMINAL_ERRORS: tuple[Exception, ...] = (
    ReauthorizationRequiredError(),
    ObjectNotFoundError(),
    ConnectionNotSelectedError(),
    CredentialBrokerRejectedError(),
    CredentialMaterialError("Microsoft Outlook Calendar credential access_token is required"),
    _sdk_decoding_failure(),
)
TRANSIENT_ERRORS: tuple[Exception, ...] = (
    CredentialBrokerUnavailableError(),
    RejectedCredentialNotRefreshedError(),
    ValueError("connection is not configured"),
    TimeoutError(),
    RuntimeError("unlisted"),
)


class FailingCredentials:
    """A credential provider that always fails with one error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        """Raise the configured error."""
        raise self.error


def _respond(request: httpx.Request) -> httpx.Response:
    raise AssertionError("no request is expected")


async def _outcomes(credentials: object) -> tuple[str, str]:
    async with httpx.AsyncClient(transport=httpx.MockTransport(_respond)) as http_client:
        client = Client(Config(), credentials, http_client)  # type: ignore[arg-type]
        read_operation = OperationRef("microsoft-outlook-calendar", "getEvent")
        read_call = Call(CallID("read-call"), CONNECTION, read_operation)
        read = await client.get_event().invoke(read_call, GetEventInput(event_id="event-1"))
        write_operation = OperationRef("microsoft-outlook-calendar", "updateEvent")
        write_call = Call(CallID("write-call"), CONNECTION, write_operation)
        update = UpdateEventInput(event_id="event-1", subject="Renamed")
        updated = await client.update_event().invoke(write_call, update)
    return (read.branch or read.kind, updated.branch or updated.kind)


@pytest.mark.asyncio
@pytest.mark.parametrize("error", TERMINAL_ERRORS + TRANSIENT_ERRORS)
async def test_every_operation_classifies_credential_failures_the_same_way(error: Exception) -> None:
    """Only the four terminal classes select defect; every other failure retries without a request."""
    expected = "defect" if error in TERMINAL_ERRORS else "retry"
    assert await _outcomes(FailingCredentials(error)) == (expected, expected)


@pytest.mark.asyncio
async def test_invalid_credential_material_and_an_unconfigured_connection() -> None:
    """A header-unsafe token is a defect; a static connection that is not configured retries."""
    unsafe = StaticCredentialProvider({CONNECTION: Credentials(access_token=SecretString("bad\ntoken"))})
    assert await _outcomes(unsafe) == ("defect", "defect")
    other = ConnectionRef("microsoft", "other")
    unconfigured = StaticCredentialProvider({other: Credentials(access_token=SecretString("token"))})
    assert await _outcomes(unconfigured) == ("retry", "retry")


class _LaterAttemptContext:
    attempt = 2


@pytest.mark.asyncio
@pytest.mark.parametrize("error", TERMINAL_ERRORS)
async def test_a_later_update_attempt_retries_a_terminal_credential_failure(error: Exception) -> None:
    """A later attempt may follow an applied PATCH, so defect would misreport it."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(_respond)) as http_client:
        client = Client(Config(), FailingCredentials(error), http_client)  # type: ignore[arg-type]
        operation = OperationRef("microsoft-outlook-calendar", "updateEvent")
        call = Call(CallID("write-call"), CONNECTION, operation, context=_LaterAttemptContext())  # type: ignore[arg-type]
        update = UpdateEventInput(event_id="event-1", subject="Renamed")
        updated = await client.update_event().invoke(call, update)
    assert updated.kind == "retry"
