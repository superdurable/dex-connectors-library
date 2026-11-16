# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""profileChanged fetcher behavior, ported from the Go connector's profile_fetcher tests."""

import json
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from datetime import timedelta
from typing import Any

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    ConnectionRef,
    CredentialProvider,
    ReauthorizationRequiredError,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
)
from dex_connectors.sdk.pollingtrigger import Cursor, RateLimitedError, is_cursor_invalid
from dex_connectors.sdk.projectconfig import encode_go_json_value

from dex_connectors.linkedin import (
    MINIMUM_POLL_INTERVAL,
    PROFILE_CHANGED_TRIGGER_DEFINITION,
    Client,
    Config,
    Credentials,
    ProfileField,
    decode_profile_changed_event_payload,
)
from dex_connectors.linkedin._profile_fetcher import ProfileFetcher
from tests.linkedin_profile_fake import ACCESS_TOKEN, USER_INFO_URL, LinkedInProfileFake, ada_profile

REFERENCE = ConnectionRef("linkedin", "linkedin-watcher")
RESIGNED_PICTURE = (
    "https://media.licdn.com/dms/image/v2/ada/profile-displayphoto-shrink_100_100/0/1?e=2&v=beta&t=second"
)


def credentials_for(token: str) -> Credentials:
    empty = SecretString("")
    return Credentials("", empty, SecretString(token), empty)


def new_client(
    http_client: httpx.AsyncClient, credentials: CredentialProvider[Credentials] | None = None
) -> Client:
    """A client polling every minute with the fake's token."""
    provider = credentials or StaticCredentialProvider({REFERENCE: credentials_for(ACCESS_TOKEN)})
    return Client(Config(user_info_url=USER_INFO_URL, poll_interval_seconds=60), provider, http_client)


@asynccontextmanager
async def fetcher_for(
    fake: LinkedInProfileFake, credentials: CredentialProvider[Credentials] | None = None
) -> AsyncIterator[ProfileFetcher]:
    async with httpx.AsyncClient(transport=httpx.MockTransport(fake.handle)) as http_client:
        client = new_client(http_client, credentials)
        yield ProfileFetcher(client, REFERENCE, PROFILE_CHANGED_TRIGGER_DEFINITION.trigger)


def context() -> TriggerContext:
    return TriggerContext(TriggerCancellation())


async def test_fetcher_seeds_without_backlog_then_delivers_each_change() -> None:
    fake = LinkedInProfileFake(ada_profile())
    async with fetcher_for(fake) as fetcher:
        seed = await fetcher.seed_cursor(context())
        assert '"subject":"782bbtaQ"' in seed.token
        unchanged = await fetcher.fetch_changes(context(), seed)
        assert not unchanged.changes, "the baseline is never delivered"
        assert unchanged.next_cursor.token == seed.token

        fake.update(picture=RESIGNED_PICTURE)
        resigned = await fetcher.fetch_changes(context(), seed)
        assert not resigned.changes, "a re-signed URL of the same picture is not a change"
        assert "t=second" in resigned.next_cursor.token, "the stored profile keeps the latest picture URL"

        fake.update(name="Ada King", family_name="King", email_verified=False)
        page = await fetcher.fetch_changes(context(), resigned.next_cursor)
        [change] = page.changes
        assert change.payload.subject == "782bbtaQ"
        assert change.payload.changed_fields == (
            ProfileField.NAME,
            ProfileField.FAMILY_NAME,
            ProfileField.EMAIL_VERIFIED,
        )
        assert change.payload.previous.name == "Ada Lovelace" and change.payload.previous.email_verified
        assert change.payload.current.name == "Ada King"
        assert change.payload.current.email == "ada@example.com", "an unverified email is still reported"
        assert not change.payload.current.email_verified
        assert change.id.startswith("linkedin-profile-") and len(change.id) == len("linkedin-profile-") + 32

        refetched = await fetcher.fetch_changes(context(), resigned.next_cursor)
        assert refetched.changes[0].id == change.id, "a refetch from the same cursor keeps the event ID"
        assert refetched.next_cursor.token == page.next_cursor.token

        payload = encode_go_json_value(change.payload).decode()
        assert decode_profile_changed_event_payload(payload, payload.encode()) == change.payload


async def test_a_profile_edited_back_and_forth_gets_a_new_event_id_each_time() -> None:
    fake = LinkedInProfileFake(ada_profile())
    async with fetcher_for(fake) as fetcher:
        cursor = await fetcher.seed_cursor(context())
        identifiers = []
        for name in ("Ada King", "Ada Lovelace", "Ada King"):
            fake.update(name=name)
            page = await fetcher.fetch_changes(context(), cursor)
            identifiers.append(page.changes[0].id)
            cursor = page.next_cursor
        assert len(set(identifiers)) == 3
        assert json.loads(cursor.token)["sequence"] == 3


@pytest.mark.parametrize(
    "token",
    [
        "not json",
        "null",
        "[]",
        '{"version":2,"subject":"782bbtaQ","hash":"00","profile":{"subject":"782bbtaQ"}}',
        '{"version":1,"subject":"782bbtaQ","profile":{"subject":"782bbtaQ"}}',
        '{"version":1,"subject":"782bbtaQ","hash":"00","sequence":"1","profile":{"subject":"782bbtaQ"}}',
        '{"version":1,"subject":"782bbtaQ","hash":"00","profile":{"subject":"782bbtaQ","emailVerified":1}}',
    ],
)
async def test_an_undecodable_cursor_reseeds_without_a_request(token: str) -> None:
    fake = LinkedInProfileFake(ada_profile())
    async with fetcher_for(fake) as fetcher:
        with pytest.raises(Exception) as raised:
            await fetcher.fetch_changes(context(), Cursor(token))
        assert is_cursor_invalid(raised.value)
    assert fake.request_count == 0


async def test_another_member_reseeds() -> None:
    fake = LinkedInProfileFake(ada_profile())
    async with fetcher_for(fake) as fetcher:
        seed = await fetcher.seed_cursor(context())
        fake.update(sub="grace-member")
        with pytest.raises(Exception) as raised:
            await fetcher.fetch_changes(context(), seed)
        assert is_cursor_invalid(raised.value)


@pytest.mark.parametrize(("retry_after", "expected"), [("120", timedelta(minutes=2)), ("", timedelta(0))])
async def test_a_429_pauses_for_retry_after_or_backs_off(retry_after: str, expected: timedelta) -> None:
    fake = LinkedInProfileFake(ada_profile())
    async with fetcher_for(fake) as fetcher:
        seed = await fetcher.seed_cursor(context())
        fake.fail_next(429, retry_after)
        with pytest.raises(RateLimitedError) as raised:
            await fetcher.fetch_changes(context(), seed)
        assert raised.value.retry_after == expected


@pytest.mark.parametrize(
    ("status", "message"),
    [
        (401, "invalid or revoked"),
        (403, "lacks the openid or profile scope"),
        (500, "HTTP 500"),
        (400, "HTTP 400"),
    ],
)
async def test_other_failures_are_ordinary_failed_polls(status: int, message: str) -> None:
    fake = LinkedInProfileFake(ada_profile())
    async with fetcher_for(fake) as fetcher:
        seed = await fetcher.seed_cursor(context())
        fake.fail_next(status)
        with pytest.raises(RuntimeError, match=message) as raised:
            await fetcher.fetch_changes(context(), seed)
        assert not is_cursor_invalid(raised.value)
        assert not isinstance(raised.value, RateLimitedError)


class RotatingCredentials:
    """Resolves a rejected token first, then refreshes to the fake's token after a 401."""

    def __init__(self, refresh_error: Exception | None = None) -> None:
        self.refresh_error = refresh_error
        self.refresh_count = 0

    async def resolve(self, call: Call) -> Credentials:
        return credentials_for("linkedin-" + "stale-token")

    async def resolve_after_rejection(self, call: Call, driver: Any = None) -> Credentials:
        self.refresh_count += 1
        if self.refresh_error is not None:
            raise self.refresh_error
        return credentials_for(ACCESS_TOKEN)


class ReauthorizationCredentials:
    async def resolve(self, call: Call) -> Credentials:
        raise ReauthorizationRequiredError()


async def test_a_401_refreshes_once_and_repeats_the_request() -> None:
    fake = LinkedInProfileFake(ada_profile())
    credentials = RotatingCredentials()
    async with fetcher_for(fake, credentials) as fetcher:
        seed = await fetcher.seed_cursor(context())
    assert json.loads(seed.token)["profile"]["name"] == "Ada Lovelace"
    assert (credentials.refresh_count, fake.request_count) == (1, 2)


async def test_a_401_that_refresh_cannot_fix_is_a_failed_poll() -> None:
    fake = LinkedInProfileFake(ada_profile())
    credentials = RotatingCredentials(RuntimeError("refresh refused"))
    async with fetcher_for(fake, credentials) as fetcher:
        with pytest.raises(RuntimeError, match="invalid or revoked"):
            await fetcher.seed_cursor(context())
    assert (credentials.refresh_count, fake.request_count) == (1, 1)


async def test_a_connection_that_needs_reauthorization_fails_without_a_request() -> None:
    fake = LinkedInProfileFake(ada_profile())
    async with fetcher_for(fake, ReauthorizationCredentials()) as fetcher:
        with pytest.raises(RuntimeError, match="requires reauthorization"):
            await fetcher.seed_cursor(context())
    assert fake.request_count == 0


async def test_an_oversized_response_is_a_failed_poll() -> None:
    fake = LinkedInProfileFake(ada_profile())
    async with httpx.AsyncClient(transport=httpx.MockTransport(fake.handle)) as http_client:
        provider = StaticCredentialProvider({REFERENCE: credentials_for(ACCESS_TOKEN)})
        client = Client(Config(user_info_url=USER_INFO_URL, max_response_bytes=16), provider, http_client)
        fetcher = ProfileFetcher(client, REFERENCE, PROFILE_CHANGED_TRIGGER_DEFINITION.trigger)
        with pytest.raises(RuntimeError, match="size limit"):
            await fetcher.seed_cursor(context())


async def test_poll_interval_defaults_to_fifteen_minutes_and_has_a_one_minute_floor() -> None:
    provider = StaticCredentialProvider({REFERENCE: credentials_for(ACCESS_TOKEN)})
    async with httpx.AsyncClient() as http_client:
        assert Client(Config(), provider, http_client).poll_interval == timedelta(minutes=15)
        zero = Client(Config(poll_interval_seconds=0), provider, http_client)
        assert zero.poll_interval == timedelta(minutes=15)
        assert (
            Client(Config(poll_interval_seconds=60), provider, http_client).poll_interval
            == MINIMUM_POLL_INTERVAL
        )
        with pytest.raises(ValueError, match="at least 1m"):
            Client(Config(poll_interval_seconds=59), provider, http_client)


def test_a_stored_payload_that_is_no_profile_change_is_refused() -> None:
    for payload in ('{"subject":""}', '{"subject":"782bbtaQ","changedFields":["nickname"]}', "[]"):
        with pytest.raises(ValueError, match="payload is invalid"):
            decode_profile_changed_event_payload(payload, payload.encode())
