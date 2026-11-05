# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start the Google Drive text copy Worker."""

import asyncio

from .worker import run_worker


def main() -> None:
    """Serve the registered Dex Flow."""
    asyncio.run(run_worker())


if __name__ == "__main__":
    main()
