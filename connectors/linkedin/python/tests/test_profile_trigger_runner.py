# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""profileChanged runners, ported from the Go connector's profile_trigger_runner tests."""

import asyncio
import json
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from datetime import timedelta
from pathlib import Path
from typing import Any, cast

import httpx
import pytest
from dex_connectors.sdk import (
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)
from dex_connectors.sdk.projectconfig import (
    Configuration,
    InMemoryObjectStore,
    LoadedProject,
    PollCursorKey,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
)

from dex_connectors.linkedin import (
    Connection,
    ProfileChangedEvent,
    ProfileChangedTriggerRoute,
    ProjectProfileChangedTriggerRoute,
    decode_profile_changed_event_payload,
    new_connection,
    new_profile_changed_trigger,
    new_profile_trigger_runner,
    new_project_profile_trigger_runner,
)
from tests.linkedin_profile_fake import LinkedInProfileFake, ada_profile
from tests.test_profile_fetcher import REFERENCE, new_client

SCOPE = Scope("linkedin-test", "preview")
BINDING = "ownProfile"
KEY = TriggerInboxKey("linkedin", REFERENCE.name, "profileChanged", BINDING)
GO_FIXTURE = json.loads(
    (Path(__file__).parent / "fixtures" / "profile-changed-go-fetcher-cases.json").read_text()
)


class QueueTarget:
    """Consumes each event into a queue."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[ProfileChangedEvent]] = asyncio.Queue()

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[ProfileChangedEvent]) -> None:
        await self.events.put(event)

    async def next_event(self) -> TriggerEvent[ProfileChangedEvent]:
        return await asyncio.wait_for(self.events.get(), 10)


class ProjectStub:
    """The parts of LoadedProject the durable runner reads, over one in-memory object store."""

    def __init__(self, trigger_bindings: tuple[dict[str, Any], ...] = ()) -> None:
        self.objects = InMemoryObjectStore()
        self.configuration = Configuration(SCOPE, 1, (), trigger_bindings)

    def trigger_inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)

    def poll_cursor_store(self, key: PollCursorKey) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, key)


@asynccontextmanager
async def connection_for(fake: LinkedInProfileFake) -> AsyncIterator[Connection]:
    async with httpx.AsyncClient(transport=httpx.MockTransport(fake.handle)) as http_client:
        connection = new_connection(new_client(http_client), REFERENCE)
        # A short interval keeps the tests fast; Client rejects one under the one-minute minimum.
        cast(Any, connection)._client.poll_interval = timedelta(seconds=1)
        yield connection


async def eventually(condition: Callable[[], Awaitable[bool]]) -> None:
    async with asyncio.timeout(10):
        while not await condition():
            await asyncio.sleep(0.01)


@asynccontextmanager
async def running(runner: Any) -> AsyncIterator[None]:
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:
        yield
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


async def test_runner_rejects_incomplete_routes() -> None:
    fake = LinkedInProfileFake(ada_profile())
    target = QueueTarget()
    async with connection_for(fake) as connection:
        for routes in (
            [],
            [ProfileChangedTriggerRoute("", target)],
            [ProfileChangedTriggerRoute(BINDING, cast(Any, None))],
            [ProfileChangedTriggerRoute("a", target), ProfileChangedTriggerRoute("a", target)],
        ):
            with pytest.raises(ValueError):
                new_profile_trigger_runner(connection, routes)
    with pytest.raises(ValueError, match="LinkedIn client"):
        new_profile_trigger_runner(cast(Connection, object()), [ProfileChangedTriggerRoute(BINDING, target)])


async def test_durable_runner_records_each_change_before_the_profile_commits() -> None:
    fake = LinkedInProfileFake(ada_profile())
    project = ProjectStub()
    target = QueueTarget()
    cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(KEY))
    inbox = project.trigger_inbox(KEY)

    async def commit_count() -> int:
        state = (await cursor.read_poll_cursor()).state
        return state.commit_count if state is not None else 0

    async with connection_for(fake) as connection:
        runner = new_project_profile_trigger_runner(
            cast(LoadedProject, project), connection, [ProjectProfileChangedTriggerRoute(BINDING, target)]
        )
        async with running(runner):
            await eventually(lambda: _at_least(commit_count, 1))
            state = (await cursor.read_poll_cursor()).state
            assert state is not None and json.loads(state.token)["profile"]["name"] == "Ada Lovelace"
            assert target.events.empty(), "the baseline is never delivered"

            fake.update(email="ada@lovelace.example", email_verified=False)
            runner.wake_poller(BINDING)
            event = await target.next_event()
            assert event.payload.changed_fields == ("email", "emailVerified")

            async def has_committed_the_change() -> bool:
                state = (await cursor.read_poll_cursor()).state
                return state is not None and json.loads(state.token).get("sequence") == 1

            await eventually(has_committed_the_change)

            async def is_drained() -> bool:
                return not await inbox.pending()

            await eventually(is_drained)


async def _at_least(read: Callable[[], Awaitable[int]], expected: int) -> bool:
    return await read() >= expected


async def test_durable_runner_rejects_a_saved_configuration_with_fields() -> None:
    fake = LinkedInProfileFake(ada_profile())
    binding = {
        "connectorId": "linkedin",
        "connectionName": REFERENCE.name,
        "triggerName": "profileChanged",
        "bindingName": BINDING,
        "configuration": {"memberId": "someone-else"},
    }
    async with connection_for(fake) as connection:
        with pytest.raises(ValueError, match=BINDING):
            new_project_profile_trigger_runner(
                cast(LoadedProject, ProjectStub((binding,))),
                connection,
                [ProjectProfileChangedTriggerRoute(BINDING, QueueTarget())],
            )


async def test_single_trigger_baselines_and_delivers_the_next_change() -> None:
    fake = LinkedInProfileFake(ada_profile())
    target = QueueTarget()
    async with connection_for(fake) as connection:
        trigger = new_profile_changed_trigger(connection=connection, binding_name=BINDING, target=target)
        async with running(trigger):

            async def has_polled() -> bool:
                return fake.request_count >= 1

            await eventually(has_polled)
            fake.update(locale="fr-FR")
            event = await target.next_event()
            assert event.payload.changed_fields == ("locale",)
            assert (event.payload.previous.locale, event.payload.current.locale) == ("en-US", "fr-FR")


def test_inbox_payload_decodes_a_go_written_event() -> None:
    [recorded] = [step for step in GO_FIXTURE["steps"] if step["step"]["name"] == "renamed and unverified"]
    go_payload = recorded["result"]["events"][0]["payload"]
    event = decode_profile_changed_event_payload(go_payload, go_payload.encode())
    assert event.previous.name == "Ada <b>&amp;</b> Lovelace 😀"
    assert event.current.name == "Ada King" and not event.current.email_verified
    assert event.changed_fields == ("name", "familyName", "emailVerified", "locale")
