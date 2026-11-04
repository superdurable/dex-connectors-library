# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the GitLab operations."""

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

from dex_connectors.gitlab import (
    DEFECT,
    FOUND,
    GET_PROJECT_DEFINITION,
    INVALID_RESPONSE,
    LIST_ISSUES_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
    UPDATE_ISSUE_LABELS_DEFINITION,
    UPDATED,
    Client,
    Config,
    Credentials,
    GetProjectInput,
    Issue,
    ListIssuesInput,
    Project,
    UpdateIssueLabelsInput,
)

CONNECTION = ConnectionRef("gitlab", "gitlab")
TOKEN = "glpat-test"
PROJECT = {
    "id": 42,
    "name": "Meridian Web",
    "path_with_namespace": "meridian/web",
    "web_url": "https://gitlab.test/m",
}
ISSUE = {
    "id": 123,
    "iid": 7,
    "title": "Review",
    "labels": ["review", "ready"],
    "web_url": "https://gitlab.test/i",
}
UPDATE = UpdateIssueLabelsInput("meridian/web", 7, ("review", "ready"))


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, token: str = TOKEN
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config("http://localhost:8848/api/v4", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(token))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def call_for(definition: object) -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, definition.operation)  # type: ignore[attr-defined]


@pytest.mark.asyncio
async def test_get_project_escapes_namespace_path_and_sends_private_token() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=PROJECT), requests).get_project(),
        call_for(GET_PROJECT_DEFINITION),
        GetProjectInput("meridian/web"),
    )
    assert result.branch == FOUND
    assert result.value == Project(42, "Meridian Web", "meridian/web", "https://gitlab.test/m")
    request = requests[0]
    assert request.method == "GET"
    assert request.url.raw_path == b"/api/v4/projects/meridian%2Fweb"
    assert request.headers["PRIVATE-TOKEN"] == TOKEN
    assert "Content-Type" not in request.headers


@pytest.mark.asyncio
async def test_list_issues_decodes_array() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=[ISSUE]), requests).list_issues(),
        call_for(LIST_ISSUES_DEFINITION),
        ListIssuesInput("42"),
    )
    assert result.branch == LISTED
    assert result.value.issues == (Issue(123, 7, "Review", ("review", "ready"), "https://gitlab.test/i"),)
    assert requests[0].url.raw_path == b"/api/v4/projects/42/issues"


@pytest.mark.asyncio
async def test_update_issue_labels_puts_form_body() -> None:
    requests: list[httpx.Request] = []
    result = await run_mutation(
        make_client(httpx.Response(200, json=ISSUE), requests).update_issue_labels(),
        call_for(UPDATE_ISSUE_LABELS_DEFINITION),
        UPDATE,
    )
    assert result.branch == UPDATED
    assert result.value.labels == ("review", "ready")
    request = requests[0]
    assert request.method == "PUT"
    assert request.url.raw_path == b"/api/v4/projects/meridian%2Fweb/issues/7"
    assert request.headers["Content-Type"] == "application/x-www-form-urlencoded"
    assert request.content == b"labels=review%2Cready"


@pytest.mark.asyncio
async def test_empty_label_set_clears_labels() -> None:
    requests: list[httpx.Request] = []
    await run_mutation(
        make_client(httpx.Response(200, json=ISSUE), requests).update_issue_labels(),
        call_for(UPDATE_ISSUE_LABELS_DEFINITION),
        UpdateIssueLabelsInput("42", 7, ()),
    )
    assert requests[0].content == b"labels="


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        UpdateIssueLabelsInput(" ", 7, ()),
        UpdateIssueLabelsInput("42", 0, ()),
        UpdateIssueLabelsInput("42", 7, ("a,b",)),
        UpdateIssueLabelsInput("42", 7, (" ",)),
    ],
)
async def test_invalid_label_update_is_defect_without_request(value: UpdateIssueLabelsInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_mutation(
        make_client(httpx.Response(200, json=ISSUE), requests).update_issue_labels(),
        call_for(UPDATE_ISSUE_LABELS_DEFINITION),
        value,
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.VALIDATION
    assert requests == []


@pytest.mark.asyncio
async def test_blank_project_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=PROJECT), requests).get_project(),
        call_for(GET_PROJECT_DEFINITION),
        GetProjectInput(" "),
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(404), NOT_FOUND, FailureKind.NOT_FOUND),
        (httpx.Response(400), PROVIDER_REJECTED, FailureKind.VALIDATION),
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.AUTHENTICATION),
        (httpx.Response(403), PROVIDER_REJECTED, FailureKind.AUTHORIZATION),
        (httpx.Response(200, json={**ISSUE, "iid": 0}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, json={**ISSUE, "labels": "review"}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_update_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_mutation(
        make_client(response).update_issue_labels(), call_for(UPDATE_ISSUE_LABELS_DEFINITION), UPDATE
    )
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
@pytest.mark.parametrize("body", [{"name": "x"}, {"id": "42"}, [], None])
async def test_project_without_positive_id_is_invalid(body: object) -> None:
    result = await run_query(
        make_client(httpx.Response(200, json=body)).get_project(),
        call_for(GET_PROJECT_DEFINITION),
        GetProjectInput("42"),
    )
    assert result.branch == INVALID_RESPONSE


@pytest.mark.asyncio
@pytest.mark.parametrize("body", [{"issues": []}, None])
async def test_issue_page_requires_array(body: object) -> None:
    result = await run_query(
        make_client(httpx.Response(200, json=body)).list_issues(),
        call_for(LIST_ISSUES_DEFINITION),
        ListIssuesInput("42"),
    )
    assert result.branch == INVALID_RESPONSE


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"),
    [
        (408, FailureKind.AVAILABILITY),
        (409, FailureKind.AVAILABILITY),
        (429, FailureKind.RATE_LIMIT),
        (500, FailureKind.AVAILABILITY),
    ],
)
async def test_safe_failures_retry(status: int, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_mutation(
            make_client(httpx.Response(status)).update_issue_labels(),
            call_for(UPDATE_ISSUE_LABELS_DEFINITION),
            UPDATE,
        )
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
async def test_blank_token_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=PROJECT), requests, token="").get_project(),
        call_for(GET_PROJECT_DEFINITION),
        GetProjectInput("42"),
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []
