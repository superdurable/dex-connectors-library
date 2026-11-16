# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver signed Help Scout webhooks through the example receiver to a real Dex Worker and Flow."""

import asyncio
import base64
import hashlib
import hmac
import json
import os
import random
import tempfile
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from datetime import timedelta
from pathlib import Path

import dex
import httpx
import pytest
from dex_connectors.sdk import (
    ConnectionRef,
    SecretString,
    StaticCredentialProvider,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerEvent,
)
from dex_connectors.sdk.projectconfig import LoadedProject, TriggerInboxKey, load_from_environment

from dex_connectors.helpscout import (
    Client,
    Config,
    ConversationEvent,
    ConversationEventEndpointRunner,
    Credentials,
    ProjectConversationEventTriggerRoute,
    new_connection,
    new_project_conversation_event_endpoint_runner,
)
from tests.helpscout_conversations_fake import ACCESS_TOKEN, HelpScoutConversationsFake

from .flow import (
    CONNECTION_NAME,
    NEW_CONVERSATION_TRIGGER_BINDING,
    TRIAGED_TAG,
    TaggingRecord,
    TagNewConversationFlow,
)
from .receiver import WEBHOOK_PATH, open_new_conversation_receiver

# Not a credential: an inert webhook key for the local stand-in.
WEBHOOK_SECRET = "integration-" + "webhook-key"
INBOX_ID = 123
INBOX_KEY = TriggerInboxKey(
    "helpscout", CONNECTION_NAME, "conversationEvent", NEW_CONVERSATION_TRIGGER_BINDING
)


def write_project(directory: Path) -> Path:
    """Write the owner-only connections.json that Dex Web saves for the connection and binding."""
    path = directory / "connections.json"
    connection = {
        "connectorId": "helpscout",
        "connectionName": CONNECTION_NAME,
        "modulePath": "github.com/superdurable/dex-connectors-library/connectors/helpscout/go",
        "provider": "helpscout",
        "configuration": {},
        "credentials": {
            "app_id": "inert-app",
            "app_secret": "inert-app-key",
            "webhook_secret": WEBHOOK_SECRET,
        },
    }
    binding = {
        "connectorId": "helpscout",
        "connectionName": CONNECTION_NAME,
        "triggerName": "conversationEvent",
        "bindingName": NEW_CONVERSATION_TRIGGER_BINDING,
        "configuration": {"mailboxId": INBOX_ID},
    }
    document = {
        "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
        "connections": [connection],
        "triggerBindings": [binding],
    }
    path.write_text(json.dumps(document))
    path.chmod(0o600)
    return path


def event_id(event: str, conversation_id: int, body: bytes) -> str:
    """The Trigger event ID: event, conversation ID, and 32 hex digits of the body's SHA-256."""
    return f"{event}:{conversation_id}:{hashlib.sha256(body).hexdigest()[:32]}"


def flow_id(event: str, conversation_id: int, body: bytes) -> str:
    """The example's Flow ID for one delivery."""
    return "helpscout-" + event_id(event, conversation_id, body).replace(":", "-")


async def post_signed(
    receiver: ConversationEventEndpointRunner, event: str, body: bytes, secret: str = WEBHOOK_SECRET
) -> int:
    """Post one webhook as Help Scout signs it through the receiver's ASGI application."""
    signature = base64.b64encode(hmac.new(secret.encode(), body, hashlib.sha1).digest()).decode()
    transport = httpx.ASGITransport(app=receiver)
    async with httpx.AsyncClient(transport=transport, base_url="http://receiver") as http_client:
        response = await http_client.post(
            WEBHOOK_PATH,
            content=body,
            headers={"X-HelpScout-Event": event, "X-HelpScout-Signature": signature},
        )
    return response.status_code


async def until_answered(receiver: ConversationEventEndpointRunner, event: str, body: bytes) -> int:
    """Retry like Help Scout while the binding is still starting (503)."""
    for _ in range(500):
        status = await post_signed(receiver, event, body)
        if status != 503:
            return status
        await asyncio.sleep(0.01)
    raise AssertionError("receiver did not start")


@asynccontextmanager
async def running(receiver: ConversationEventEndpointRunner) -> AsyncIterator[None]:
    """Run the receiver's binding until the block ends, as an ASGI lifespan does."""
    context = TriggerContext()
    task = asyncio.create_task(receiver.run(context))
    try:
        yield
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


async def pending_event_ids(project: LoadedProject) -> list[str]:
    """Return the IDs still waiting in the binding's durable inbox."""
    return [event.id for event in await project.trigger_inbox(INBOX_KEY).pending()]


async def wait_for_tagging(dex_client: dex.AsyncClient, started_flow_id: str) -> TaggingRecord:
    """Wait for the Flow the receiver starts for one delivery to complete."""
    # The Trigger starts the Flow asynchronously, so poll until it exists.
    for _ in range(1000):
        try:
            result = await asyncio.wait_for(
                dex_client.wait_for_flow(started_flow_id, timedelta(seconds=20)), 30
            )
        except dex.FlowNotFoundError:
            await asyncio.sleep(0.02)
            continue
        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
        return result.single_output(TaggingRecord)
    raise AssertionError(f"Flow {started_flow_id} was not started")


class HeldTarget:
    """Never consumes, standing in for an application that stopped before delivery."""

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[ConversationEvent]) -> None:
        await context.cancellation.wait()
        context.cancellation.raise_if_cancelled()


async def record_without_consuming(body: bytes, expected_event_id: str) -> None:
    """Act as a first process that records one delivery and stops before its target consumes it."""
    project = await load_from_environment()
    route = ProjectConversationEventTriggerRoute(NEW_CONVERSATION_TRIGGER_BINDING, HeldTarget())
    held = await new_project_conversation_event_endpoint_runner(project, CONNECTION_NAME, [route])
    try:
        async with running(held):
            assert await until_answered(held, "convo.created", body) == 200
        assert await pending_event_ids(project) == [expected_event_id]
    finally:
        await held.aclose()
        await project.close()


@asynccontextmanager
async def tagging_worker(
    provider: HelpScoutConversationsFake, directory: Path
) -> AsyncIterator[tuple[dex.AsyncClient, TagNewConversationFlow]]:
    """Serve the tagging Flow on a real Dex Worker whose connector Steps call the provider stand-in."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    reference = ConnectionRef("helpscout", CONNECTION_NAME)
    credentials = Credentials(
        "inert-app", SecretString("inert-app-key"), SecretString(ACCESS_TOKEN), SecretString(WEBHOOK_SECRET)
    )
    async with httpx.AsyncClient(transport=httpx.MockTransport(provider.handle)) as http_client:
        client = Client(Config(), StaticCredentialProvider({reference: credentials}), http_client)
        flow = TagNewConversationFlow(new_connection(client, reference))
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache = dex.open_blob_cache(dex.BlobCacheConfig(str(directory / "blobs"), 1 << 24))
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        worker_task = asyncio.create_task(worker.start())
        try:
            await worker.wait_pull_registered(timeout=30)
            options = dex.ClientOptions(server_address=server_address)
            async with dex.AsyncClient(registry, cache, options) as dex_client:
                yield dex_client, flow
        finally:
            await worker.stop()
            await worker_task
            cache.close()


@pytest.mark.asyncio
async def test_new_conversations_start_one_tagging_flow_each_and_survive_a_restart(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """A recorded delivery replays; a new conversation is tagged once; others start nothing."""
    provider = HelpScoutConversationsFake()
    first_id = random.randrange(10**9, 10**12)
    replayed, live, other_inbox, pending, sentinel = (first_id + offset for offset in range(5))
    replayed_body = provider.add(replayed, INBOX_ID)
    live_body = provider.add(live, INBOX_ID)
    other_inbox_body = provider.add(other_inbox, 999)
    pending_body = provider.add(pending, INBOX_ID, "pending")
    sentinel_body = provider.add(sentinel, INBOX_ID)
    with tempfile.TemporaryDirectory(prefix="helpscout-python-webhook-") as directory:
        monkeypatch.setenv("DEX_CONNECTOR_CONFIG_FILE", str(write_project(Path(directory))))
        await record_without_consuming(replayed_body, event_id("convo.created", replayed, replayed_body))
        async with tagging_worker(provider, Path(directory)) as (dex_client, flow):
            project = await load_from_environment()
            receiver = await open_new_conversation_receiver(project, dex_client, flow)
            try:
                assert await post_signed(receiver, "convo.created", live_body, "forged-key") == 400
                async with running(receiver):
                    recovered = await wait_for_tagging(
                        dex_client, flow_id("convo.created", replayed, replayed_body)
                    )
                    assert recovered.tags == (TRIAGED_TAG,)

                    assert await until_answered(receiver, "convo.created", live_body) == 200
                    live_flow = flow_id("convo.created", live, live_body)
                    record = await wait_for_tagging(dex_client, live_flow)
                    assert record.stage == "completed" and record.status == "active"
                    assert record.request.mailbox_id == INBOX_ID
                    assert record.newest_thread_types == ("customer",)
                    assert record.tags == (TRIAGED_TAG,)
                    assert provider.conversations[live].tags == [TRIAGED_TAG]
                    started_run = (await dex_client.describe_flow(live_flow)).run_id

                    # Help Scout redelivers the same body, which is a duplicate start.
                    assert await post_signed(receiver, "convo.created", live_body) == 200
                    assert await post_signed(receiver, "convo.tags", live_body) == 200
                    assert await post_signed(receiver, "convo.created", other_inbox_body) == 200
                    assert await post_signed(receiver, "convo.created", pending_body) == 200
                    assert await post_signed(receiver, "convo.created", sentinel_body) == 200
                    # One binding delivers in arrival order, so the sentinel completes last.
                    await wait_for_tagging(dex_client, flow_id("convo.created", sentinel, sentinel_body))
                    assert (await dex_client.describe_flow(live_flow)).run_id == started_run
                    assert provider.tag_writes(live) == 1
                    for skipped in (
                        flow_id("convo.tags", live, live_body),
                        flow_id("convo.created", other_inbox, other_inbox_body),
                        flow_id("convo.created", pending, pending_body),
                    ):
                        with pytest.raises(dex.FlowNotFoundError):
                            await dex_client.describe_flow(skipped)
                    assert provider.tag_writes(other_inbox) == provider.tag_writes(pending) == 0
                    assert await pending_event_ids(project) == []
            finally:
                await receiver.aclose()
                await project.close()
