# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Wire the envelope-outcomes binding to its Flow target and mount it with a readiness check."""

from collections.abc import Awaitable, Callable, MutableMapping
from typing import Any

import dex
from dex_connectors.sdk import TriggerLogger, new_dex_flow_trigger_target
from dex_connectors.sdk.projectconfig import LoadedProject

from dex_connectors.docusign import (
    Connection,
    EnvelopeEventReceivedEndpointRunner,
    ProjectEnvelopeEventReceivedTriggerRoute,
    new_project_envelope_event_received_endpoint_runner,
)

from .flow import (
    ENVELOPE_OUTCOME_TRIGGER_BINDING,
    EnvelopeOutcomeFlow,
    accept_correlated_envelope_event,
    map_to_flow_input,
    resolve_flow_id,
)

# The Connect configuration's URL to Publish; expose it through an HTTPS tunnel in local development.
CONNECT_PATH = "/docusign/connect"
READINESS_PATH = "/readyz"

_Receive = Callable[[], Awaitable[MutableMapping[str, Any]]]
_Send = Callable[[MutableMapping[str, Any]], Awaitable[None]]


def new_envelope_outcome_endpoint_runner(
    project: LoadedProject,
    connection: Connection,
    client: dex.AsyncClient,
    flow: EnvelopeOutcomeFlow,
    logger: TriggerLogger | None = None,
) -> EnvelopeEventReceivedEndpointRunner:
    """Start one Flow per correlated envelope outcome, through the binding's durable project inbox."""
    target = new_dex_flow_trigger_target(
        client,
        flow,
        accepts_event=accept_correlated_envelope_event,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
        logger=logger,
    )
    return new_project_envelope_event_received_endpoint_runner(
        project,
        connection,
        [ProjectEnvelopeEventReceivedTriggerRoute(ENVELOPE_OUTCOME_TRIGGER_BINDING, target)],
        logger=logger,
    )


class ConnectWebhookApplication:
    """ASGI app: Connect at CONNECT_PATH, readiness at READINESS_PATH, and the runner's lifespan."""

    def __init__(self, runner: EnvelopeEventReceivedEndpointRunner) -> None:
        """Serve runner; the server's lifespan starts and stops the binding."""
        self._runner = runner

    async def __call__(self, scope: MutableMapping[str, Any], receive: _Receive, send: _Send) -> None:
        """Route one ASGI scope; lifespan starts and stops the binding with the server."""
        if scope["type"] == "http" and scope.get("path") == READINESS_PATH:
            is_ready = self._runner.running_source_count() > 0
            await _send_text(send, 200 if is_ready else 503, "ready" if is_ready else "not receiving yet")
        elif scope["type"] == "http" and scope.get("path") != CONNECT_PATH:
            await _send_text(send, 404, "not found")
        else:
            await self._runner(scope, receive, send)


async def _send_text(send: _Send, status: int, text: str) -> None:
    headers = [(b"content-type", b"text/plain; charset=utf-8")]
    await send({"type": "http.response.start", "status": status, "headers": headers})
    await send({"type": "http.response.body", "body": (text + "\n").encode()})
