# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver LinkedIn profile changes through the example's Flow target to a real Dex Worker and Flow."""

import asyncio
import json
import os
import tempfile
import time
from collections.abc import AsyncIterator, Callable
from contextlib import asynccontextmanager
from dataclasses import dataclass, field, replace
from datetime import timedelta
from pathlib import Path
from typing import Any, cast

import dex
import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)
from dex_connectors.sdk.pollingtrigger import PollRunner
from dex_connectors.sdk.projectconfig import (
    Configuration,
    InMemoryObjectStore,
    LoadedProject,
    PollCursorKey,
    PollCursorState,
    PollCursorStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
)

from dex_connectors.linkedin import (
    Client,
    Config,
    Credentials,
    ProfileChangedEvent,
    ProjectProfileChangedTriggerRoute,
    new_connection,
    new_project_profile_trigger_runner,
)
from tests.linkedin_profile_fake import ACCESS_TOKEN, USER_INFO_URL, LinkedInProfileFake, ada_profile

from .flow import BINDING_NAME, CONNECTION_NAME, ProfileChangeFlow, ProfileChangeOutcome, resolve_flow_id
from .worker import new_profile_change_flow_target

SCOPE = Scope("linkedin-profile-changed", "live")
INBOX_KEY = TriggerInboxKey("linkedin", CONNECTION_NAME, "profileChanged", BINDING_NAME)


class DurableProject:
    """The project storage two Trigger processes share: hosted storage keeps inboxes and cursors."""

    def __init__(self) -> None:
        self.objects = InMemoryObjectStore()
        self.configuration = Configuration(SCOPE, 1, ())

    def trigger_inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)

    def poll_cursor_store(self, key: PollCursorKey) -> PollCursorStore:
        return PollCursorStore(self.objects, SCOPE, key)

    async def cursor_state(self) -> PollCursorState | None:
        return (
            await self.poll_cursor_store(PollCursorKey.from_trigger_inbox_key(INBOX_KEY)).read_poll_cursor()
        ).state


@dataclass
class CountingTarget:
    """Wraps the example's Flow target and records each delivered event once, in order."""

    inner: Any
    delivered: dict[str, TriggerEvent[ProfileChangedEvent]] = field(default_factory=dict)

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[ProfileChangedEvent]) -> None:
        await self.inner.handle_trigger(context, event)
        self.delivered.setdefault(event.id, event)


async def eventually(condition: Callable[[], Any], timeout: float = 30) -> None:
    """Poll a sync or async condition until it holds."""
    deadline = time.monotonic() + timeout
    while True:
        result = condition()
        if asyncio.iscoroutine(result):
            result = await result
        if result:
            return
        assert time.monotonic() < deadline, "condition did not converge"
        await asyncio.sleep(0.05)


@asynccontextmanager
async def trigger_process(runner: PollRunner) -> AsyncIterator[PollRunner]:
    """Run one Trigger process until the block exits."""
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:
        yield runner
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


@dataclass
class Environment:
    provider: LinkedInProfileFake
    project: DurableProject
    target: CountingTarget
    dex_client: dex.AsyncClient
    connection: Any

    def new_runner(self) -> PollRunner:
        routes = [ProjectProfileChangedTriggerRoute(BINDING_NAME, self.target)]
        return new_project_profile_trigger_runner(cast(LoadedProject, self.project), self.connection, routes)

    async def wait_for_commits(self, count: int) -> None:
        async def has_count() -> bool:
            state = await self.project.cursor_state()
            return state is not None and state.commit_count >= count

        await eventually(has_count)

    async def wait_for_outcomes(self, count: int) -> list[ProfileChangeOutcome]:
        await eventually(lambda: len(self.target.delivered) >= count)
        outcomes = []
        for event in list(self.target.delivered.values()):
            result = await asyncio.wait_for(
                self.dex_client.wait_for_flow(resolve_flow_id(event), timedelta(seconds=30)), 40
            )
            assert result.status == dex.FlowStatus.COMPLETED, result.error_message
            outcome = result.single_output(ProfileChangeOutcome)
            assert outcome.subject == event.payload.subject
            outcomes.append(outcome)
        return outcomes


@asynccontextmanager
async def profile_changed_environment() -> AsyncIterator[Environment]:
    """Serve the example Flow on a real Dex Worker against a fake LinkedIn member's UserInfo."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    # A subject unique per run keeps event IDs, and so Flow IDs, unique on a reused Dex server.
    provider = LinkedInProfileFake(replace(ada_profile(), sub=f"member-{time.time_ns()}"))
    reference = ConnectionRef("linkedin", CONNECTION_NAME)
    empty = SecretString("")
    credentials = Credentials("", empty, SecretString(ACCESS_TOKEN), empty)
    async with httpx.AsyncClient(transport=httpx.MockTransport(provider.handle)) as http_client:
        client = Client(
            Config(user_info_url=USER_INFO_URL, poll_interval_seconds=60),
            StaticCredentialProvider({reference: credentials}),
            http_client,
        )
        connection = new_connection(client, reference)
        flow = ProfileChangeFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="linkedin-profile-changed-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 26))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=30)
                options = dex.ClientOptions(server_address=server_address)
                async with dex.AsyncClient(registry, cache, options) as dex_client:
                    yield Environment(
                        provider,
                        DurableProject(),
                        CountingTarget(new_profile_change_flow_target(dex_client, flow)),
                        dex_client,
                        connection,
                    )
            finally:
                await worker.stop()
                await worker_task
                cache.close()


@pytest.mark.asyncio
async def test_profile_changes_start_one_flow_each_across_a_restart_with_real_dex() -> None:
    """The baseline starts nothing; each change starts one Flow; a change made while stopped arrives later."""
    async with profile_changed_environment() as environment:
        provider = environment.provider
        async with trigger_process(environment.new_runner()) as first:
            await environment.wait_for_commits(1)
            provider.update(name="Ada King", family_name="King")
            first.wake_poller(BINDING_NAME)
            [renamed] = await environment.wait_for_outcomes(1)
        assert renamed.changed_fields == ("name", "familyName")
        assert renamed.verified_profile is not None and renamed.verified_profile.name == "Ada King"

        provider.update(email="ada@king.example", email_verified=False)
        async with trigger_process(environment.new_runner()) as second:
            second.wake_poller(BINDING_NAME)
            outcomes = await environment.wait_for_outcomes(2)
            await environment.wait_for_commits(3)
        unverified = outcomes[1]
        assert unverified.changed_fields == ("email", "emailVerified")
        assert unverified.verified_profile is None, "verifiedEmailRequired records no verified profile"
        assert len(environment.target.delivered) == 2, "no change twice and never the baseline"
        state = await environment.project.cursor_state()
        assert state is not None and json.loads(state.token)["sequence"] == 2


@pytest.mark.asyncio
async def test_another_members_profile_records_a_new_baseline_with_real_dex() -> None:
    """A connection re-authorized as another member reseeds; that member's profile starts no Flow."""
    async with profile_changed_environment() as environment:
        provider = environment.provider
        async with trigger_process(environment.new_runner()):
            await environment.wait_for_commits(1)

        other_member = f"other-{time.time_ns()}"
        provider.update(sub=other_member, name="Grace Hopper", given_name="Grace", family_name="Hopper")
        async with trigger_process(environment.new_runner()) as second:

            async def is_reset() -> bool:
                state = await environment.project.cursor_state()
                return state is not None and state.reset_count == 1

            await eventually(is_reset)
            provider.update(locale="en-GB")
            second.wake_poller(BINDING_NAME)
            [outcome] = await environment.wait_for_outcomes(1)
        assert outcome.subject == other_member and outcome.changed_fields == ("locale",)
        state = await environment.project.cursor_state()
        assert state is not None and json.loads(state.token)["subject"] == other_member
