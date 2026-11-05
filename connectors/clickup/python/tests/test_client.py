# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Focused ClickUp HTTP and wire-decoding checks."""

import asyncio

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    OperationRef,
    SecretString,
    StaticCredentialProvider,
)

from dex_connectors.clickup.client import (
    Client,
    Config,
    Credentials,
    decode_object,
    decode_task,
    path_escape,
    unix_milliseconds,
)
from dex_connectors.clickup.models import CreateTaskInput, FindMemberByEmailInput, GetTaskInput
from dex_connectors.clickup.operations import _bare_email, _budgeted_exchange, _equal_fold


@pytest.mark.asyncio
async def test_large_rate_limit_body_keeps_status_classification() -> None:
    """A large provider error body remains a retry, as in Go."""
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: httpx.Response(429, content=b"x" * 1000))
    ) as transport:
        client = Client(
            Config(max_response_bytes=32),
            StaticCredentialProvider({}),
            transport,
            api_base_url="http://localhost/api/v2",
        )
        result = await client.exchange(
            "getTask", Credentials(SecretString("pk_inert"), SecretString("")), "GET", "/task/abc"
        )
        assert result.kind == "rate_limited"
        assert result.failure is not None and result.failure.kind.value == "RATE_LIMIT"


def test_path_escape_matches_go_safe_set() -> None:
    """Preserve Go's safe punctuation and escape all other path separators."""
    assert path_escape("A z~$&+=:@!'/") == "A%20z~$&+=:@%21%27%2F"


def test_wrong_typed_task_fields_are_invalid() -> None:
    """Provider arrays and wrong typed identifiers cannot silently become task data."""
    with pytest.raises(ValueError):
        decode_task({"id": 123, "assignees": [], "tags": []})
    with pytest.raises(ValueError):
        decode_task({"id": "abc", "assignees": "wrong", "tags": []})


def test_nullable_task_fields_match_go_wire_defaults() -> None:
    """Go leaves null strings, status, and archived at zero values."""
    task = decode_task(
        {
            "id": "abc",
            "name": None,
            "url": None,
            "status": None,
            "archived": None,
            "tags": [{"name": None}],
            "assignees": [{"id": 1, "username": None}],
            "priority": {"id": 1},
            "list": {"name": None},
        }
    )
    assert task.name == task.url == task.status.name == task.list_name == ""
    assert task.is_archived is False
    assert task.tags == ("",)
    assert task.assignees[0].username == ""
    assert task.priority == 1


@pytest.mark.parametrize(
    "field",
    [
        {"creator": {"id": 1, "username": 4}},
        {"list": {"name": 4}},
        {"custom_fields": [{"id": "field1", "name": 4}]},
        {"custom_fields": [{"id": "field1", "type": 4}]},
    ],
)
def test_wrong_typed_nested_fields_are_invalid(field: dict) -> None:
    """Go rejects wrong wire types even when the task ID is valid."""
    with pytest.raises(ValueError):
        decode_task({"id": "abc", **field}, details=True)


@pytest.mark.parametrize("literal", ["12345678901234567890.5", "1e400", "0.1"])
def test_custom_field_numbers_that_python_cannot_preserve_are_invalid(literal: str) -> None:
    """A returned custom field value never silently loses Go's JSON number."""
    response = f'{{"id":"abc","custom_fields":[{{"id":"field1","value":{literal}}}]}}'
    with pytest.raises(ValueError, match="custom field field1 value"):
        decode_task(decode_object(response.encode()), details=True)


def test_custom_field_exact_number_and_non_json_number() -> None:
    """Exactly representable values survive and NaN is not JSON."""
    response = b'{"id":"abc","custom_fields":[{"id":"field1","value":1.5}]}'
    assert decode_task(decode_object(response), details=True).custom_fields[0].value == 1.5
    with pytest.raises(ValueError, match="invalid JSON number"):
        decode_object(b'{"id":"abc","custom_fields":[{"id":"field1","value":NaN}]}')


def test_milliseconds_outside_python_datetime_range_fail_terminally() -> None:
    """Go accepts the int64 value; Python must name a range it cannot return."""
    with pytest.raises(ValueError, match="date_created"):
        decode_task({"id": "abc", "date_created": "9223372036854775807"})
    with pytest.raises(ValueError, match="time is not Unix milliseconds"):
        unix_milliseconds("9223372036854775808")


def test_bare_email_and_unicode_fold_match_go() -> None:
    """Go rejects malformed addr specs and compares Unicode runes one at a time."""
    assert _bare_email('"a b"@example.com')
    assert not _bare_email("a b@example.com")
    assert not _bare_email("a..b@example.com")
    assert _equal_fold("K@example.com", "k@example.com")
    assert not _equal_fold("ß@example.com", "ss@example.com")


@pytest.mark.asyncio
@pytest.mark.parametrize("bad_user", [{"username": 1}, {"role": "3"}, {"role": 3.0}])
async def test_member_wrong_wire_types_are_invalid_even_in_other_workspace(bad_user: dict) -> None:
    """Go unmarshals every Workspace member before choosing the requested one."""
    connection = ConnectionRef("clickup", "test")
    credential = Credentials(SecretString("pk_inert"), SecretString(""))
    response = {"teams": [{"id": "2", "members": [{"user": {"id": 1, "email": "a@b", **bad_user}}]}]}
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: httpx.Response(200, json=response))
    ) as transport:
        client = Client(
            Config(),
            StaticCredentialProvider({connection: credential}),
            transport,
            api_base_url="http://localhost/api/v2",
        )
        call = Call(
            CallID("a21330be-7cf8-4848-a5fb-d9888add03d9"),
            connection,
            OperationRef("clickup", "findMemberByEmail"),
        )
        result = await client.find_member_by_email().invoke(call, FindMemberByEmailInput("1", "a@b"))
        assert result.branch == "invalidResponse"


@pytest.mark.asyncio
async def test_create_task_custom_field_needs_value_before_send() -> None:
    """A missing RawMessage must select defect before any provider call."""
    connection = ConnectionRef("clickup", "test")
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: pytest.fail("request sent"))
    ) as transport:
        client = Client(
            Config(), StaticCredentialProvider({}), transport, api_base_url="http://localhost/api/v2"
        )
        call = Call(
            CallID("a21330be-7cf8-4848-a5fb-d9888add03d9"), connection, OperationRef("clickup", "createTask")
        )
        result = await client.create_task().invoke(
            call, CreateTaskInput(list_id="1", name="Task", custom_fields=({"id": "field1"},))
        )
        assert result.branch == "defect"
        assert result.failure is not None and "field1" in result.failure.message


@pytest.mark.asyncio
async def test_budgeted_tag_exchange_stops_before_late_request() -> None:
    """The operation's shared 25-second deadline prevents another request."""
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: pytest.fail("request sent"))
    ) as transport:
        client = Client(
            Config(), StaticCredentialProvider({}), transport, api_base_url="http://localhost/api/v2"
        )
        result = await _budgeted_exchange(
            client,
            asyncio.get_running_loop().time() - 1,
            Credentials(SecretString("pk_inert"), SecretString("")),
            "GET",
            "/task/abc",
        )
        assert result.kind == "unconfirmed"


@pytest.mark.asyncio
async def test_query_tilde_matches_go_url_values_encode() -> None:
    """Go leaves tilde literal in query values."""
    observed = []

    def respond(request: httpx.Request) -> httpx.Response:
        observed.append(str(request.url))
        return httpx.Response(200, json={})

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as transport:
        client = Client(
            Config(), StaticCredentialProvider({}), transport, api_base_url="http://localhost/api/v2"
        )
        await client.exchange(
            "getTask",
            Credentials(SecretString("pk_inert"), SecretString("")),
            "GET",
            "/task/abc",
            query={"filter": "a~b"},
        )
    assert observed == ["http://localhost/api/v2/task/abc?filter=a~b"]


@pytest.mark.asyncio
async def test_get_task_lossy_custom_field_selects_named_invalid_response() -> None:
    """A provider number that Python would round is a terminal protocol failure."""
    connection = ConnectionRef("clickup", "test")
    response = b'{"id":"abc","custom_fields":[{"id":"field1","value":12345678901234567890.5}]}'
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: httpx.Response(200, content=response))
    ) as transport:
        client = Client(
            Config(),
            StaticCredentialProvider({connection: Credentials(SecretString("pk_inert"), SecretString(""))}),
            transport,
            api_base_url="http://localhost/api/v2",
        )
        call = Call(
            CallID("a21330be-7cf8-4848-a5fb-d9888add03d9"),
            connection,
            OperationRef("clickup", "getTask"),
        )
        result = await client.get_task().invoke(call, GetTaskInput(task_id="abc"))
        assert result.branch == "invalidResponse"
        assert result.failure is not None and "custom field field1 value" in result.failure.message
