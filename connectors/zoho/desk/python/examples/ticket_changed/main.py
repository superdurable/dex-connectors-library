# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Worker for the ticket change Flow and poll Zoho Desk ticket search, one Flow per changed ticket."""

import asyncio
import contextlib
import logging
import os
import tempfile
from pathlib import Path

import dex
import httpx
from dex_connectors.sdk import TriggerContext, TriggerDeliveryAbortedError
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.zoho_desk import new_project_connection

from .flow import CONNECTION_NAME, TicketChangedFlow
from .triggers import new_ticket_change_trigger_runner

# Points the example at a local Zoho Desk-compatible fake for verification only.
LOCAL_API_BASE_URL_ENVIRONMENT_VARIABLE = "ZOHO_DESK_LOCAL_API_BASE_URL"


class _LocalBaseURLTransport(httpx.AsyncBaseTransport):
    """Send every request to the local base URL instead of the data center's Zoho Desk host."""

    def __init__(self, base_url: str) -> None:
        self._base = httpx.URL(base_url.rstrip("/"))
        self._transport = httpx.AsyncHTTPTransport()

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        path = request.url.path.removeprefix("/api/v1")
        request.url = self._base.copy_with(path=self._base.path + path, query=request.url.query)
        request.headers["Host"] = request.url.netloc.decode()
        return await self._transport.handle_async_request(request)

    async def aclose(self) -> None:
        await self._transport.aclose()


def _local_http_client() -> httpx.AsyncClient | None:
    base_url = os.environ.get(LOCAL_API_BASE_URL_ENVIRONMENT_VARIABLE, "")
    if not base_url:
        return None
    logging.getLogger(__name__).warning(
        "Zoho Desk API requests go to a local base URL (%s)", LOCAL_API_BASE_URL_ENVIRONMENT_VARIABLE
    )
    return httpx.AsyncClient(transport=_LocalBaseURLTransport(base_url), timeout=12)


async def run() -> None:
    """Serve the Flow and poll the saved ticketChanged binding until the process stops."""
    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO").upper())
    project = await load_from_environment()
    http_client = _local_http_client()
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-zoho-desk-ticket-changed-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    connection = new_project_connection(project, CONNECTION_NAME, http_client)
    try:
        flow = TicketChangedFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        client_options = dex.ClientOptions(server_address=server_address)
        async with dex.AsyncClient(registry, cache, client_options) as client:
            runner = new_ticket_change_trigger_runner(project, connection, client, flow)
            worker_task = asyncio.create_task(worker.start())
            context = TriggerContext()
            try:
                await runner.run(context)
            finally:
                context.cancellation.cancel()
                await worker.stop()
                await worker_task
    finally:
        await connection.aclose()
        if http_client is not None:
            await http_client.aclose()
        cache.close()
        await project.close()


def main() -> None:
    """Run until interrupted."""
    with contextlib.suppress(KeyboardInterrupt, TriggerDeliveryAbortedError):
        asyncio.run(run())


if __name__ == "__main__":
    main()
