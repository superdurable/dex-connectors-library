# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class TrackEventInput:
    """Typed TrackEventInput value from the Go connector contract."""

    event: str = ''
    distinct_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'distinctID'})
    time: int = 0
    properties: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})


setattr(TrackEventInput, '__module__', 'dex_connectors.mixpanel.models')


@dataclass(frozen=True, slots=True)
class EventImportResult:
    """Typed EventImportResult value from the Go connector contract."""

    imported: bool = False


setattr(EventImportResult, '__module__', 'dex_connectors.mixpanel.models')


@dataclass(frozen=True, slots=True)
class ExportEventsInput:
    """Typed ExportEventsInput value from the Go connector contract."""

    from_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'fromDate'})
    to_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'toDate'})
    limit: int = 0


setattr(ExportEventsInput, '__module__', 'dex_connectors.mixpanel.models')


@dataclass(frozen=True, slots=True)
class ExportedEvent:
    """Typed ExportedEvent value from the Go connector contract."""

    event: str = ''
    distinct_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'distinctID'})
    time: int = 0
    insert_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'insertID'})


setattr(ExportedEvent, '__module__', 'dex_connectors.mixpanel.models')


@dataclass(frozen=True, slots=True)
class ExportedEvents:
    """Typed ExportedEvents value from the Go connector contract."""

    events: tuple[ExportedEvent, ...] | None = None


setattr(ExportedEvents, '__module__', 'dex_connectors.mixpanel.models')
