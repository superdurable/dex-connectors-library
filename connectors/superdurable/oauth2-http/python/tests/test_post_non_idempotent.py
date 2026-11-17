# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""postNonIdempotent sends one POST per Step execution and never resends a possible send."""

from collections.abc import Callable
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.superdurable_oauth2_http as oauth2_http
from dex_connectors.superdurable_oauth2_http._generated.definitions import POST_NON_IDEMPOTENT_DEFINITION

CONNECTION = sdk.ConnectionRef("oauth2-http", "oauth2-http")
CALL_ID = sdk.CallID("659e7b4f-b114-59f6-98d1-17c1143ef228")


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(self) -> None:
        self.attempt = 1
        self.run_id = "run"
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.marker: Marker | None = None
        self.is_unreadable = False

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        if self.is_unreadable:
            raise RuntimeError("heartbeat unavailable")
        return self.marker is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last marker."""
        return self.marker

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's marker."""
        self.marker = value if isinstance(value, Marker) else None


async def post(
    answer: Callable[[httpx.Request], httpx.Response], context: AttemptContext, requests: list[httpx.Request]
) -> sdk.MutationResult[oauth2_http.SendRequestOutput] | sdk.RetryError:
    """Run one Execute attempt of postNonIdempotent."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return answer(request)

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = oauth2_http.Client(
            oauth2_http.Config(
                authorization_endpoint="http://127.0.0.1:12345/auth",
                token_endpoint="http://127.0.0.1:12345/token",
                scopes=("profile",),
                api_base_url="http://127.0.0.1:12345/v1",
            ),
            sdk.StaticCredentialProvider(
                {
                    CONNECTION: oauth2_http.Credentials(
                        "client",
                        sdk.SecretString("client-secret"),
                        sdk.SecretString("access-token"),
                        sdk.SecretString(""),
                    )
                }
            ),
            transport,
        )
        call = sdk.Call(
            CALL_ID,
            CONNECTION,
            sdk.OperationRef("oauth2-http", "postNonIdempotent"),
            context=context,  # type: ignore[arg-type]
            max_sends=1,
        )
        try:
            order = oauth2_http.PostNonIdempotentInput(path="/orders", body="{}")
            return await sdk.run_mutation(client.post_non_idempotent(), call, order)
        except sdk.RetryError as error:
            return error


def test_post_non_idempotent_is_unsafe_and_fenced() -> None:
    assert POST_NON_IDEMPOTENT_DEFINITION.repeat_on_unknown == sdk.RepeatOnUnknown.UNSAFE


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [302, 408, 409, 500, 502])
async def test_unknown_outcome_selects_uncertain_and_is_never_sent_again(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    first = await post(lambda _: httpx.Response(status), context, requests)
    assert isinstance(first, sdk.MutationResult) and first.branch == "uncertain"
    assert context.marker is not None and context.marker.state == "claimed"
    context.attempt = 2
    second = await post(lambda _: httpx.Response(201), context, requests)
    assert isinstance(second, sdk.MutationResult) and second.branch == "uncertain"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_lost_answer_after_send_selects_uncertain() -> None:
    def lost(request: httpx.Request) -> httpx.Response:
        raise httpx.ReadError("connection reset", request=request)

    result = await post(lost, AttemptContext(), [])
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"


@pytest.mark.asyncio
async def test_definite_refusal_records_can_resend_and_selects_rejected() -> None:
    context = AttemptContext()
    result = await post(lambda _: httpx.Response(422, json={"error": "invalid"}), context, [])
    assert isinstance(result, sdk.MutationResult) and result.branch == "rejected"
    assert context.marker is not None and context.marker.state == "canResend"


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [429, 503])
async def test_unprocessed_post_returns_retry_and_is_sent_on_the_next_attempt(status: int) -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    assert isinstance(await post(lambda _: httpx.Response(status), context, requests), sdk.RetryError)
    assert context.marker is not None and context.marker.state == "canResend"
    context.attempt = 2
    result = await post(lambda _: httpx.Response(201), context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "completed"
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_unreadable_checkpoint_returns_retry_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.is_unreadable = True
    assert isinstance(await post(lambda _: httpx.Response(201), context, requests), sdk.RetryError)
    assert requests == []


@pytest.mark.asyncio
async def test_missing_checkpoint_after_the_first_attempt_selects_uncertain_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 2
    result = await post(lambda _: httpx.Response(201), context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert requests == []
