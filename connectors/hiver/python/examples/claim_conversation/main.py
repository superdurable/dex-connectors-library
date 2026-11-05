# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Load the Hiver project connection and start the claim Worker."""

import asyncio

from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.hiver import new_project_connection

from .flow import CONNECTION_NAME, ClaimConversationFlow
from .worker import run_worker


async def main() -> None:
    """Bind the static Hiver connection from Dex project configuration."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        await run_worker(ClaimConversationFlow(connection))
    finally:
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
