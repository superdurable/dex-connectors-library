# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Space incremental export requests inside Zendesk's per-minute limit."""

import asyncio
import time
from datetime import timedelta

from dex_connectors.sdk import TriggerContext


class RequestSpacingLimiter:
    """Start at most one request per spacing, in arrival order, so a burst of polls avoids a 429.

    One limiter is shared by every ticketChanged binding of a client and is safe for concurrent tasks on
    one event loop.
    """

    def __init__(self, spacing: timedelta) -> None:
        """Space request starts by spacing."""
        self._spacing_seconds = spacing.total_seconds()
        self._next_start = 0.0

    async def wait_for_request_start(self, context: TriggerContext) -> None:
        """Wait until the caller's reserved start time.

        Raises TriggerDeliveryAbortedError once context is cancelled; a cancelled wait keeps its
        reservation, which only delays the next request.
        """
        context.cancellation.raise_if_cancelled()
        now = time.monotonic()
        start = max(now, self._next_start)
        self._next_start = start + self._spacing_seconds
        delay = start - now
        if delay <= 0:
            return
        sleeping = asyncio.ensure_future(asyncio.sleep(delay))
        cancelled = asyncio.ensure_future(context.cancellation.wait())
        try:
            await asyncio.wait({sleeping, cancelled}, return_when=asyncio.FIRST_COMPLETED)
        finally:
            sleeping.cancel()
            cancelled.cancel()
        context.cancellation.raise_if_cancelled()
