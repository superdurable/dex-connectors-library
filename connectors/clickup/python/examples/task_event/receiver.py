# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Mount the ClickUp webhook and bind it to the task event Flow."""

from collections.abc import Awaitable, Callable, MutableMapping
from typing import Any

import dex
from dex_connectors.sdk import TriggerLogger, new_dex_flow_trigger_target
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.clickup import (
    Connection,
    ProjectTaskEventTriggerRoute,
    TaskEventEndpointRunner,
    new_project_task_event_endpoint_runner,
)

from .flow import BINDING_NAME, TaskEventFlow, map_to_flow_input, resolve_flow_id

WEBHOOK_PATH = "/clickup/task-events"
READINESS_PATH = "/readyz"
_Receive = Callable[[], Awaitable[MutableMapping[str, Any]]]
_Send = Callable[[MutableMapping[str, Any]], Awaitable[None]]


def new_task_event_endpoint_runner(
    project: LoadedProject,
    connection: Connection,
    client: dex.AsyncClient,
    flow: TaskEventFlow,
    logger: TriggerLogger | None = None,
) -> TaskEventEndpointRunner:
    """Persist signed events before acknowledgment and start one Flow per event."""
    target = new_dex_flow_trigger_target(
        client,
        flow,
        accepts_event=lambda event: True,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
        logger=logger
    )
    return new_project_task_event_endpoint_runner(
        project, connection, [ProjectTaskEventTriggerRoute(BINDING_NAME, target)], logger=logger
    )


class TaskEventWebhookApplication:
    """ASGI webhook with a readiness endpoint and Trigger lifespan."""

    def __init__(self, runner: TaskEventEndpointRunner) -> None:
        """Serve one endpoint runner."""
        self._runner = runner

    async def __call__(self, scope: MutableMapping[str, Any], receive: _Receive, send: _Send) -> None:
        """Route HTTP and lifespan scopes."""
        if scope["type"] == "http" and scope.get("path") == READINESS_PATH:
            is_ready = self._runner.running_source_count() > 0
            await _send_text(send, 200 if is_ready else 503, "ready" if is_ready else "not receiving yet")
        elif scope["type"] == "http" and scope.get("path") != WEBHOOK_PATH:
            await _send_text(send, 404, "not found")
        else:
            await self._runner(scope, receive, send)


async def _send_text(send: _Send, status: int, message: str) -> None:
    await send(
        {"type": "http.response.start", "status": status, "headers": [(b"content-type", b"text/plain")]}
    )
    await send({"type": "http.response.body", "body": (message + "\n").encode()})
