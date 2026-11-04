# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Focused ClickUp HTTP and wire-decoding checks."""

import httpx
import pytest
from dex_connectors.sdk import SecretString, StaticCredentialProvider

from dex_connectors.clickup.client import Client, Config, Credentials, decode_task, path_escape


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
