# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The project conversationEvent endpoint runner, ported from the Go conversation_event_runner tests."""

import asyncio
from collections.abc import Mapping
from typing import Any, cast

import httpx
import pytest
from dex_connectors.sdk import (
    TriggerCancellation,
    TriggerContext,
    TriggerDeliveryAbortedError,
    TriggerLogLevel,
    TriggerLogValue,
)
from dex_connectors.sdk.projectconfig import LoadedProject, ObjectNotFoundError, TriggerInboxKey

from dex_connectors.intercom import (
    TOPIC_CONVERSATION_ADMIN_CLOSED,
    TOPIC_CONVERSATION_USER_CREATED,
    ConversationEventEndpointRunner,
    ProjectConversationEventTriggerRoute,
    new_project_conversation_event_endpoint_runner,
)
from tests.conversation_event_support import (
    ACCESS_TOKEN,
    CLIENT_SECRET,
    CONNECTION_NAME,
    ProjectStub,
    QueueTarget,
    conversation_item,
    notification_body,
    signed_headers,
    trigger_binding,
)

WEBHOOK_URL = "https://webhook.test/webhooks/intercom"


class FailingTarget:
    """Fails every delivery, so recorded events stay pending in the inbox."""

    async def handle_trigger(self, context: TriggerContext, event: Any) -> None:
        """Keep the event pending for a retry."""
        raise RuntimeError("Dex is unavailable")


async def open_runner(project: ProjectStub, *routes: ProjectConversationEventTriggerRoute) -> Any:
    """Open the project runner from the stub."""
    return await new_project_conversation_event_endpoint_runner(
        cast(LoadedProject, project), CONNECTION_NAME, list(routes)
    )


async def run_until_receiving(
    runner: ConversationEventEndpointRunner, cancellation: TriggerCancellation
) -> Any:
    """Start runner and wait until every binding receives notifications."""
    task = asyncio.create_task(runner.run(TriggerContext(cancellation)))
    async with asyncio.timeout(10):
        while runner.running_source_count() == 0:
            await asyncio.sleep(0.01)
    return task


async def test_runner_records_before_acknowledging_and_replays_after_a_restart() -> None:
    """A 200 means the event is in the binding's inbox; the next process delivers it to its target."""
    project = ProjectStub(
        trigger_binding("inbound", {"topics": [TOPIC_CONVERSATION_USER_CREATED]}),
        trigger_binding("closed", {"topics": [TOPIC_CONVERSATION_ADMIN_CLOSED]}),
    )
    first = await open_runner(
        project,
        ProjectConversationEventTriggerRoute("inbound", FailingTarget()),
        ProjectConversationEventTriggerRoute("closed", FailingTarget()),
    )
    created = notification_body("notif_created", TOPIC_CONVERSATION_USER_CREATED, conversation_item())
    assert (await first.handle_request("POST", "/", signed_headers(created), created)).status == 503
    cancellation = TriggerCancellation()
    task = await run_until_receiving(first, cancellation)
    async with asyncio.timeout(10):
        while first.running_source_count() < 2:
            await asyncio.sleep(0.01)
    assert (await first.handle_request("HEAD", "/", [], b"")).status == 200
    assert (await first.handle_request("POST", "/", signed_headers(created), created)).status == 200
    inbox = project.trigger_inbox(
        TriggerInboxKey("intercom", CONNECTION_NAME, "conversationEvent", "inbound")
    )
    closed_inbox = project.trigger_inbox(
        TriggerInboxKey("intercom", CONNECTION_NAME, "conversationEvent", "closed")
    )
    pending = await inbox.pending()
    assert [event.id for event in pending] == ["notif_created"]
    assert await closed_inbox.pending() == (), "the closed binding filters the topic"
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await task
    await first.aclose()

    target = QueueTarget()
    second = await open_runner(
        project,
        ProjectConversationEventTriggerRoute("inbound", target),
        ProjectConversationEventTriggerRoute("closed", QueueTarget()),
    )
    cancellation = TriggerCancellation()
    task = await run_until_receiving(second, cancellation)
    replayed = await target.next_event()
    assert replayed.id == "notif_created"
    assert replayed.payload.conversation.id == "215468"
    assert replayed.payload.workspace_id == "ecahpwf5"
    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=second)) as http:
        assert (await http.head(WEBHOOK_URL)).status_code == 200
        replied = notification_body("notif_replied", "conversation.user.replied", conversation_item())
        response = await http.post(WEBHOOK_URL, content=replied, headers=signed_headers(replied))
        assert response.status_code == 200, "no binding accepts the topic, so nothing is recorded"
    assert await inbox.pending() == ()
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await task
    await second.aclose()


@pytest.mark.parametrize(
    ("bindings", "routes", "error", "message"),
    [
        ((), (), ValueError, "routes are required"),
        ((), (("", True),), ValueError, "binding name and target are required"),
        ((), (("inbound", False),), ValueError, "binding name and target are required"),
        (({"topics": []},), (("inbound", True), (" inbound ", True)), ValueError, "is duplicated"),
        ((), (("inbound", True),), ObjectNotFoundError, ""),
        (({"topics": ["ping"]},), (("inbound", True),), ValueError, 'binding "inbound": .*"ping"'),
        (({"topic": []},), (("inbound", True),), ValueError, 'binding "inbound" configuration'),
        (("topics",), (("inbound", True),), ValueError, "configuration"),
    ],
)
async def test_runner_rejects_missing_or_invalid_bindings(
    bindings: tuple[Any, ...], routes: tuple[tuple[str, bool], ...], error: type[Exception], message: str
) -> None:
    """Every route needs a unique name, a target, and a stored, valid binding."""
    project = ProjectStub(*(trigger_binding("inbound", configuration) for configuration in bindings))
    project_routes = [
        ProjectConversationEventTriggerRoute(name, cast(Any, QueueTarget() if has_target else None))
        for name, has_target in routes
    ]
    with pytest.raises(error, match=message or None):
        await open_runner(project, *project_routes)


class RecordingLogger:
    """Keeps every record at every level."""

    def __init__(self) -> None:
        self.records: list[str] = []

    def is_enabled_for(self, level: TriggerLogLevel) -> bool:
        """Enable every level, including debug."""
        return True

    def log(self, level: TriggerLogLevel, message: str, attributes: Mapping[str, TriggerLogValue]) -> None:
        """Record the message and attributes as text."""
        self.records.append(f"{level} {message} {dict(attributes)!r}")


async def test_runner_logs_never_carry_the_client_secret_or_access_token() -> None:
    """Accepted, forged, and failing deliveries log without the connection's secrets."""
    logger = RecordingLogger()
    project = ProjectStub(trigger_binding("inbound", {"topics": [TOPIC_CONVERSATION_USER_CREATED]}))
    runner = await new_project_conversation_event_endpoint_runner(
        cast(LoadedProject, project),
        CONNECTION_NAME,
        [ProjectConversationEventTriggerRoute("inbound", FailingTarget())],
        logger=logger,
    )
    cancellation = TriggerCancellation()
    task = await run_until_receiving(runner, cancellation)
    created = notification_body("notif_logged", TOPIC_CONVERSATION_USER_CREATED, conversation_item())
    assert (await runner.handle_request("POST", "/", signed_headers(created), created)).status == 200
    forged = signed_headers(created, secret="intercom-test-" + "forged")
    assert (await runner.handle_request("POST", "/", forged, created)).status == 400
    async with asyncio.timeout(10):
        while not any("delivery failed" in record for record in logger.records):
            await asyncio.sleep(0.01)
    cancellation.cancel()
    with pytest.raises(TriggerDeliveryAbortedError):
        await task
    await runner.aclose()
    assert logger.records
    for record in logger.records:
        assert CLIENT_SECRET not in record and ACCESS_TOKEN not in record, record
