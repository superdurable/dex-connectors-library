# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Linear transport boundaries outside ordinary conformance cases."""

import httpx
import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, FailureKind, OperationRef, SecretString
from dex_connectors.sdk.projectconfig.broker import (
    ConnectionNotSelectedError,
    CredentialBrokerRejectedError,
    CredentialBrokerUnavailableError,
)
from dex_connectors.sdk.projectconfig.provider import RefreshFailedError
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError

from dex_connectors.linear import Client, Config, Credentials, ListWorkflowStatesInput
from dex_connectors.linear._generated.connection import CredentialMaterialError
from dex_connectors.linear.client import Exchange, _classify_failure

CONNECTION = ConnectionRef("linear", "linear-test")
CALL = Call(
    CallID("33333333-3333-4333-8333-333333333333"), CONNECTION, OperationRef("linear", "listWorkflowStates")
)
TEAM_ID = "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4"


class _Credentials:
    async def resolve(self, call: Call) -> Credentials:
        return Credentials(api_key=SecretString("lin_" + "api_" + "inert"))


class _UnavailableRefresh:
    async def resolve(self, call: Call) -> Credentials:
        raise RefreshFailedError("temporary")


@pytest.mark.asyncio
@pytest.mark.parametrize("header, delay", [("7200", 3600.0), ("1_0", 0.0)])
async def test_large_429_remains_rate_limit_retry(header: str, delay: float) -> None:
    """Classify a large error by status even when its body exceeds the success limit."""
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(
            lambda _: httpx.Response(429, headers={"Retry-After": header}, content=b"x" * 100)
        )
    ) as transport:
        client = Client(
            Config(max_response_bytes=10), _Credentials(), transport, api_url="http://127.0.0.1/graphql"
        )
        attempt = await client.list_workflow_states().invoke(CALL, ListWorkflowStatesInput(TEAM_ID))
    assert attempt.kind == "retry"
    assert attempt.failure is not None and attempt.failure.kind == FailureKind.RATE_LIMIT
    assert attempt.retry_after_seconds == delay


def test_graphql_error_code_precedes_http_status() -> None:
    """A precise Linear error code wins over the HTTP fallback."""
    result = _classify_failure("listWorkflowStates", 429, "", [{"extensions": {"code": "FORBIDDEN"}}], 0)
    assert result.outcome == "rejected"
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHORIZATION


@pytest.mark.asyncio
async def test_refresh_failure_retries_without_provider_request() -> None:
    """A temporary OAuth refresh failure must not become a terminal credential branch."""
    client = Client(Config(), _UnavailableRefresh(), api_url="http://127.0.0.1/graphql")
    resolved = await client.resolve(CALL, "listWorkflowStates")
    await client.aclose()
    assert isinstance(resolved, Exchange)
    assert resolved.outcome == "retry"
    assert resolved.failure is not None and resolved.failure.kind == FailureKind.AVAILABILITY


@pytest.mark.parametrize(
    ("status", "expected"), [(302, "retry"), (425, "retry"), (409, "retry"), (422, "rejected")]
)
def test_unlisted_status_is_unknown_and_retries(status: int, expected: str) -> None:
    """Only a definite refusal status rejects; a redirect or unlisted status retries."""
    assert _classify_failure("updateIssue", status, "", None, 0).outcome == expected


def _sdk_decoding_failure() -> ValueError:
    """Build the SDK project provider's wrapper around a malformed stored credential."""
    try:
        try:
            raise CredentialMaterialError("Linear credential api_key is required")
        except CredentialMaterialError:
            raise ValueError("project credential decoding failed") from None
    except ValueError as error:
        return error


class _FailingCredentials:
    def __init__(self, error: Exception) -> None:
        self.error = error

    async def resolve(self, call: Call) -> Credentials:
        raise self.error


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("error", "expected"),
    [
        (ObjectNotFoundError("absent"), "defect"),
        (ConnectionNotSelectedError(), "defect"),
        (CredentialBrokerRejectedError(), "defect"),
        (CredentialMaterialError("Linear credential api_key is required"), "defect"),
        (_sdk_decoding_failure(), "defect"),
        (CredentialBrokerUnavailableError(), "retry"),
        (ValueError("connection is not configured"), "retry"),
        (LookupError("unlisted"), "retry"),
    ],
)
async def test_credential_resolve_errors_use_the_shared_terminal_set(error: Exception, expected: str) -> None:
    """Only the listed credential failures are terminal; every other one retries."""
    client = Client(Config(), _FailingCredentials(error), api_url="http://127.0.0.1/graphql")
    resolved = await client.resolve(CALL, "listWorkflowStates")
    await client.aclose()
    assert isinstance(resolved, Exchange)
    assert resolved.outcome == expected


@pytest.mark.parametrize(
    "url", ["http://linear.example/graphql", "http://127.0.0.1/graphql?", "http://127.0.0.1/\n"]
)
def test_api_url_rejects_disallowed_addresses(url: str) -> None:
    """Reject non-loopback HTTP and URL content Go's base URL validator rejects."""
    with pytest.raises(ValueError):
        Client(Config(), _Credentials(), api_url=url)
