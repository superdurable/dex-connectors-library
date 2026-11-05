# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Worker entrypoint for the Gemini generate summary Flow."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import LoadedProject, load_from_environment

from dex_connectors.google_gemini import CONNECTOR_ID, new_project_connection

from .flow import (
    CONNECTION_NAME,
    FLOW_TYPE,
    GENERATE_SUMMARY_STEP_TYPE,
    SUMMARY_OPERATION_ID,
    GenerateSummaryFlow,
    SummaryModelConfiguration,
)


def load_summary_model_configuration(project: LoadedProject) -> SummaryModelConfiguration:
    """Read the summary Step's model pick; no saved pick uses the connection's model."""
    for entry in project.configuration.operation_configurations:
        if (
            entry.get("connectorId") == CONNECTOR_ID
            and entry.get("connectionName") == CONNECTION_NAME
            and entry.get("operationId") == SUMMARY_OPERATION_ID
            and entry.get("flowType") == FLOW_TYPE
            and entry.get("stepType") == GENERATE_SUMMARY_STEP_TYPE
        ):
            configuration = entry["configuration"]
            if set(configuration) - {"model"} or not isinstance(configuration.get("model", ""), str):
                raise ValueError("summary model configuration accepts only a model string")
            return SummaryModelConfiguration(configuration.get("model", ""))
    return SummaryModelConfiguration()


async def run_worker() -> None:
    """Load the named Dex Web connection and serve structured summaries."""
    project = await load_from_environment()
    connection = None
    cache = None
    try:
        connection = new_project_connection(project, CONNECTION_NAME)
        flow = GenerateSummaryFlow(connection, load_summary_model_configuration(project))
        registry = dex.Registry([flow], allow_async_handlers=True)
        cache_directory = os.environ.get(
            "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-gemini-generate-summary-blobs")
        )
        cache = dex.open_blob_cache(dex.BlobCacheConfig(cache_directory, 1 << 30))
        worker = dex.AsyncWorker(
            registry,
            cache,
            dex.WorkerOptions(server_address=os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801")),
        )
        async with worker:
            await worker.start()
    finally:
        if connection is not None:
            await connection.aclose()
        if cache is not None:
            cache.close()
        await project.close()


def main() -> None:
    """Run until the Worker stops or the process receives an interrupt."""
    asyncio.run(run_worker())


if __name__ == "__main__":
    main()
