# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the Typeform webhook receiver and the response intake's Dex Worker on one event loop.

uvicorn is not a package dependency; run with ``uv run --with uvicorn``.
"""

import asyncio
import logging
import os
import tempfile
from pathlib import Path
from typing import Any

import dex
from dex_connectors.sdk import TriggerLogger, TriggerTarget, new_dex_flow_trigger_target
from dex_connectors.sdk.projectconfig import LoadedProject, load_from_environment

from dex_connectors.typeform import (
    FormResponseEvent,
    ProjectResponseSubmittedTriggerRoute,
    ResponseSubmittedEndpointRunner,
    new_project_response_submitted_endpoint_runner,
)

from .flow import (
    BINDING_NAME,
    CONNECTION_NAME,
    ResponseIntakeFlow,
    accept_response,
    map_to_flow_input,
    resolve_flow_id,
)

WEBHOOK_PATH = "/webhooks/typeform"
"""The subscription's callback path; expose it through an HTTPS tunnel in local development."""
READINESS_PATH = "/readyz"
"""Answers 200 once the binding receives deliveries."""


def new_response_target(
    dex_client: dex.AsyncClient, flow: ResponseIntakeFlow, logger: TriggerLogger | None = None
) -> TriggerTarget[FormResponseEvent]:
    """Start one ResponseIntakeFlow per admitted response; the event ID is the Flow-start request ID."""
    return new_dex_flow_trigger_target(
        dex_client,
        flow,
        accepts_event=accept_response,
        resolve_flow_id=resolve_flow_id,
        map_to_flow_input=map_to_flow_input,
        logger=logger,
    )


async def open_response_receiver(
    project: LoadedProject,
    dex_client: dex.AsyncClient,
    flow: ResponseIntakeFlow,
) -> ResponseSubmittedEndpointRunner:
    """Route the stored binding to the Flow through its durable inbox; close it with ``aclose``."""
    route = ProjectResponseSubmittedTriggerRoute(BINDING_NAME, new_response_target(dex_client, flow))
    return await new_project_response_submitted_endpoint_runner(project, CONNECTION_NAME, [route])


class ResponseIntakeApplication:
    """ASGI 3 application: the webhook at WEBHOOK_PATH, readiness at READINESS_PATH, 404 elsewhere."""

    def __init__(self, receiver: ResponseSubmittedEndpointRunner) -> None:
        """Serve receiver, whose bindings start with the ASGI lifespan."""
        self.receiver = receiver

    async def __call__(self, scope: Any, receive: Any, send: Any) -> None:
        """Dispatch one ASGI scope."""
        path = scope.get("path") if scope.get("type") == "http" else None
        if path is None or path == WEBHOOK_PATH:
            await self.receiver(scope, receive, send)
            return
        is_ready = path == READINESS_PATH and self.receiver.running_source_count() > 0
        status = 200 if is_ready else 503 if path == READINESS_PATH else 404
        body = (
            b"ready\n" if is_ready else b"Typeform binding is not receiving yet\n" if status == 503 else b""
        )
        await send(
            {
                "type": "http.response.start",
                "status": status,
                "headers": [(b"content-type", b"text/plain; charset=utf-8")],
            }
        )
        await send({"type": "http.response.body", "body": body})


async def main() -> None:
    """Load the project, register the Flow, and receive Typeform webhooks until shutdown."""
    import uvicorn

    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO").upper())
    project = await load_from_environment()
    flow = ResponseIntakeFlow()
    registry = dex.Registry([flow], allow_async_handlers=True)
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-typeform-response-intake-python-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    try:
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=address))
        async with dex.AsyncClient(registry, cache, dex.ClientOptions(server_address=address)) as dex_client:
            receiver = await open_response_receiver(project, dex_client, flow)
            host, _, port = os.environ.get("WEBHOOK_BIND_ADDRESS", "127.0.0.1:8852").rpartition(":")
            server = uvicorn.Server(
                uvicorn.Config(ResponseIntakeApplication(receiver), host=host, port=int(port), lifespan="on")
            )
            worker_task = asyncio.create_task(worker.start())
            try:
                await server.serve()
            finally:
                await worker.stop()
                await worker_task
                await receiver.aclose()
    finally:
        cache.close()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
