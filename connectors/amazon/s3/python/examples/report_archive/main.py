# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Run the S3 report archive Worker with its Dex Web connection."""

import asyncio

from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.amazon_s3 import new_project_connection

from .flow import CONNECTION_NAME, ReportArchiveFlow
from .worker import run_worker


async def main() -> None:
    """Load the project connection and serve the report archive Flow."""
    project = await load_from_environment()
    connection = new_project_connection(project, CONNECTION_NAME)
    try:
        await run_worker(ReportArchiveFlow(connection))
    finally:
        await connection.aclose()
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
