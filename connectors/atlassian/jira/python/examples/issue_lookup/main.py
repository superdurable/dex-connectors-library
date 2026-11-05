# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Load the Jira project connection and run the issue lookup Worker."""

import asyncio

from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.atlassian_jira import new_project_connection

from .flow import CONNECTION_NAME, IssueLookupFlow
from .worker import run_worker


async def main() -> None:
    """Serve the Flow with project-managed OAuth credentials."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        await run_worker(IssueLookupFlow(connection))
    finally:
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
