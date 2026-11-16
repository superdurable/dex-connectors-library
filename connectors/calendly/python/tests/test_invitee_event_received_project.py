# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Stored inviteeEventReceived bindings behind durable inboxes: record, replay, and route validation."""

import asyncio
import json
from typing import Any

import pytest
from dex_connectors.sdk import TriggerContext, TriggerDeliveryAbortedError, TriggerEvent, TriggerTarget
from dex_connectors.sdk.projectconfig import (
    Configuration,
    ConnectionConfiguration,
    InMemoryObjectStore,
    PendingTriggerEvent,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    new_durable_trigger_target,
)
from dex_connectors.sdk.projectconfig.storage import ObjectNotFoundError

from dex_connectors.calendly import InviteeEvent, ProjectInviteeEventReceivedTriggerRoute, new_connection
from dex_connectors.calendly.triggers import _decode_pending_invitee_event, _new_endpoint_runner
from tests.test_invitee_event_received_trigger import (
    CANCELED_BODY,
    CONNECTION,
    CREATED_BODY,
    RECORDING,
    TYPE_URI,
    RecordingTarget,
    calendly_client,
    eventually,
    signature,
)

SCOPE = Scope("calendly-invitee-recorder", "live")


def project_configuration(bindings: dict[str, object]) -> Configuration:
    """The project configuration Dex Web saves for the connection and its bindings."""
    connection = ConnectionConfiguration(
        "calendly",
        CONNECTION.name,
        "github.com/superdurable/dex-connectors-library/connectors/calendly/go",
        "calendly",
        {},
        "personal-access-token",
    )
    return Configuration(
        SCOPE,
        1,
        (connection,),
        tuple(
            {
                "connectorId": "calendly",
                "connectionName": CONNECTION.name,
                "triggerName": "inviteeEventReceived",
                "bindingName": binding_name,
                "configuration": binding_configuration,
            }
            for binding_name, binding_configuration in bindings.items()
        ),
    )


class DurableInboxes:
    """Real project Trigger inboxes over one in-memory object store, shared by restarted processes."""

    def __init__(self) -> None:
        self.objects = InMemoryObjectStore()
        self.keys: list[TriggerInboxKey] = []

    def inbox(self, key: TriggerInboxKey) -> TriggerInbox:
        return TriggerInbox(self.objects, SCOPE, key)

    def wrap(self, key: TriggerInboxKey, target: TriggerTarget[InviteeEvent]) -> TriggerTarget[InviteeEvent]:
        self.keys.append(key)
        return new_durable_trigger_target(
            self.inbox(key), target, decode_payload=_decode_pending_invitee_event
        )


class HeldTarget:
    """Never consumes, standing in for a process that stops before its Flow starts."""

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[InviteeEvent]) -> None:
        await context.cancellation.wait()
        context.cancellation.raise_if_cancelled()


async def serve(runner: Any, body: str) -> int:
    headers = [("Calendly-Webhook-Signature", signature(body))]
    return int((await runner.handle_request("POST", "/webhooks/calendly", headers, body.encode())).status)


async def run_until(runner: Any, condition: Any) -> None:
    context = TriggerContext()
    task = asyncio.create_task(runner.run(context))
    try:
        await condition()
    finally:
        context.cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task


async def test_each_binding_records_before_acknowledging_and_replays_after_a_restart() -> None:
    """A recorded event survives a stopped process; a Go-written pending event also replays."""
    inboxes = DurableInboxes()
    configuration = project_configuration(
        {
            "invitee-created": {"events": ["invitee.created"], "eventTypeUri": TYPE_URI},
            "cancellations": {"events": ["invitee.canceled"]},
        }
    )
    key = TriggerInboxKey("calendly", CONNECTION.name, "inviteeEventReceived", "invitee-created")
    go_case = RECORDING["created"]
    go_event = PendingTriggerEvent(
        "invitee.created:EVENT0001:GOREPLICA",
        (
            '{"eventId":"invitee.created:EVENT0001:GOREPLICA","occurredAt":"2026-09-30T11:59:00Z","payload":'
            + go_case["payloadJson"]
            + "}"
        ).encode(),
    )
    first = _new_endpoint_runner(
        new_connection(calendly_client(), CONNECTION),
        configuration,
        [ProjectInviteeEventReceivedTriggerRoute("invitee-created", HeldTarget())],
        inboxes.wrap,
        None,
    )
    assert await serve(first, CREATED_BODY) == 503, "Calendly retries until the binding runs"

    async def record_one() -> None:
        for _ in range(500):
            if await serve(first, CREATED_BODY) == 200:
                return
            await asyncio.sleep(0.01)
        raise AssertionError("binding did not start")

    await run_until(first, record_one)
    # A Go replica sharing the project storage records the next event.
    await inboxes.inbox(key).add(go_event)
    pending = [event.id for event in await inboxes.inbox(key).pending()]
    assert pending == ["invitee.created:EVENT0001:INVITEE01", "invitee.created:EVENT0001:GOREPLICA"]

    created, canceled = RecordingTarget(), RecordingTarget()
    second = _new_endpoint_runner(
        new_connection(calendly_client(), CONNECTION),
        configuration,
        [
            ProjectInviteeEventReceivedTriggerRoute(" invitee-created ", created),
            ProjectInviteeEventReceivedTriggerRoute("cancellations", canceled),
        ],
        inboxes.wrap,
        None,
    )

    async def deliver_live() -> None:
        await eventually(lambda: len(created.events) == 2)
        assert await serve(second, CANCELED_BODY) == 200
        await eventually(lambda: len(canceled.events) == 1)

    await run_until(second, deliver_live)
    assert [event.id for event in created.events] == pending
    assert created.events[1].payload == _decode_pending_invitee_event(go_case["payloadJson"], b"")
    assert canceled.events[0].id == "invitee.canceled:EVENT0001:INVITEE02"
    assert await inboxes.inbox(key).pending() == ()
    assert inboxes.keys[-1] == TriggerInboxKey(
        "calendly", CONNECTION.name, "inviteeEventReceived", "cancellations"
    )


@pytest.mark.parametrize(
    ("routes", "error"),
    [
        ([], ValueError),
        ([ProjectInviteeEventReceivedTriggerRoute("missing", RecordingTarget())], ObjectNotFoundError),
        ([ProjectInviteeEventReceivedTriggerRoute("invalid", RecordingTarget())], ValueError),
        ([ProjectInviteeEventReceivedTriggerRoute("unknown", RecordingTarget())], ValueError),
        ([ProjectInviteeEventReceivedTriggerRoute(" ", RecordingTarget())], ValueError),
        ([ProjectInviteeEventReceivedTriggerRoute("invitee-created", None)], ValueError),  # type: ignore[arg-type]
        ([ProjectInviteeEventReceivedTriggerRoute("invitee-created", RecordingTarget())] * 2, ValueError),
    ],
)
def test_routes_need_a_stored_valid_binding_a_target_and_a_unique_name(
    routes: list[ProjectInviteeEventReceivedTriggerRoute], error: type[Exception]
) -> None:
    """Construction refuses what Go's NewProjectInviteeEventReceivedEndpointRunner refuses."""
    configuration = project_configuration(
        {
            "invitee-created": {},
            "invalid": {"events": ["invitee_no_show.created"]},
            "unknown": {"eventTypes": [json.dumps("invitee.created")]},
        }
    )
    connection = new_connection(calendly_client(), CONNECTION)
    with pytest.raises(error):
        _new_endpoint_runner(connection, configuration, routes, DurableInboxes().wrap, None)
