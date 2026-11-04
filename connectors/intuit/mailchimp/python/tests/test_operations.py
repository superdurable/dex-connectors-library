# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Check Mailchimp client invariants outside the Go-recorded provider replay."""

import hashlib

import dex_connectors.sdk as sdk
import httpx
import pytest

import dex_connectors.intuit_mailchimp as mailchimp

CONNECTION = sdk.ConnectionRef("mailchimp", "test")


def make_call(operation: str) -> sdk.Call:
    """Build one non-durable call identity for isolated operation checks."""
    return sdk.Call(
        sdk.CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf"),
        CONNECTION,
        sdk.OperationRef("mailchimp", operation),
        sdk.IdempotencyKey(""),
    )


@pytest.mark.asyncio
async def test_key_suffix_selects_host_without_leaking_key_in_results() -> None:
    """A key chooses its own data center and is never retained in an output."""
    requests: list[httpx.Request] = []
    key = "mailchimpfixturekey-us21"

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(404, json={"title": "Resource Not Found"})

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = mailchimp.Client(
            mailchimp.Config(),
            sdk.StaticCredentialProvider(
                {
                    CONNECTION: mailchimp.Credentials(sdk.SecretString(key)),
                }
            ),
            http_client,
        )
        attempt = await client.get_member().invoke(
            make_call("getMember"), mailchimp.GetMemberInput("57afe96172", "ben@example.com")
        )
    assert attempt.branch == "notFound"
    assert requests[0].url.host == "us21.api.mailchimp.com"
    assert requests[0].headers["Authorization"] == f"Bearer {key}"
    assert key not in repr(attempt)


@pytest.mark.asyncio
async def test_invalid_key_and_malformed_email_fail_before_provider_call() -> None:
    """Credential and address defects are terminal and make no HTTP request."""
    requests: list[httpx.Request] = []

    def answer(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200)

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = mailchimp.Client(
            mailchimp.Config(),
            sdk.StaticCredentialProvider(
                {
                    CONNECTION: mailchimp.Credentials(sdk.SecretString("missing-suffix")),
                }
            ),
            http_client,
        )
        invalid_key = await client.get_member().invoke(
            make_call("getMember"), mailchimp.GetMemberInput("57afe96172", "ben@example.com")
        )
        invalid_email = await client.get_member().invoke(
            make_call("getMember"), mailchimp.GetMemberInput("57afe96172", "Ben <ben@example.com>")
        )
    assert invalid_key.branch == invalid_email.branch == "defect"
    assert invalid_key.failure is not None and invalid_key.failure.kind == sdk.FailureKind.AUTHENTICATION
    assert invalid_email.failure is not None and invalid_email.failure.kind == sdk.FailureKind.VALIDATION
    assert requests == []


@pytest.mark.asyncio
async def test_subscriber_hash_uses_lowercase_address() -> None:
    """Mailchimp's contact path is stable across address case changes."""
    paths: list[str] = []

    def answer(request: httpx.Request) -> httpx.Response:
        paths.append(request.url.path)
        return httpx.Response(404, json={"title": "Resource Not Found"})

    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http_client:
        client = mailchimp.Client(
            mailchimp.Config(),
            sdk.StaticCredentialProvider(
                {
                    CONNECTION: mailchimp.Credentials(sdk.SecretString("mailchimpfixturekey-us6")),
                }
            ),
            http_client,
        )
        await client.get_member().invoke(
            make_call("getMember"), mailchimp.GetMemberInput("57afe96172", "Ben@Example.com")
        )
    assert paths == ["/3.0/lists/57afe96172/members/" + hashlib.md5(b"ben@example.com").hexdigest()]
