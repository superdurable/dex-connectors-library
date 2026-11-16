# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Worker entrypoint: poll the picked Salesforce object and start one Flow per record change."""

import asyncio
import logging
import os
import signal

from dex_connectors.sdk import TriggerCancellation
from dex_connectors.sdk.projectconfig import load_from_environment

from .watcher import run_record_change_watcher


async def main() -> None:
    """Load the project and run until SIGINT or SIGTERM."""
    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO").upper())
    cancellation = TriggerCancellation()
    loop = asyncio.get_running_loop()
    for stop_signal in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(stop_signal, cancellation.cancel)
    project = await load_from_environment()
    try:
        await run_record_change_watcher(
            project, os.environ.get("DEX_FLOW_SERVICE_ADDRESS", "localhost:8801"), cancellation
        )
    finally:
        await project.close()


if __name__ == "__main__":
    asyncio.run(main())
