# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# TrackEventInput identifies one stable event across Dex retries.
@dataclass(frozen=True, slots=True)
class TrackEventInput:
    # Event is the Mixpanel event name.
    event: str = ''
    # DistinctID identifies the associated user and must remain stable across retries.
    distinct_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'distinctID'})
    # Time is the event's Unix timestamp in seconds and must remain stable across retries.
    time: int = 0
    # Properties contains optional non-secret string properties.
    properties: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})


setattr(TrackEventInput, '__module__', 'dex_connectors.mixpanel.models')


# EventImportResult confirms Mixpanel accepted one event for ingestion.
@dataclass(frozen=True, slots=True)
class EventImportResult:
    # Imported means the strict import response accepted the single event.
    imported: bool = False


setattr(EventImportResult, '__module__', 'dex_connectors.mixpanel.models')


# ExportEventsInput selects a bounded raw event export.
@dataclass(frozen=True, slots=True)
class ExportEventsInput:
    # FromDate is the inclusive YYYY-MM-DD start date.
    from_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'fromDate'})
    # ToDate is the inclusive YYYY-MM-DD end date.
    to_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'toDate'})
    # Limit is the maximum number of raw events, from 1 through 100.
    limit: int = 0


setattr(ExportEventsInput, '__module__', 'dex_connectors.mixpanel.models')


# ExportedEvent contains only non-secret identity fields from one raw event.
@dataclass(frozen=True, slots=True)
class ExportedEvent:
    # Event is the event name.
    event: str = ''
    # DistinctID identifies the user.
    distinct_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'distinctID'})
    # Time is the Unix event timestamp in seconds.
    time: int = 0
    # InsertID is the provider's deduplication ID.
    insert_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'insertID'})


setattr(ExportedEvent, '__module__', 'dex_connectors.mixpanel.models')


# ExportedEvents is a bounded raw event batch; raw exports can include duplicates.
@dataclass(frozen=True, slots=True)
class ExportedEvents:
    # Events contains at most the requested limit, without arbitrary properties.
    events: tuple[ExportedEvent, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'events', _replace_nil_slices(self.events, 1))


ExportedEvents.__annotations__['events'] = tuple[ExportedEvent, ...] | None
ExportedEvents.__dataclass_fields__['events'].type = tuple[ExportedEvent, ...] | None
setattr(ExportedEvents, '__module__', 'dex_connectors.mixpanel.models')
