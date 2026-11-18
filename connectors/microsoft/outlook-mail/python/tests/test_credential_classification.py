# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Every Outlook Mail operation classifies credential failures the same way."""

from datetime import UTC, datetime, timedelta

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    MutationResult,
    OperationRef,
    ReauthorizationRequiredError,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_mutation,
)
from dex_connectors.sdk.projectconfig import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    ObjectNotFoundError,
)
from dex_connectors.sdk.projectconfig.broker import RejectedCredentialNotRefreshedError

from dex_connectors.microsoft_outlook_mail import (
    Client,
    Config,
    Credentials,
    MoveMessageInput,
    SendMessageInput,
)
from dex_connectors.microsoft_outlook_mail.draft_send import Checkpoint

CONNECTION = ConnectionRef("microsoft", "outlook-mail")
TERMINAL_ERRORS: tuple[Exception, ...] = (
    ReauthorizationRequiredError(),
    ObjectNotFoundError(),
    ConnectionNotSelectedError(),
    CredentialBrokerRejectedError(),
)
TRANSIENT_ERRORS: tuple[Exception, ...] = (
    CredentialBrokerUnavailableError(),
    RejectedCredentialNotRefreshedError(),
    ValueError("connection is not configured"),
    TimeoutError(),
    RuntimeError("unlisted"),
)


class AttemptContext:
    """Return a fixed earlier heartbeat, as Dex supplies it to a later attempt."""

    def __init__(self, attempt: int, checkpoint: Checkpoint | None) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value: object = checkpoint

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None

    def get_last_heartbeat_value(self, value_type: object) -> object:
        """Return the earlier heartbeat."""
        return self.heartbeat_value

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat."""
        self.heartbeat_value = value


class FailingCredentials:
    """A credential provider that always fails with one error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        """Raise the configured error."""
        raise self.error


def _respond(request: httpx.Request) -> httpx.Response:
    raise AssertionError("no request is expected")


async def _send(credentials: object, context: AttemptContext) -> str:
    async with httpx.AsyncClient(transport=httpx.MockTransport(_respond)) as transport:
        client = Client(Config(), credentials, transport)  # type: ignore[arg-type]
        call = Call(
            CallID("74390522-858d-5ed6-bdf7-04e979ba9e4f"), CONNECTION,
            OperationRef("outlook-mail", "sendMessage"), context=context, max_sends=3,
        )
        send_input = SendMessageInput(to=["a@example.com"], subject="Test", text="Body")
        try:
            result = await run_mutation(client.send_message(), call, send_input)
        except RetryError:
            return "retry"
    assert isinstance(result, MutationResult)
    return result.branch


@pytest.mark.asyncio
@pytest.mark.parametrize("error", TERMINAL_ERRORS + TRANSIENT_ERRORS)
async def test_send_message_classifies_every_credential_failure(error: Exception) -> None:
    """Only the four terminal classes end the send; every other failure retries."""
    is_terminal = error in TERMINAL_ERRORS
    dispatched = Checkpoint(phase="sendDispatched", draft_message_id="draft1", send_count=1)
    first = await _send(FailingCredentials(error), AttemptContext(1, None))
    later = await _send(FailingCredentials(error), AttemptContext(2, dispatched))
    assert (first, later) == (("providerRejected", "uncertain") if is_terminal else ("retry", "retry"))


@pytest.mark.asyncio
async def test_a_static_connection_that_is_not_configured_retries() -> None:
    """The static provider's plain ValueError is a transient failure."""
    other = StaticCredentialProvider(
        {ConnectionRef("microsoft", "other"): Credentials(access_token=SecretString("access-token"))}
    )
    assert await _send(other, AttemptContext(1, None)) == "retry"


@pytest.mark.asyncio
async def test_credential_material_that_fails_local_checks_is_a_defect() -> None:
    """A header-unsafe token is invalid material, so it never retries."""
    unsafe = StaticCredentialProvider({CONNECTION: Credentials(access_token=SecretString("bad\ntoken"))})
    assert await _send(unsafe, AttemptContext(1, None)) == "defect"


@pytest.mark.asyncio
@pytest.mark.parametrize("error", TERMINAL_ERRORS + TRANSIENT_ERRORS)
async def test_move_message_classifies_every_credential_failure(error: Exception) -> None:
    """The repeat-safe move keeps its rejection branch for a terminal failure and retries the rest."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(_respond)) as transport:
        client = Client(Config(), FailingCredentials(error), transport)  # type: ignore[arg-type]
        call = Call(
            CallID("74390522-858d-5ed6-bdf7-04e979ba9e50"), CONNECTION,
            OperationRef("outlook-mail", "moveMessage"), context=AttemptContext(1, None),
        )
        move_input = MoveMessageInput("AAMkAD", "archive")
        try:
            branch = (await run_mutation(client.move_message(), call, move_input)).branch
        except RetryError:
            branch = "retry"
    assert branch == ("providerRejected" if error in TERMINAL_ERRORS else "retry")
