# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deliver signed Intercom notifications through the example's receiver to a real Dex Worker and Flow."""

import asyncio
import os
import tempfile
import time
from collections import Counter
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from dataclasses import dataclass
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
)
from dex_connectors.sdk.projectconfig import LoadedProject, TriggerInboxKey

from dex_connectors.intercom import (
    TOPIC_CONVERSATION_USER_CREATED,
    TOPIC_CONVERSATION_USER_REPLIED,
    Client,
    Config,
    ConversationEventEndpointRunner,
    Credentials,
    ProjectConversationEventTriggerRoute,
    new_connection,
    new_project_conversation_event_endpoint_runner,
)
from tests.conversation_event_support import (
    ACCESS_TOKEN,
    CLIENT_SECRET,
    ProjectStub,
    conversation_item,
    notification_body,
    sign,
    signed_headers,
    trigger_binding,
)

from .flow import BINDING_NAME, CONNECTION_NAME, InboundConversationFlow, InboundConversationOutcome
from .receiver import open_inbound_conversation_receiver

WEBHOOK_URL = "http://127.0.0.1/webhooks/intercom"
INBOX_KEY = TriggerInboxKey("intercom", CONNECTION_NAME, "conversationEvent", BINDING_NAME)


class IntercomConversationsFake:
    """Answers GET /conversations/{id} with the conversation and counts each read."""

    def __init__(self) -> None:
        self.reads: Counter[str] = Counter()

    def handle(self, request: httpx.Request) -> httpx.Response:
        assert request.headers["Authorization"] == "Bearer " + ACCESS_TOKEN
        conversation_id = request.url.path.removeprefix("/conversations/")
        self.reads[conversation_id] += 1
        item = conversation_item(conversation_id)
        item["conversation_parts"] = {
            "total_count": 1,
            "conversation_parts": [
                {
                    "id": "9001",
                    "part_type": "comment",
                    "body": "Thanks",
                    "author": {"type": "admin", "id": "991"},
                }
            ],
        }
        return httpx.Response(200, json=item)


class UnavailableDexTarget:
    """Stands in for a Dex that cannot start Flows, so recorded notifications stay pending."""

    async def handle_trigger(self, context: TriggerContext, event: Any) -> None:
        raise RuntimeError("Dex is unavailable")


@asynccontextmanager
async def receiving(runner: ConversationEventEndpointRunner) -> AsyncIterator[httpx.AsyncClient]:
    """Run the receiver's binding until the block exits and yield an HTTP client for its webhook."""
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    try:
        async with asyncio.timeout(30):
            while runner.running_source_count() == 0:
                await asyncio.sleep(0.02)
        async with httpx.AsyncClient(transport=httpx.ASGITransport(app=runner)) as http:
            yield http
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
        await runner.aclose()


@dataclass
class Environment:
    provider: IntercomConversationsFake
    project: ProjectStub
    dex_client: dex.AsyncClient
    flow: InboundConversationFlow

    async def outcome(self, conversation_id: str) -> InboundConversationOutcome:
        """Wait for the conversation's Flow, which the binding starts after acknowledging Intercom."""
        flow_id = "intercom-inbound-conversation-" + conversation_id
        async with asyncio.timeout(40):
            while True:
                try:
                    result = await self.dex_client.wait_for_flow(flow_id, timedelta(seconds=30))
                    break
                except dex.FlowNotFoundError:
                    await asyncio.sleep(0.05)
        assert result.status == dex.FlowStatus.COMPLETED, result.error_message
        return result.single_output(InboundConversationOutcome)


@asynccontextmanager
async def inbound_conversation_environment() -> AsyncIterator[Environment]:
    """Serve the example Flow on a real Dex Worker against a fake Intercom conversation API."""
    server_address = os.environ["DEX_FLOW_SERVICE_ADDRESS"]
    provider = IntercomConversationsFake()
    reference = ConnectionRef("intercom", CONNECTION_NAME)
    credentials = Credentials(SecretString(ACCESS_TOKEN), SecretString(CLIENT_SECRET))
    async with httpx.AsyncClient(transport=httpx.MockTransport(provider.handle)) as http_client:
        client = Client(Config(region="eu"), StaticCredentialProvider({reference: credentials}), http_client)
        flow = InboundConversationFlow(new_connection(client, reference))
        registry = dex.Registry([flow], allow_async_handlers=True)
        with tempfile.TemporaryDirectory(prefix="intercom-inbound-conversation-blobs-") as directory:
            cache = dex.open_blob_cache(dex.BlobCacheConfig(str(Path(directory) / "blobs"), 1 << 26))
            worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
            worker_task = asyncio.create_task(worker.start())
            try:
                await worker.wait_pull_registered(timeout=30)
                async with dex.AsyncClient(
                    registry, cache, dex.ClientOptions(server_address=server_address)
                ) as dex_client:
                    project = ProjectStub(trigger_binding(BINDING_NAME, {"topics": []}))
                    yield Environment(provider, project, dex_client, flow)
            finally:
                await worker.stop()
                await worker_task
                cache.close()


@pytest.mark.asyncio
async def test_new_conversations_start_one_flow_each_across_a_restart_with_real_dex() -> None:
    """A notification acknowledged while Dex is down starts its Flow after a restart; duplicates do not."""
    async with inbound_conversation_environment() as environment:
        base = 10_000_000 + time.time_ns() % 1_000_000_000 * 10
        offline, live = str(base), str(base + 1)
        project = cast(LoadedProject, environment.project)
        while_down = notification_body(
            "notif_" + offline, TOPIC_CONVERSATION_USER_CREATED, conversation_item(offline)
        )
        first = await new_project_conversation_event_endpoint_runner(
            project,
            CONNECTION_NAME,
            [ProjectConversationEventTriggerRoute(BINDING_NAME, UnavailableDexTarget())],
        )
        async with receiving(first) as http:
            response = await http.post(WEBHOOK_URL, content=while_down, headers=signed_headers(while_down))
            assert response.status_code == 200
        pending = await environment.project.trigger_inbox(INBOX_KEY).pending()
        assert [event.id for event in pending] == ["notif_" + offline]

        second = await open_inbound_conversation_receiver(project, environment.dex_client, environment.flow)
        async with receiving(second) as http:
            replayed = await environment.outcome(offline)
            assert replayed.notification_id == "notif_" + offline
            assert replayed.customer_email == "jane@acme.example.com"
            assert replayed.part_count == 1, "the Flow read the conversation back with getConversation"

            created = notification_body(
                "notif_" + live, TOPIC_CONVERSATION_USER_CREATED, conversation_item(live)
            )
            assert (await http.head(WEBHOOK_URL)).status_code == 200
            assert (
                await http.post(WEBHOOK_URL, content=created, headers=signed_headers(created))
            ).status_code == 200
            outcome = await environment.outcome(live)
            assert (outcome.conversation_id, outcome.conversation_state) == (live, "open")

            redelivered = await http.post(WEBHOOK_URL, content=created, headers=signed_headers(created))
            assert redelivered.status_code == 200
            replied = notification_body(
                "notif_reply_" + live, TOPIC_CONVERSATION_USER_REPLIED, conversation_item(live)
            )
            assert (
                await http.post(WEBHOOK_URL, content=replied, headers=signed_headers(replied))
            ).status_code == 200
            forged = await http.post(
                WEBHOOK_URL, content=created, headers=[("X-Hub-Signature", sign(created, "another-secret"))]
            )
            assert forged.status_code == 400

            async def is_drained() -> bool:
                return await environment.project.trigger_inbox(INBOX_KEY).pending() == ()

            async with asyncio.timeout(30):
                while not await is_drained():
                    await asyncio.sleep(0.05)
        assert environment.provider.reads == Counter({offline: 1, live: 1}), "no Flow started twice"
