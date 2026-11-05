# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Linear transport boundaries outside ordinary conformance cases."""

import httpx
import pytest
from dex_connectors.sdk import Call, CallID, ConnectionRef, FailureKind, OperationRef, SecretString
from dex_connectors.sdk.projectconfig.provider import RefreshFailedError

from dex_connectors.linear import Client, Config, Credentials, ListWorkflowStatesInput
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
    "url", ["http://linear.example/graphql", "http://127.0.0.1/graphql?", "http://127.0.0.1/\n"]
)
def test_api_url_rejects_disallowed_addresses(url: str) -> None:
    """Reject non-loopback HTTP and URL content Go's base URL validator rejects."""
    with pytest.raises(ValueError):
        Client(Config(), _Credentials(), api_url=url)
