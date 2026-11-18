# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One credential classification: only listed failures are terminal; the rest retry."""

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    MutationResult,
    OperationRef,
    QueryResult,
    ReauthorizationRequiredError,
    RetryError,
    run_query,
)
from dex_connectors.sdk.projectconfig import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
    ObjectNotFoundError,
)
from dex_connectors.sdk.projectconfig.broker import RejectedCredentialNotRefreshedError
from test_post_resend import CALL_ID, REFERENCE, SERVER_FAILURE, AttemptContext, SequencedCredentials, post

from dex_connectors.slack import Client, Config, Credentials
from dex_connectors.slack._generated.connection import CredentialMaterialError
from dex_connectors.slack.models import ListChannelsInput


def sdk_wrapped_material_error() -> ValueError:
    """Build the error the SDK project provider raises over a decoder failure."""
    try:
        try:
            raise CredentialMaterialError("Slack credential bot_token is required")
        except ValueError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error


CASES: list[tuple[str, Exception, bool]] = [
    ("malformed stored material", CredentialMaterialError("Slack credential bot_token is required"), True),
    ("SDK-wrapped malformed material", sdk_wrapped_material_error(), True),
    ("reauthorization", ReauthorizationRequiredError(), True),
    ("object not found", ObjectNotFoundError(), True),
    ("connection not selected", ConnectionNotSelectedError(), True),
    ("broker rejected", CredentialBrokerRejectedError(), True),
    ("broker unavailable", CredentialBrokerUnavailableError(), False),
    ("rejected credential not refreshed", RejectedCredentialNotRefreshedError(), False),
    ("static not configured", ValueError("connection is not configured"), False),
    ("unlisted", RuntimeError("credential store hiccup"), False),
]


class FailingAfterCredentials(SequencedCredentials):
    """Resolve valid credentials for the first resolutions, then raise one error."""

    def __init__(self, error: Exception, valid_resolutions: int) -> None:
        super().__init__(["bot-token"])
        self.error = error
        self.valid_resolutions = valid_resolutions

    async def resolve(self, call: Call) -> Credentials:
        """Raise the configured error once the valid resolutions are used."""
        if self.resolutions >= self.valid_resolutions:
            raise self.error
        return await super().resolve(call)


@pytest.mark.asyncio
@pytest.mark.parametrize(("name", "error", "is_terminal"), CASES)
async def test_credential_failure_before_any_send(name: str, error: Exception, is_terminal: bool) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    credentials = FailingAfterCredentials(error, 0)
    result = await post(lambda _: SERVER_FAILURE, context, requests, credentials=credentials)
    assert not requests
    if is_terminal:
        assert isinstance(result, MutationResult) and result.branch == "defect", name
    else:
        assert isinstance(result, RetryError), name
        assert result.failure.kind == "AVAILABILITY"


@pytest.mark.asyncio
@pytest.mark.parametrize(("name", "error", "is_terminal"), CASES)
async def test_credential_failure_after_possible_send(name: str, error: Exception, is_terminal: bool) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    credentials = FailingAfterCredentials(error, 1)
    first = await post(lambda _: SERVER_FAILURE, context, requests, credentials=credentials)
    assert isinstance(first, RetryError)
    context.attempt = 2
    result = await post(lambda _: SERVER_FAILURE, context, requests, credentials=credentials)
    assert len(requests) == 1
    if is_terminal:
        assert isinstance(result, MutationResult) and result.branch == "uncertain", name
    else:
        assert isinstance(result, RetryError), name
        assert context.marker is not None and context.marker.state != "notSent"


@pytest.mark.asyncio
@pytest.mark.parametrize(("name", "error", "is_terminal"), CASES)
async def test_credential_failure_in_query(name: str, error: Exception, is_terminal: bool) -> None:
    client = Client(Config(), FailingAfterCredentials(error, 0))
    call = Call(CALL_ID, REFERENCE, OperationRef("slack", "listChannels"))
    try:
        result = await run_query(client.list_channels(), call, ListChannelsInput())
    except RetryError:
        assert not is_terminal, name
        return
    assert is_terminal, name
    assert isinstance(result, QueryResult) and result.branch == "defect"
