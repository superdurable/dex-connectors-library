# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Sentry issue operations."""

import json
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
    run_mutation,
    run_query,
)

from dex_connectors.sentry import (
    DEFECT,
    FOUND,
    GET_ISSUE_DEFINITION,
    INVALID_RESPONSE,
    LIST_ISSUES_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    UPDATE_ISSUE_STATUS_DEFINITION,
    UPDATED,
    Client,
    Config,
    Credentials,
    GetIssueInput,
    ListIssuesInput,
    UpdateIssueStatusInput,
)

CONNECTION = ConnectionRef("sentry", "sentry")
TOKEN = "sntrys_test_token"
ISSUE = {"id": "123", "shortId": "APP-1", "title": "ExampleError", "status": "unresolved", "count": "4"}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, token: str = TOKEN
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848/api/0", max_response_bytes=256),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def make_call(definition: object) -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, definition.operation)  # type: ignore[attr-defined]


@pytest.mark.asyncio
async def test_list_escapes_organization_and_query() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json=[ISSUE]), requests)
    result = await run_query(
        client.list_issues(),
        make_call(LIST_ISSUES_DEFINITION),
        ListIssuesInput("my org", "is:unresolved level:error"),
    )
    assert result.branch == LISTED
    assert result.value.issues[0].short_id == "APP-1"
    assert (
        requests[0].url.raw_path
        == b"/api/0/organizations/my%20org/issues/?query=is%3Aunresolved%20level%3Aerror"
    )
    assert requests[0].headers["Authorization"] == "Bearer " + TOKEN
    await run_query(client.list_issues(), make_call(LIST_ISSUES_DEFINITION), ListIssuesInput("acme"))
    assert requests[1].url.raw_path == b"/api/0/organizations/acme/issues/"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch"),
    [
        (httpx.Response(200, json=None), INVALID_RESPONSE),
        (httpx.Response(200, json={"issues": []}), INVALID_RESPONSE),
        (httpx.Response(200, content=b"x" * 300), INVALID_RESPONSE),
        (httpx.Response(404), PROVIDER_REJECTED),
        (httpx.Response(200, json=[]), LISTED),
    ],
)
async def test_list_branches(response: httpx.Response, branch: str) -> None:
    result = await run_query(
        make_client(response).list_issues(), make_call(LIST_ISSUES_DEFINITION), ListIssuesInput("acme")
    )
    assert result.branch == branch


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch"),
    [
        (httpx.Response(200, json=ISSUE), FOUND),
        (httpx.Response(404), NOT_FOUND),
        (httpx.Response(403), PROVIDER_REJECTED),
        (httpx.Response(200, json={"title": "no id"}), INVALID_RESPONSE),
    ],
)
async def test_get_branches(response: httpx.Response, branch: str) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(response, requests).get_issue(),
        make_call(GET_ISSUE_DEFINITION),
        GetIssueInput("acme", "1/2"),
    )
    assert result.branch == branch
    assert requests[0].url.raw_path == b"/api/0/organizations/acme/issues/1%2F2/"


@pytest.mark.asyncio
async def test_update_puts_absolute_status() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json={**ISSUE, "status": "resolved"}), requests)
    result = await run_mutation(
        client.update_issue_status(),
        make_call(UPDATE_ISSUE_STATUS_DEFINITION),
        UpdateIssueStatusInput("acme", "123", "resolved"),
    )
    assert result.branch == UPDATED
    assert result.value.status == "resolved"
    assert requests[0].method == "PUT"
    assert json.loads(requests[0].content) == {"status": "resolved"}
    assert "Idempotency-Key" not in requests[0].headers


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch"),
    [
        (httpx.Response(404), NOT_FOUND),
        (httpx.Response(400), PROVIDER_REJECTED),
        (httpx.Response(409, json={"name": "invalid_idempotent_request"}), PROVIDER_REJECTED),
        (httpx.Response(202, json=ISSUE), UPDATED),
        (httpx.Response(200, content=b"[]"), INVALID_RESPONSE),
    ],
)
async def test_update_branches(response: httpx.Response, branch: str) -> None:
    result = await run_mutation(
        make_client(response).update_issue_status(),
        make_call(UPDATE_ISSUE_STATUS_DEFINITION),
        UpdateIssueStatusInput("acme", "123", "ignored"),
    )
    assert result.branch == branch


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "kind"),
    [
        (httpx.Response(429, headers={"Retry-After": "2"}), FailureKind.RATE_LIMIT),
        (httpx.Response(500), FailureKind.AVAILABILITY),
        (httpx.Response(409), FailureKind.AVAILABILITY),
    ],
)
async def test_update_retries_safe_failures(response: httpx.Response, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_mutation(
            make_client(response).update_issue_status(),
            make_call(UPDATE_ISSUE_STATUS_DEFINITION),
            UpdateIssueStatusInput("acme", "123", "resolved"),
        )
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
async def test_invalid_inputs_and_blank_token_are_defects_without_request() -> None:
    requests: list[httpx.Request] = []
    client = make_client(httpx.Response(200, json=ISSUE), requests)
    for status_input in (
        UpdateIssueStatusInput("acme", "123", "closed"),
        UpdateIssueStatusInput("", "123", "resolved"),
    ):
        result = await run_mutation(
            client.update_issue_status(), make_call(UPDATE_ISSUE_STATUS_DEFINITION), status_input
        )
        assert result.branch == DEFECT
    listed = await run_query(client.list_issues(), make_call(LIST_ISSUES_DEFINITION), ListIssuesInput(""))
    assert listed.branch == DEFECT
    blank_token = await run_query(
        make_client(httpx.Response(200), requests, token=" ").get_issue(),
        make_call(GET_ISSUE_DEFINITION),
        GetIssueInput("acme", "1"),
    )
    assert blank_token.branch == DEFECT
    assert blank_token.failure is not None and blank_token.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []
