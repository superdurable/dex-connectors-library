# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bind Dex Web's static email connection and run the Worker."""

import asyncio

from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.superdurable_email import new_project_connection

from .flow import CONNECTION_NAME, MailboxSearchFlow
from .worker import run_worker


async def main() -> None:
    """Load the connection and serve the mailbox search Flow."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        await run_worker(MailboxSearchFlow(connection))
    finally:
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
