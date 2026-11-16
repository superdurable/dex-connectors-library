# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Serve the Gmail thread reply Flow and poll the gmail-inbox connection until SIGINT or SIGTERM."""

import asyncio
import logging
import os
import signal

from dex_connectors.sdk import TriggerCancellation
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.google_gmail import new_project_connection

from .app import run_thread_reply_application
from .flow import CONNECTION_NAME


async def run() -> None:
    """Load the Dex Web project, open the connection, and run the application."""
    project = await load_from_environment()
    try:
        connection = new_project_connection(project, CONNECTION_NAME)
        cancellation = TriggerCancellation()
        loop = asyncio.get_running_loop()
        for stop_signal in (signal.SIGINT, signal.SIGTERM):
            loop.add_signal_handler(stop_signal, cancellation.cancel)
        try:
            await run_thread_reply_application(
                project,
                connection,
                os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801"),
                cancellation,
            )
        finally:
            await connection.aclose()
    finally:
        await project.close()


def main() -> None:
    """Run with Trigger records at LOG_LEVEL; debug also shows every ignored message and delivery."""
    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO").upper())
    asyncio.run(run())


if __name__ == "__main__":
    main()
