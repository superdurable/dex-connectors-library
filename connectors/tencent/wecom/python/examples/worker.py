# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Worker entrypoint for both WeCom message Flows."""

import asyncio
import os
import tempfile
from pathlib import Path

import dex
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.tencent_wecom import new_project_connection

from .application.flow import WeComApplicationFlow
from .group_robot.flow import WeComGroupRobotFlow


async def run_worker() -> None:
    """Load the named Dex Web connections and register both message Flows."""
    project = await load_from_environment()
    robot_connection = None
    application_connection = None
    cache = None
    try:
        robot_connection = new_project_connection(project, "wecom-robot")
        application_connection = new_project_connection(project, "wecom-application")
        registry = dex.Registry(
            [WeComGroupRobotFlow(robot_connection), WeComApplicationFlow(application_connection)],
            allow_async_handlers=True,
        )
        cache_directory = os.environ.get(
            "DEX_BLOB_CACHE_DIR", str(Path(tempfile.gettempdir()) / "dex-wecom-python-blobs")
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
        if robot_connection is not None:
            await robot_connection.aclose()
        if application_connection is not None:
            await application_connection.aclose()
        if cache is not None:
            cache.close()
        await project.close()


def main() -> None:
    """Run until the Worker stops or the process receives an interrupt."""
    asyncio.run(run_worker())


if __name__ == "__main__":
    main()
