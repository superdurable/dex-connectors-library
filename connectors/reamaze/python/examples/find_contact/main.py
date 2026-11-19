# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Open the project connection and run the contact lookup Worker."""

import asyncio

from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.reamaze import new_project_connection

from .flow import CONNECTION_NAME, FindContactFlow
from .worker import run_worker


async def main() -> None:
    """Resolve current Dex Web credentials and serve the Flow."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        await run_worker(FindContactFlow(connection))
    finally:
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
