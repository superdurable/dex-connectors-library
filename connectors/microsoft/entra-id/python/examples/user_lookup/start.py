# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start the Microsoft Entra ID user lookup Worker."""

import asyncio

from .worker import run_worker

if __name__ == "__main__":
    asyncio.run(run_worker())
