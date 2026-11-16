# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""memberChanged runners, ported from the Go connector's member_trigger_runner tests."""

import asyncio
import json
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from datetime import timedelta
from typing import Any, cast

import httpx
import pytest
from dex_connectors.sdk import (
    SecretString,
    StaticCredentialProvider,
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

from dex_connectors.intuit_mailchimp import (
    Client,
    Config,
    Connection,
    Credentials,
    MemberChangedEvent,
    MemberChangedTriggerConfiguration,
    MemberChangedTriggerRoute,
    ProjectMemberChangedTriggerRoute,
    decode_member_changed_event_payload,
    member_statuses,
    new_connection,
    new_member_changed_trigger,
    new_member_trigger_runner,
    new_project_member_trigger_runner,
)
from tests.mailchimp_audience_fake import API_BASE_URL, API_KEY, MailchimpAudienceFake
from tests.test_member_change_fetcher import AUDIENCE_ID, REFERENCE

SCOPE = Scope("mailchimp-trigger-test", "live")
KEY = TriggerInboxKey("mailchimp", REFERENCE.name, "memberChanged", "audienceChanges")
AUDIENCE = MemberChangedTriggerConfiguration(audience_id=AUDIENCE_ID)


class QueueTarget:
    """Consumes each event into a queue."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[MemberChangedEvent]] = asyncio.Queue()

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[MemberChangedEvent]) -> None:
        """Record the event."""
        await self.events.put(event)

    async def next_event(self) -> TriggerEvent[MemberChangedEvent]:
        """Wait up to ten seconds for the next event."""
        return await asyncio.wait_for(self.events.get(), 10)


class ProjectStub:
    """The parts of LoadedProject the durable runner reads, over one in-memory object store."""

    def __init__(self, trigger_bindings: tuple[dict[str, Any], ...]) -> None:
        self.objects = InMemoryObjectStore()
        self.configuration = Configuration(SCOPE, 1, (), trigger_bindings)

    def trigger_inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)

    def poll_cursor_store(self, key: PollCursorKey) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, key)


def saved_binding(configuration: dict[str, Any]) -> dict[str, Any]:
    return {
        "connectorId": "mailchimp",
        "connectionName": REFERENCE.name,
        "triggerName": "memberChanged",
        "bindingName": "audienceChanges",
        "configuration": configuration,
    }


@asynccontextmanager
async def connection_for(audience: MailchimpAudienceFake) -> AsyncIterator[Connection]:
    """A connection whose requests the fake answers, polling every five seconds."""
    async with httpx.AsyncClient(transport=httpx.MockTransport(audience.handle)) as http_client:
        client = Client(
            Config(poll_interval_seconds=5),
            StaticCredentialProvider({REFERENCE: Credentials(SecretString(API_KEY))}),
            http_client,
            api_base_url=API_BASE_URL,
        )
        yield new_connection(client, REFERENCE)


async def eventually(condition: Callable[[], Awaitable[bool]]) -> None:
    """Poll condition for up to ten seconds."""
    async with asyncio.timeout(10):
        while not await condition():
            await asyncio.sleep(0.01)


@asynccontextmanager
async def running(runner: Any) -> AsyncIterator[None]:
    """Run a runner until the block exits, then require it to stop through cancellation."""
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:
        yield
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


async def test_runner_rejects_incomplete_routes() -> None:
    audience = MailchimpAudienceFake(AUDIENCE_ID)
    target = QueueTarget()
    async with connection_for(audience) as connection:
        for routes in {
            "none": [],
            "no binding": [MemberChangedTriggerRoute("", AUDIENCE, target)],
            "no target": [MemberChangedTriggerRoute("audienceChanges", AUDIENCE, cast(Any, None))],
            "duplicate": [
                MemberChangedTriggerRoute("audienceChanges", AUDIENCE, target),
                MemberChangedTriggerRoute("audienceChanges", AUDIENCE, target),
            ],
            "no audience": [
                MemberChangedTriggerRoute("audienceChanges", MemberChangedTriggerConfiguration(), target)
            ],
        }.values():
            with pytest.raises(ValueError):
                new_member_trigger_runner(connection, routes)
    with pytest.raises(ValueError, match="Mailchimp client"):
        new_member_trigger_runner(
            cast(Connection, object()), [MemberChangedTriggerRoute("audienceChanges", AUDIENCE, target)]
        )


async def test_durable_runner_requires_a_saved_audience() -> None:
    audience = MailchimpAudienceFake(AUDIENCE_ID)
    async with connection_for(audience) as connection:
        with pytest.raises(ValueError, match="no saved audience"):
            new_project_member_trigger_runner(
                cast(LoadedProject, ProjectStub(())),
                connection,
                [ProjectMemberChangedTriggerRoute("audienceChanges", QueueTarget())],
            )
        invalid = ProjectStub((saved_binding({"audienceId": AUDIENCE_ID, "statuses": ["gone"]}),))
        with pytest.raises(ValueError, match="not a Mailchimp contact status"):
            new_project_member_trigger_runner(
                cast(LoadedProject, invalid),
                connection,
                [ProjectMemberChangedTriggerRoute("audienceChanges", QueueTarget())],
            )


async def test_durable_runner_loads_the_saved_audience_and_commits_after_recording() -> None:
    audience = MailchimpAudienceFake(AUDIENCE_ID)
    audience.upsert("ben@example.com", "subscribed", "Ben")
    project = ProjectStub((saved_binding({"audienceId": AUDIENCE_ID, "statuses": ["subscribed"]}),))
    target = QueueTarget()
    cursor = project.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(KEY))

    async def commit_count() -> int:
        state = (await cursor.read_poll_cursor()).state
        return state.commit_count if state is not None else 0

    async with connection_for(audience) as connection:
        runner = new_project_member_trigger_runner(
            cast(LoadedProject, project),
            connection,
            [ProjectMemberChangedTriggerRoute("audienceChanges", target)],
        )
        async with running(runner):
            await eventually(lambda: _at_least(commit_count, 1))
            state = (await cursor.read_poll_cursor()).state
            assert state is not None
            assert json.loads(state.token)["baseline"] == "2026-11-01T09:00:01Z"
            audience.upsert("cara@example.com", "unsubscribed", "Cara")
            audience.upsert("dana@example.com", "subscribed", "Dana")
            runner.wake_poller("audienceChanges")
            event = await target.next_event()
            assert event.payload.member.email_address == "dana@example.com", "the saved statuses filter"
            assert event.payload.audience_id == AUDIENCE_ID
            inbox = project.trigger_inbox(KEY)

            async def is_drained() -> bool:
                return not await inbox.pending()

            await eventually(is_drained)


async def _at_least(read: Callable[[], Awaitable[int]], expected: int) -> bool:
    return await read() >= expected


async def test_single_trigger_baselines_and_delivers_the_next_change() -> None:
    audience = MailchimpAudienceFake(AUDIENCE_ID)
    audience.upsert("ben@example.com", "subscribed", "Ben")
    target = QueueTarget()
    async with connection_for(audience) as connection:
        # A short interval keeps the test fast; Client rejects one under the 5s minimum.
        assert isinstance(connection._client, Client)
        connection._client.poll_interval = timedelta(seconds=1)
        trigger = new_member_changed_trigger(
            connection=connection, binding_name="audienceChanges", configuration=AUDIENCE, target=target
        )
        async with running(trigger):
            await eventually(lambda: _requested(audience))
            audience.upsert("ben@example.com", "unsubscribed", "Ben")
            event = await target.next_event()
            assert event.payload.member.status == "unsubscribed"
            assert event.occurred_at.isoformat() == "2026-11-01T09:00:02+00:00"


async def _requested(audience: MailchimpAudienceFake) -> bool:
    return len(audience.requests) >= 1


def test_inbox_payload_decodes_a_go_written_event() -> None:
    go_payload = (
        '{"audienceId":"57afe96172","member":{"subscriberHash":"2c66f6142933ed5a97948f89cb1c7be0",'
        '"emailAddress":"ben@example.com","status":"unsubscribed",'
        '"mergeFields":{"FNAME":"Ben \\u003cB\\u003e"},'
        '"tagCount":0,"listId":"57afe96172","lastChangedAt":"2026-11-01T09:00:06Z"}}'
    )
    event = decode_member_changed_event_payload(go_payload, go_payload.encode())
    assert event.member.merge_fields == {"FNAME": "Ben <B>"}
    assert event.member.last_changed_at is not None and event.member.last_changed_at.minute == 0
    with pytest.raises(ValueError):
        decode_member_changed_event_payload('{"member":{"status":"subscribed"}}', b"")


def test_member_statuses_lists_the_picker_order() -> None:
    assert member_statuses() == (
        "subscribed",
        "unsubscribed",
        "cleaned",
        "pending",
        "transactional",
        "archived",
    )
