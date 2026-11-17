# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Repeat-safe HubSpot writes retry an HTTP status that does not prove a refusal."""

from datetime import UTC, datetime

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.hubspot as hubspot

CONNECTION = sdk.ConnectionRef("hubspot", "hubspot-crm")


def make_client(status: int, requests: list[httpx.Request]) -> tuple[hubspot.Client, httpx.AsyncClient]:
    """Build a client whose provider answers every request with one status."""

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(status, json={"category": "CONFLICT" if status == 409 else "UNKNOWN"})

    transport = httpx.AsyncClient(transport=httpx.MockTransport(respond))
    client = hubspot.Client(
        hubspot.Config(endpoint="http://127.0.0.1:42701"),
        sdk.StaticCredentialProvider(
            {CONNECTION: hubspot.Credentials(access_token=sdk.SecretString("pat-na1-test-token"))}
        ),
        transport,
        lambda: datetime(2026, 10, 30, tzinfo=UTC),
    )
    return client, transport


def call(operation: str) -> sdk.Call:
    """Return a Call for one HubSpot operation."""
    return sdk.Call(
        sdk.CallID("778856d1-a2cd-5939-9ae5-02d7ba5fd015"), CONNECTION, sdk.OperationRef("hubspot", operation)
    )


@pytest.mark.asyncio
async def test_lasting_http_conflict_retries_an_upsert_and_keeps_conflict_for_an_update() -> None:
    requests: list[httpx.Request] = []
    client, transport = make_client(409, requests)
    async with transport:
        with pytest.raises(sdk.RetryError):
            await sdk.run_mutation(
                client.upsert_object(),
                call("upsertObject"),
                hubspot.UpsertObjectInput("contacts", "email", "ada@example.com"),
            )
        assert len(requests) == 2, "the upsert resends once inside the attempt"
        update = await sdk.run_mutation(
            client.update_object(),
            call("updateObject"),
            hubspot.UpdateObjectInput("contacts", "501", {"email": "taken@example.com"}),
        )
    assert update.branch == "conflict"


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [501, 302, 418])
async def test_writes_retry_a_status_that_does_not_prove_a_refusal(status: int) -> None:
    client, transport = make_client(status, [])
    async with transport:
        with pytest.raises(sdk.RetryError):
            await sdk.run_mutation(
                client.update_object(),
                call("updateObject"),
                hubspot.UpdateObjectInput("deals", "9001", {"dealstage": "qualifiedtobuy"}),
            )
        with pytest.raises(sdk.RetryError):
            await sdk.run_mutation(
                client.upsert_object(),
                call("upsertObject"),
                hubspot.UpsertObjectInput("contacts", "email", "ada@example.com"),
            )
