# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class GetStatsInput:
    """Typed GetStatsInput value from the Go connector contract."""

    start_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'startDate'})
    end_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'endDate', 'dex_json_omit_default': True})


setattr(GetStatsInput, '__module__', 'dex_connectors.sendgrid.models')


@dataclass(frozen=True, slots=True)
class StatsDay:
    """Typed StatsDay value from the Go connector contract."""

    date: str = ''
    requests: int = 0
    delivered: int = 0


setattr(StatsDay, '__module__', 'dex_connectors.sendgrid.models')


@dataclass(frozen=True, slots=True)
class StatsOutput:
    """Typed StatsOutput value from the Go connector contract."""

    days: tuple[StatsDay, ...] | None = None


setattr(StatsOutput, '__module__', 'dex_connectors.sendgrid.models')


@dataclass(frozen=True, slots=True)
class ListTemplatesInput:
    """Typed ListTemplatesInput value from the Go connector contract."""

    generation: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})


setattr(ListTemplatesInput, '__module__', 'dex_connectors.sendgrid.models')


@dataclass(frozen=True, slots=True)
class Template:
    """Typed Template value from the Go connector contract."""

    id: str = ''
    name: str = ''
    generation: str = ''


setattr(Template, '__module__', 'dex_connectors.sendgrid.models')


@dataclass(frozen=True, slots=True)
class TemplatesOutput:
    """Typed TemplatesOutput value from the Go connector contract."""

    templates: tuple[Template, ...] | None = None
    count: int = 0
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})


setattr(TemplatesOutput, '__module__', 'dex_connectors.sendgrid.models')
