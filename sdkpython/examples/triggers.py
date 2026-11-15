# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Trigger delivery: a relayed webhook recorded in a durable inbox, then routed by a target.

It runs offline: the inbox is in memory and the request is handed to `handle_request` directly. The
`serve_*` functions show the two supported ways to serve the same runner with an ASGI server.
"""

import asyncio
import importlib
import json
import logging
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk import (
    ConnectionRef,
    StaticCredentialProvider,
    TriggerBindingRef,
    TriggerCancellation,
    TriggerConfig,
    TriggerContext,
    TriggerDefinition,
    TriggerDeliveryAbortedError,
    TriggerEvent,
    TriggerRef,
    UndeliverableTriggerError,
    new_trigger,
)
from dex_connectors.sdk.projectconfig import (
    InMemoryObjectStore,
    Scope,
    TriggerInbox,
    TriggerInboxKey,
    new_durable_trigger_target,
    parse_trigger_payload_json,
)
from dex_connectors.sdk.relaysignature import (
    RELAY_SIGNATURE_HEADER,
    RELAY_TIMESTAMP_HEADER,
    sign_relayed_request,
    verify_relay_signature,
)
from dex_connectors.sdk.webhooktrigger import (
    WebhookEndpoint,
    WebhookEndpointConfig,
    WebhookEndpointRunner,
    WebhookRequest,
)

CONNECTION = ConnectionRef("sample", "workspace")
SCOPE_TOKEN = "example-scope-token"
DEFINITION = TriggerDefinition(TriggerRef("sample", "itemCreated"), "Receive a new item.")
ItemPayload = dict[str, str]


async def verify_relayed_item(
    request: WebhookRequest, credentials: object, cancellation: TriggerCancellation
) -> None:
    """Refuse a request the Dex host did not sign with the scope token."""
    verification = verify_relay_signature(SCOPE_TOKEN, request.headers, request.body, request.received_at)
    if not verification.is_valid:
        raise ValueError(f"relayed request refused: {verification.refusal}")


def decode_item_created(request: WebhookRequest) -> TriggerEvent[ItemPayload] | None:
    """Decode one item event; other event types are valid but unhandled."""
    body = json.loads(request.body)
    if body.get("type") != "item.created":
        return None
    return TriggerEvent(
        body["eventId"], datetime.fromisoformat(body["occurredAt"]), {"item_id": body["itemId"]}
    )


def decode_stored_item(payload_json: str, payload_bytes: bytes) -> ItemPayload:
    """Decode a replayed item payload; anything but a string item ID is refused."""
    payload = parse_trigger_payload_json(payload_bytes)
    if not isinstance(payload, dict) or not isinstance(payload.get("item_id"), str):
        raise ValueError("stored item payload is invalid")
    return {"item_id": payload["item_id"]}


class ItemRecorder:
    """Stands in for `new_dex_flow_trigger_target`, which would start one Flow per item."""

    def __init__(self) -> None:
        self.item_ids: list[str] = []

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[ItemPayload]) -> None:
        """Consume the event, or reject it for good when it has no item."""
        if not event.payload["item_id"]:
            raise UndeliverableTriggerError("the event names no item")
        self.item_ids.append(event.payload["item_id"])


def new_item_runner(recorder: ItemRecorder) -> tuple[WebhookEndpointRunner, TriggerInbox]:
    """Wire one endpoint, one durable binding, and the runner that serves them."""
    endpoint: WebhookEndpoint[dict[str, str], ItemPayload] = WebhookEndpoint(
        WebhookEndpointConfig(
            connector_id="sample",
            trigger_name="itemCreated",
            connection=CONNECTION,
            credentials=StaticCredentialProvider({CONNECTION: {}}),
            verify_request=verify_relayed_item,
            decode_event=decode_item_created,
            maximum_body_bytes=1 << 20,
        )
    )
    # An application uses project.trigger_inbox(key), which persists across restarts.
    key = TriggerInboxKey("sample", "workspace", "itemCreated", "start")
    inbox = TriggerInbox(InMemoryObjectStore(), Scope("example", "preview"), key)
    target = new_durable_trigger_target(inbox, recorder, decode_payload=decode_stored_item)
    binding = TriggerBindingRef(CONNECTION, DEFINITION.trigger, "start")
    runner = new_trigger(TriggerConfig(DEFINITION, binding, endpoint.new_source(), target))
    return WebhookEndpointRunner(endpoint, [runner]), inbox


def serve_with_lifespan(application: WebhookEndpointRunner) -> None:
    """Serve with uvicorn; lifespan startup runs the Triggers on the server's event loop."""
    uvicorn: Any = importlib.import_module("uvicorn")
    uvicorn.run(application, host="127.0.0.1", port=8080)


async def serve_on_owned_loop(application: WebhookEndpointRunner) -> None:
    """Serve next to the runner on a loop the host owns, stopping the runner when the server returns."""
    uvicorn: Any = importlib.import_module("uvicorn")
    config = uvicorn.Config(application, host="127.0.0.1", port=8080, lifespan="off")
    cancellation = TriggerCancellation()

    async def run_triggers() -> None:
        try:
            await application.run(TriggerContext(cancellation))
        except TriggerDeliveryAbortedError:
            return

    async with asyncio.TaskGroup() as group:
        group.create_task(run_triggers())
        await uvicorn.Server(config).serve()
        cancellation.cancel()


async def main() -> None:
    """Record one signed request durably, deliver it, and leave the inbox empty."""
    logging.basicConfig()
    logging.getLogger("dex_connectors.sdk.trigger").setLevel(logging.INFO)
    recorder = ItemRecorder()
    application, inbox = new_item_runner(recorder)
    cancellation = TriggerCancellation()
    running = asyncio.create_task(application.run(TriggerContext(cancellation)))
    body = json.dumps(
        {
            "type": "item.created",
            "eventId": "Ev1",
            "occurredAt": "2025-10-12T00:00:00+00:00",
            "itemId": "item-1",
        }
    ).encode()
    signed_at = datetime.now(UTC)
    headers = [
        (RELAY_TIMESTAMP_HEADER, str(int(signed_at.timestamp()))),
        (RELAY_SIGNATURE_HEADER, sign_relayed_request(SCOPE_TOKEN, signed_at, body)),
    ]
    try:
        for _ in range(1000):
            response = await application.handle_request("POST", "/hooks/items", headers, body)
            if response.status != 503:
                break
            # The source registers once the runner task starts.
            await asyncio.sleep(0.01)
        if response.status != 200:
            raise RuntimeError(f"webhook answered {response.status}")
        for _ in range(1000):
            if recorder.item_ids and not await inbox.pending():
                break
            await asyncio.sleep(0.01)
        if recorder.item_ids != ["item-1"] or await inbox.pending():
            raise RuntimeError("Trigger example did not deliver the event")
        print("delivered", recorder.item_ids[0])
    finally:
        cancellation.cancel()
        try:
            await running
        except TriggerDeliveryAbortedError:
            pass


if __name__ == "__main__":
    asyncio.run(main())
