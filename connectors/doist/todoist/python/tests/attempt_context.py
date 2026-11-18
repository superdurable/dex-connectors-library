# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A Dex attempt context that keeps the last heartbeat across Execute attempts."""

from datetime import UTC, datetime, timedelta

from dex_connectors.sdk.singledispatch import Marker


class AttemptContext:
    """Keep the last heartbeat across Execute attempts like Dex does."""

    def __init__(
        self, attempt: int = 1, heartbeat_value: object = None, deadline: datetime | None = None
    ) -> None:
        self.attempt = attempt
        self.run_id = "run"
        self.deadline = deadline or datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_value = heartbeat_value
        self.heartbeat_read_error: Exception | None = None

    @property
    def marker(self) -> Marker | None:
        """Decode the single call's checkpoint from either SDK heartbeat format."""
        if isinstance(self.heartbeat_value, Marker) or self.heartbeat_value is None:
            return self.heartbeat_value
        assert isinstance(self.heartbeat_value, dict)
        entries = list(self.heartbeat_value["singleDispatchMarkers"].values())
        assert len(entries) == 1, "each attempt makes one call"
        return Marker.from_wire(entries[0])

    def has_last_heartbeat_value(self) -> bool:
        """Report whether an earlier attempt recorded a heartbeat."""
        return self.heartbeat_value is not None or self.heartbeat_read_error is not None

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Return the last heartbeat as the reading SDK expects it."""
        if self.heartbeat_read_error is not None:
            raise self.heartbeat_read_error
        seeded = self.heartbeat_value
        if isinstance(seeded, Marker) and value_type is not Marker:
            return {"singleDispatchMarkers": {str(seeded.call_id): seeded.to_wire()}}
        if isinstance(seeded, dict) and value_type is Marker:
            return self.marker
        return seeded

    async def heartbeat(self, value: object = None) -> None:
        """Record the heartbeat exactly as the SDK wrote it."""
        self.heartbeat_value = value
