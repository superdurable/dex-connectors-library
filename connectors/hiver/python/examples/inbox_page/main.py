# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bind the project Hiver connection and start its Worker."""

import asyncio

from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.hiver import new_project_connection

from .flow import CONNECTION_NAME, InboxPageFlow
from .worker import run_worker


async def main() -> None:
    """Load the static Hiver connection and run the inbox page Flow."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        await run_worker(InboxPageFlow(connection))
    finally:
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
