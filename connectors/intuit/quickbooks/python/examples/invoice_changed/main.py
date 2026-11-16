# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Worker for the invoice change Flow and poll the company, one Flow per invoice version."""

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

from dex_connectors.intuit_quickbooks import new_project_connection

from .flow import CONNECTION_NAME, InvoiceChangedFlow
from .triggers import new_invoice_change_trigger_runner

# Points the example at a local QuickBooks-compatible fake for verification only.
LOCAL_PROVIDER_URL_ENVIRONMENT_VARIABLE = "QUICKBOOKS_LOCAL_PROVIDER_URL"


class _LocalProviderTransport(httpx.AsyncBaseTransport):
    """Send every QuickBooks API and Intuit token request to the local provider URL instead."""

    def __init__(self, provider_url: str) -> None:
        self._base = httpx.URL(provider_url.rstrip("/"))
        self._transport = httpx.AsyncHTTPTransport()

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        request.url = request.url.copy_with(
            scheme=self._base.scheme, host=self._base.host, port=self._base.port
        )
        request.headers["Host"] = request.url.netloc.decode()
        return await self._transport.handle_async_request(request)

    async def aclose(self) -> None:
        await self._transport.aclose()


def _local_http_client() -> httpx.AsyncClient | None:
    provider_url = os.environ.get(LOCAL_PROVIDER_URL_ENVIRONMENT_VARIABLE, "")
    if not provider_url:
        return None
    logging.getLogger(__name__).warning(
        "QuickBooks API and token requests go to a local URL (%s)", LOCAL_PROVIDER_URL_ENVIRONMENT_VARIABLE
    )
    return httpx.AsyncClient(transport=_LocalProviderTransport(provider_url), timeout=20)


async def run() -> None:
    """Serve the Flow and poll the invoiceChanged binding until the process stops."""
    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO").upper())
    project = await load_from_environment()
    http_client = _local_http_client()
    server_address = os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")
    cache_directory = os.environ.get(
        "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-quickbooks-invoice-changed-blobs")
    )
    cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
    connection = new_project_connection(project, CONNECTION_NAME, http_client)
    try:
        flow = InvoiceChangedFlow(connection)
        registry = dex.Registry([flow], allow_async_handlers=True)
        worker = dex.AsyncWorker(registry, cache, dex.WorkerOptions(server_address=server_address))
        client_options = dex.ClientOptions(server_address=server_address)
        async with dex.AsyncClient(registry, cache, client_options) as client:
            runner = new_invoice_change_trigger_runner(project, connection, client, flow)
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
