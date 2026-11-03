# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Exercise Asana input and secret-safety boundaries outside provider replay."""

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

from dex_connectors.asana import Client, Config, Credentials, CustomFieldValueInput, ListTasksInput
from dex_connectors.asana.task import custom_field_values, due_date, task_name


def test_custom_field_values_keep_zero_and_empty_text() -> None:
    """Preserve explicit zero values in the JSON sent to Asana."""
    values = custom_field_values(
        (
            CustomFieldValueInput(custom_field_id="1300000000000001", text=""),
            CustomFieldValueInput(custom_field_id="1300000000000002", number=0.0),
        )
    )
    assert values == {"1300000000000001": "", "1300000000000002": 0.0}


@pytest.mark.parametrize("value", ["2026-02-30", "10/15/2026", "2026-13-01"])
def test_invalid_calendar_date_is_rejected(value: str) -> None:
    """Stop malformed dates before they reach a provider request."""
    with pytest.raises(ValueError, match="YYYY-MM-DD"):
        due_date(value, "dueOn")


def test_task_name_rejects_control_characters() -> None:
    """Prevent multiline or control-bearing task names."""
    with pytest.raises(ValueError, match="one line"):
        task_name("approval\nrequest")


@pytest.mark.asyncio
async def test_token_reflection_selects_secret_safe_invalid_response() -> None:
    """Never persist a provider body that repeats the personal token."""
    connection = ConnectionRef("asana", "asana-tasks")
    token = "asana-inert-token"
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, text=f'{{"data":[],"echo":"{token}"}}')

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = Client(
            Config(endpoint="http://127.0.0.1:22201/api/1.0"),
            StaticCredentialProvider({connection: Credentials(SecretString(token))}),
            http_client,
        )
        call = Call(
            CallID("4faed739-92ee-58da-b3cf-fc1372dcd3f5"),
            connection,
            OperationRef("asana", "listTasks"),
        )
        attempt = await client.list_tasks().invoke(call, ListTasksInput(project_id="1201000000000001"))
        assert attempt.branch == "invalidResponse"
        assert attempt.failure is not None
        assert token not in attempt.failure.message
        assert token not in repr(attempt)
        assert len(requests) == 1


def test_endpoint_rejects_non_loopback_http() -> None:
    """Keep bearer tokens away from arbitrary cleartext endpoints."""
    connection = ConnectionRef("asana", "asana-tasks")
    with pytest.raises(ValueError, match="HTTPS"):
        Client(
            Config(endpoint="http://asana.example/api/1.0"),
            StaticCredentialProvider({connection: Credentials(SecretString("inert"))}),
        )
