# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Mixpanel event imports and raw exports."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class TrackEventInput:
    """Event name, distinct ID, Unix time in seconds, and optional string properties."""

    event: str = ""
    distinct_id: str = field(default="", metadata={"dex_json_name": "distinctID"})
    time: int = 0
    properties: dict[str, str] = field(default_factory=dict, metadata={"dex_json_omit_default": True})


@dataclass(frozen=True, slots=True)
class EventImportResult:
    """Whether Mixpanel imported the event."""

    imported: bool = False


@dataclass(frozen=True, slots=True)
class ExportEventsInput:
    """Inclusive YYYY-MM-DD range of at most 30 days and a limit from 1 through 100."""

    from_date: str = field(default="", metadata={"dex_json_name": "fromDate"})
    to_date: str = field(default="", metadata={"dex_json_name": "toDate"})
    limit: int = 0


@dataclass(frozen=True, slots=True)
class ExportedEvent:
    """One raw event's name, distinct ID, Unix time, and insert ID."""

    event: str = ""
    distinct_id: str = field(default="", metadata={"dex_json_name": "distinctID"})
    time: int = 0
    insert_id: str = field(default="", metadata={"dex_json_name": "insertID"})


@dataclass(frozen=True, slots=True)
class ExportedEvents:
    """Raw events in export order."""

    events: tuple[ExportedEvent, ...] = ()
