# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class GetEventsInput:
    """Typed GetEventsInput value from the Go connector contract."""

    domain: str = ''
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(GetEventsInput, '__module__', 'dex_connectors.mailgun.models')


@dataclass(frozen=True, slots=True)
class Event:
    """Typed Event value from the Go connector contract."""

    id: str = ''
    event: str = ''
    timestamp: float = 0.0


setattr(Event, '__module__', 'dex_connectors.mailgun.models')


@dataclass(frozen=True, slots=True)
class EventsOutput:
    """Typed EventsOutput value from the Go connector contract."""

    events: tuple[Event, ...] | None = None


setattr(EventsOutput, '__module__', 'dex_connectors.mailgun.models')


@dataclass(frozen=True, slots=True)
class ListDomainsInput:
    """Typed ListDomainsInput value from the Go connector contract."""

    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    skip: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListDomainsInput, '__module__', 'dex_connectors.mailgun.models')


@dataclass(frozen=True, slots=True)
class Domain:
    """Typed Domain value from the Go connector contract."""

    name: str = ''
    state: str = ''


setattr(Domain, '__module__', 'dex_connectors.mailgun.models')


@dataclass(frozen=True, slots=True)
class DomainsOutput:
    """Typed DomainsOutput value from the Go connector contract."""

    domains: tuple[Domain, ...] | None = None
    total_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalCount'})


setattr(DomainsOutput, '__module__', 'dex_connectors.mailgun.models')
