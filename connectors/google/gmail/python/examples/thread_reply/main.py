# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Gmail read-and-reply example against Dex and one named connection."""

import asyncio
import os

from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.google_gmail import Connection, new_project_connection

from .flow import ThreadReplyFlow
from .models import CONNECTION_NAME
from .worker import run_thread_reply_worker


async def main() -> None:
    """Load Dex Web connection material and serve one Flow on a pull Worker."""
    project = await load_from_environment()
    connection: Connection | None = None
    try:
        connection = new_project_connection(project, CONNECTION_NAME)
        await run_thread_reply_worker(
            ThreadReplyFlow(connection),
            os.environ.get("DEX_BLOB_CACHE_DIRECTORY", "/tmp/dex-gmail-thread-reply-cache"),
            os.environ.get("DEX_SERVER_ADDRESS", "localhost:8801"),
        )
    finally:
        if connection is not None:
            await connection.close()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
