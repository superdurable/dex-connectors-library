# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the Stripe card Checkout example Worker."""

import asyncio

from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.stripe import new_project_connection

from .flow import CONNECTION_NAME, CardCheckoutFlow
from .worker import run_worker


async def main() -> None:
    """Load the Dex project, bind Stripe, and serve until shutdown."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        await run_worker(CardCheckoutFlow(connection))
    finally:
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
