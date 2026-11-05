# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Snowflake Python Worker from a Dex Web connection."""

import asyncio

from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.snowflake import new_project_connection

from .flow import CONNECTION_NAME
from .worker import run_worker


async def main() -> None:
    """Load the declared connection and serve both Flows."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        await run_worker(connection)
    finally:
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
