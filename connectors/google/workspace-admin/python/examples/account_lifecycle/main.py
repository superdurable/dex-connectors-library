# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Load the Google Workspace Admin project connection and start its Worker."""

import asyncio

from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.google_workspace_admin import new_project_connection

from .flow import CONNECTION_NAME, AccountListFlow
from .worker import run_worker


async def main() -> None:
    """Serve the account-list Flow with the configured Dex connection."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        await run_worker(AccountListFlow(connection))
    finally:
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
