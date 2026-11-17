# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Mount the webhook receiver and route accepted events into the submission Flow."""

from collections.abc import Awaitable, Callable, MutableMapping
from typing import Any

import dex
from dex_connectors.sdk import TriggerLogger, new_dex_flow_trigger_target
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.superdurable_webhook import (
    Connection,
    ProjectRequestReceivedTriggerRoute,
    RequestReceivedWebhookRuntime,
    new_project_request_received_webhook_runtime,
)

from .flow import BINDING_NAME, SubmissionFlow, map_to_flow_input, resolve_flow_id

WEBHOOK_PATH = "/webhook/form-submissions"
READINESS_PATH = "/readyz"
_Receive = Callable[[], Awaitable[MutableMapping[str, Any]]]
_Send = Callable[[MutableMapping[str, Any]], Awaitable[None]]


def new_submission_runtime(
    project: LoadedProject,
    connection: Connection,
    client: dex.AsyncClient,
    flow: SubmissionFlow,
    logger: TriggerLogger | None = None,
) -> RequestReceivedWebhookRuntime:
    """Persist a verified event before 200 and start one Flow per event ID."""
    target = new_dex_flow_trigger_target(
        client,
        flow,
        accepts_event=lambda event: True,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
        logger=logger,
    )
    return new_project_request_received_webhook_runtime(
        project, connection, [ProjectRequestReceivedTriggerRoute(BINDING_NAME, target)], logger=logger
    )


class SubmissionWebhookApplication:
    """ASGI webhook endpoint with readiness and Trigger lifespan."""

    def __init__(self, runtime: RequestReceivedWebhookRuntime) -> None:
        """Bind one receiver runtime."""
        self._runtime = runtime

    async def __call__(self, scope: MutableMapping[str, Any], receive: _Receive, send: _Send) -> None:
        """Route one HTTP or lifespan scope."""
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
