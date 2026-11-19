# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""postNonIdempotent sends one POST per Step execution and never resends a possible send."""

from collections.abc import Callable
from datetime import UTC, datetime, timedelta

import dex_connectors.sdk as sdk
import httpx
import pytest
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
)
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError
from dex_connectors.sdk.singledispatch import Marker

import dex_connectors.superdurable_oauth2_http as oauth2_http
from dex_connectors.superdurable_oauth2_http._generated.connection import CredentialMaterialError
from dex_connectors.superdurable_oauth2_http._generated.definitions import POST_NON_IDEMPOTENT_DEFINITION

CONNECTION = sdk.ConnectionRef("oauth2-http", "oauth2-http")
CALL_ID = sdk.CallID("659e7b4f-b114-59f6-98d1-17c1143ef228")


def _sdk_wrapped_material_error() -> ValueError:
    """Build the error the SDK project provider raises over a decoder failure."""
    try:
        try:
            raise CredentialMaterialError("OAuth2 HTTP credential access_token is required")
        except ValueError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error


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
    answer: Callable[[httpx.Request], httpx.Response],
    context: AttemptContext,
    requests: list[httpx.Request],
    access_token: str = "access-token",
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
                        sdk.SecretString(access_token),
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


class _ExpiredAttemptContext(AttemptContext):
    """Has no attempt time left, and loses every checkpoint write."""

    def __init__(self) -> None:
        super().__init__()
        self.deadline = datetime.now(UTC) - timedelta(seconds=1)

    async def heartbeat(self, value: object = None) -> None:
        """Lose the checkpoint write."""
        raise RuntimeError("heartbeat unavailable")


@pytest.mark.asyncio
async def test_no_time_left_returns_retry_without_sending() -> None:
    requests: list[httpx.Request] = []
    result = await post(lambda _: httpx.Response(201), _ExpiredAttemptContext(), requests)
    assert requests == []
    assert isinstance(result, sdk.RetryError)
    assert result.failure.kind == sdk.FailureKind.AVAILABILITY


@pytest.mark.asyncio
async def test_unreadable_checkpoint_returns_retry_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.is_unreadable = True
    assert isinstance(await post(lambda _: httpx.Response(201), context, requests), sdk.RetryError)
    assert requests == []


@pytest.mark.asyncio
async def test_credential_failure_after_a_possible_send_never_records_not_sent() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    # A canResend marker that still counts one possibly effective send keeps a send allowed.
    seeded = Marker(CALL_ID, 2, "canResend", send_count=1)
    context.marker = seeded
    context.attempt = 3
    result = await post(lambda _: httpx.Response(201), context, requests, access_token="")
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert context.marker is seeded, "notSent would hide the earlier possible POST"
    assert requests == []


@pytest.mark.asyncio
async def test_checkpoint_that_counts_an_earlier_send_selects_uncertain_with_usable_credentials() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.marker = Marker(CALL_ID, 2, "canResend", send_count=1)
    context.attempt = 3
    result = await post(lambda _: httpx.Response(201), context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain", "the send cap is used"
    assert requests == []


class UnavailableBroker:
    """Fail every resolution as a temporarily unavailable credential broker does."""

    async def resolve(self, call: sdk.Call) -> oauth2_http.Credentials:
        raise CredentialBrokerUnavailableError("credential broker is unavailable")

    async def resolve_with_refresh(self, call: sdk.Call, driver: object) -> oauth2_http.Credentials:
        raise CredentialBrokerUnavailableError("credential broker is unavailable")


class FailingCredentials(UnavailableBroker):
    """Fail every resolution with error."""

    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: sdk.Call) -> oauth2_http.Credentials:
        raise self.error

    async def resolve_with_refresh(self, call: sdk.Call, driver: object) -> oauth2_http.Credentials:
        raise self.error


class MissingConnection(FailingCredentials):
    """Fail every resolution with a terminal configuration error."""

    def __init__(self) -> None:
        super().__init__(ConnectionNotSelectedError())


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "kind"),
    [
        (ValueError("connection is not configured"), "retry"),
        (RuntimeError("unlisted"), "retry"),
        (CredentialBrokerUnavailableError(), "retry"),
        (ObjectNotFoundError(), "defect"),
        (ConnectionNotSelectedError(), "defect"),
        (CredentialBrokerRejectedError(), "defect"),
        (sdk.ReauthorizationRequiredError(), "rejected"),
        (CredentialMaterialError("OAuth2 HTTP credential access_token is required"), "defect"),
        (_sdk_wrapped_material_error(), "defect"),
    ],
)
async def test_send_request_classifies_credential_resolve_failures(error: Exception, kind: str) -> None:
    attempt = await invoke_with(FailingCredentials(error), "sendRequest", 1)
    assert (attempt.kind if attempt.kind == "retry" else attempt.branch) == kind


async def invoke_with(
    provider: object, operation: str, attempt: int, method: str = "POST"
) -> sdk.MutationAttempt[oauth2_http.SendRequestOutput]:
    """Invoke one operation directly with provider and a context at attempt."""
    context = AttemptContext()
    context.attempt = attempt
    async with httpx.AsyncClient(transport=httpx.MockTransport(lambda _: pytest.fail("sent"))) as transport:
        client = oauth2_http.Client(
            oauth2_http.Config(
                authorization_endpoint="http://127.0.0.1:12345/auth",
                token_endpoint="http://127.0.0.1:12345/token",
                scopes=("profile",),
                api_base_url="http://127.0.0.1:12345/v1",
            ),
            provider,  # type: ignore[arg-type]
            transport,
        )
        call = sdk.Call(
            CALL_ID, CONNECTION, sdk.OperationRef("oauth2-http", operation),
            context=context,  # type: ignore[arg-type]
            max_sends=1,
        )
        if operation == "postNonIdempotent":
            return await client.post_non_idempotent().invoke(
                call, oauth2_http.PostNonIdempotentInput(path="/orders")
            )
        request = oauth2_http.SendRequestInput(method=method, path="/orders")
        return await client.send_request().invoke(call, request)


@pytest.mark.asyncio
@pytest.mark.parametrize("operation", ["sendRequest", "postNonIdempotent"])
async def test_unavailable_credential_broker_returns_retry_without_sending(operation: str) -> None:
    attempt = await invoke_with(UnavailableBroker(), operation, 1)
    assert attempt.kind == "retry", "a transient credential failure never selects defect"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("method", "attempt_number", "branch"),
    [("POST", 1, "defect"), ("POST", 2, "uncertain"), ("DELETE", 2, "uncertain"), ("GET", 2, "defect")],
)
async def test_send_request_terminal_credential_failure_of_a_later_write_is_uncertain(
    method: str, attempt_number: int, branch: str
) -> None:
    attempt = await invoke_with(MissingConnection(), "sendRequest", attempt_number, method)
    assert attempt.kind != "retry" and attempt.branch == branch


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("method", "attempt_number", "branch"),
    [("POST", 1, "rejected"), ("POST", 2, "uncertain"), ("PUT", 3, "uncertain"), ("GET", 2, "rejected")],
)
async def test_send_request_definite_refusal_of_a_later_write_is_uncertain(
    method: str, attempt_number: int, branch: str
) -> None:
    context = AttemptContext()
    context.attempt = attempt_number
    async with httpx.AsyncClient(transport=httpx.MockTransport(lambda _: httpx.Response(422))) as transport:
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
                        sdk.SecretString("token"),
                        sdk.SecretString(""),
                    )
                }
            ),
            transport,
        )
        call = sdk.Call(
            CALL_ID, CONNECTION, sdk.OperationRef("oauth2-http", "sendRequest"),
            context=context,  # type: ignore[arg-type]
        )
        request = oauth2_http.SendRequestInput(method=method, path="/orders")
        attempt = await client.send_request().invoke(call, request)
    assert attempt.kind != "retry" and attempt.branch == branch
    assert attempt.value is not None and attempt.value.status_code == 422


@pytest.mark.asyncio
async def test_missing_checkpoint_after_the_first_attempt_selects_uncertain_without_sending() -> None:
    requests: list[httpx.Request] = []
    context = AttemptContext()
    context.attempt = 2
    result = await post(lambda _: httpx.Response(201), context, requests)
    assert isinstance(result, sdk.MutationResult) and result.branch == "uncertain"
    assert requests == []
