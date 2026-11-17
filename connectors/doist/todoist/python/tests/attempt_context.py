# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A Dex attempt context that keeps the last heartbeat across Execute attempts."""

from datetime import UTC, datetime, timedelta

from dex_connectors.sdk.singledispatch import Marker


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(
        self, attempt: int = 1, marker: Marker | None = None, deadline: datetime | None = None
    ) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.deadline = deadline or datetime.now(UTC) + timedelta(seconds=30)
        self.marker = marker

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.marker is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last marker."""
        return self.marker

    async def heartbeat(self, value: object = None) -> None:
        """Record the attempt's marker."""
        self.marker = value if isinstance(value, Marker) else None
