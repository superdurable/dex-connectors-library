# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Load the Mailchimp connection and run the example Worker."""

import asyncio

from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.intuit_mailchimp import new_project_connection

from .flow import CONNECTION_NAME, GetMemberFlow
from .worker import run_worker


async def main() -> None:
    """Bind the static connection and run the Dex Worker."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        await run_worker(GetMemberFlow(connection))
    finally:
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
