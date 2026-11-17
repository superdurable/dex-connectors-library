# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Mount Stripe's webhook and bind it to the Checkout event Flow."""

from collections.abc import Awaitable, Callable, MutableMapping
from typing import Any

import dex
from dex_connectors.sdk import TriggerLogger, new_dex_flow_trigger_target
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.stripe import (
    CheckoutSessionWebhookRuntime,
    Connection,
    ProjectCheckoutSessionUpdatedTriggerRoute,
    new_project_checkout_session_webhook_runtime,
)

from .flow import BINDING_NAME, CheckoutEventFlow, map_to_flow_input, resolve_flow_id

WEBHOOK_PATH = "/stripe/checkout-events"
READINESS_PATH = "/readyz"
_Receive = Callable[[], Awaitable[MutableMapping[str, Any]]]
_Send = Callable[[MutableMapping[str, Any]], Awaitable[None]]


def new_checkout_event_runtime(
    project: LoadedProject,
    connection: Connection,
    client: dex.AsyncClient,
    flow: CheckoutEventFlow,
    logger: TriggerLogger | None = None,
) -> CheckoutSessionWebhookRuntime:
    """Persist signed Stripe events before acknowledgment and start one Flow per event."""
    target = new_dex_flow_trigger_target(
        client,
        flow,
        accepts_event=lambda event: True,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
        logger=logger,
    )
    return new_project_checkout_session_webhook_runtime(
        project, connection, [ProjectCheckoutSessionUpdatedTriggerRoute(BINDING_NAME, target)], logger=logger
    )


class CheckoutWebhookApplication:
    """ASGI webhook with a readiness endpoint and Trigger lifespan."""

    def __init__(self, runtime: CheckoutSessionWebhookRuntime) -> None:
        """Serve one Checkout Session webhook runtime."""
        self._runtime = runtime

    async def __call__(self, scope: MutableMapping[str, Any], receive: _Receive, send: _Send) -> None:
        """Route HTTP and lifespan scopes."""
        if scope["type"] == "http" and scope.get("path") == READINESS_PATH:
            is_ready = self._runtime.running_source_count() > 0
            await _send_text(send, 200 if is_ready else 503, "ready" if is_ready else "not receiving yet")
        elif scope["type"] == "http" and scope.get("path") != WEBHOOK_PATH:
            await _send_text(send, 404, "not found")
        else:
            await self._runtime(scope, receive, send)


async def _send_text(send: _Send, status: int, message: str) -> None:
    await send(
        {"type": "http.response.start", "status": status, "headers": [(b"content-type", b"text/plain")]}
    )
    await send({"type": "http.response.body", "body": (message + "\n").encode()})
