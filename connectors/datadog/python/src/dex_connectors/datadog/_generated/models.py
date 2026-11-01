# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class QueryMetricsInput:
    """Typed QueryMetricsInput value from the Go connector contract."""

    from_: int = _dataclass_field(default=0, metadata={'dex_json_name': 'from'})
    to: int = 0
    query: str = ''


setattr(QueryMetricsInput, '__module__', 'dex_connectors.datadog.models')


@dataclass(frozen=True, slots=True)
class MetricSeries:
    """Typed MetricSeries value from the Go connector contract."""

    metric: str = ''
    pointlist: tuple[tuple[float | None, ...] | None, ...] | None = None


setattr(MetricSeries, '__module__', 'dex_connectors.datadog.models')


@dataclass(frozen=True, slots=True)
class MetricsQuery:
    """Typed MetricsQuery value from the Go connector contract."""

    status: str = ''
    series: tuple[MetricSeries, ...] | None = None


setattr(MetricsQuery, '__module__', 'dex_connectors.datadog.models')


@dataclass(frozen=True, slots=True)
class ListMonitorsInput:
    """Typed ListMonitorsInput value from the Go connector contract."""

    page: int = 0
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})


setattr(ListMonitorsInput, '__module__', 'dex_connectors.datadog.models')


@dataclass(frozen=True, slots=True)
class Monitor:
    """Typed Monitor value from the Go connector contract."""

    id: int = 0
    name: str = ''
    type: str = ''
    overall_state: str = ''


setattr(Monitor, '__module__', 'dex_connectors.datadog.models')


@dataclass(frozen=True, slots=True)
class MonitorsPage:
    """Typed MonitorsPage value from the Go connector contract."""

    monitors: tuple[Monitor, ...] | None = None


setattr(MonitorsPage, '__module__', 'dex_connectors.datadog.models')
