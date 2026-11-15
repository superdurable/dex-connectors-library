# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The cursor persistence contract and an in-process cursor store."""

from typing import Protocol

from ..projectconfig.models import Scope
from ..projectconfig.poll_cursor import PollCursorRecord, PollCursorState, PollCursorStore, PollFeedKey
from ..projectconfig.storage import InMemoryObjectStore


class CursorStore(Protocol):
    """Persists one poller's cursor with compare-and-swap.

    `projectconfig.PollCursorStore`, from `LoadedProject.poll_cursor_store` or
    `LoadedProject.poll_feed_cursor_store`, implements it.
    """

    async def read_poll_cursor(self) -> PollCursorRecord:
        """Return the current record; its state is None until the cursor is seeded."""
        ...

    async def replace_poll_cursor(self, expected_revision: str, state: PollCursorState) -> PollCursorRecord:
        """Write state only while the head is at expected_revision; raise ObjectConflictError otherwise."""
        ...


def new_memory_cursor_store() -> CursorStore:
    """Keep one cursor in process memory with the encoding, bounds, and compare-and-swap of project storage.

    Use it in tests and in applications without project storage. A restart reseeds, so changes made
    while the process was stopped are not delivered.
    """
    return PollCursorStore(
        InMemoryObjectStore(),
        Scope("memory", "live"),
        PollFeedKey("memory", "memory", "memoryCursor"),
    )
