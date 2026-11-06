# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Input and Graph boundary cases that complement Go-recorded replay."""

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

from dex_connectors.microsoft_excel import (
    Client,
    Config,
    Credentials,
    GetValuesInput,
    GetValuesOperation,
    UpdateValuesInput,
    UpdateValuesOperation,
)
from dex_connectors.microsoft_excel.client import path_escape

REFERENCE = ConnectionRef("microsoft", "approvals")


def test_path_escape_matches_go_segment_rules() -> None:
    """Graph IDs preserve Go's safe punctuation and escape path separators."""
    assert path_escape("A! '()*?,;/~$&+=:@") == "A%21%20%27%28%29%2A%3F%2C%3B%2F~$&+=:@"


@pytest.mark.asyncio
async def test_invalid_range_and_nonfinite_value_send_no_request() -> None:
    """Invalid range and nonfinite numbers fail before credentials or Graph IO."""
    requests = []
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda request: requests.append(request))
    ) as http:
        client = Client(
            Config(),
            StaticCredentialProvider({REFERENCE: Credentials(access_token=SecretString("inert-token"))}),
            http_client=http,
            local_provider_url="http://localhost",
        )
        query_call = Call(
            CallID("00000000-0000-4000-8000-000000000001"),
            REFERENCE,
            OperationRef("microsoft-excel", "getValues"),
        )
        query = await GetValuesOperation(client).invoke(
            query_call, GetValuesInput("drive", "workbook", "Summary", "XFE1")
        )
        assert query.branch == "defect"
        update_call = Call(
            CallID("00000000-0000-4000-8000-000000000002"),
            REFERENCE,
            OperationRef("microsoft-excel", "updateValues"),
        )
        update = await UpdateValuesOperation(client).invoke(
            update_call, UpdateValuesInput("drive", "workbook", "Summary", "A1", ((float("nan"),),))
        )
        assert update.branch == "defect"
        assert requests == []


@pytest.mark.asyncio
async def test_large_429_body_still_retries_by_status() -> None:
    """A large Graph error body never becomes a successful-response size branch."""
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda request: httpx.Response(429, content=b"x" * 100_000))
    ) as http:
        client = Client(
            Config(max_response_bytes=32),
            StaticCredentialProvider({REFERENCE: Credentials(access_token=SecretString("inert-token"))}),
            http_client=http,
            local_provider_url="http://localhost",
        )
        call = Call(
            CallID("00000000-0000-4000-8000-000000000003"),
            REFERENCE,
            OperationRef("microsoft-excel", "getValues"),
        )
        attempt = await GetValuesOperation(client).invoke(
            call, GetValuesInput("drive", "workbook", "Summary", "A1")
        )
        assert attempt.kind == "retry"
        assert attempt.failure.kind == "RATE_LIMIT"
