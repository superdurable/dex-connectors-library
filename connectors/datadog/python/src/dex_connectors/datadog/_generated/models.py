# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# QueryMetricsInput supplies the metric expression and Unix-second window.
@dataclass(frozen=True, slots=True)
class QueryMetricsInput:
    # From is the inclusive start in Unix seconds.
    from_: int = _dataclass_field(default=0, metadata={'dex_json_name': 'from'})
    # To is the exclusive end in Unix seconds and must follow From.
    to: int = 0
    # Query is a Datadog metric query expression.
    query: str = ''


setattr(QueryMetricsInput, '__module__', 'dex_connectors.datadog.models')


# MetricSeries is one Datadog timeseries.
@dataclass(frozen=True, slots=True)
class MetricSeries:
    # Metric is the queried metric name.
    metric: str = ''
    # Pointlist holds pairs of millisecond timestamps and possibly null values.
    pointlist: tuple[tuple[float | None, ...] | None, ...] | None = None


setattr(MetricSeries, '__module__', 'dex_connectors.datadog.models')


# MetricsQuery is Datadog's metric query result.
@dataclass(frozen=True, slots=True)
class MetricsQuery:
    # Status is Datadog's query status, normally ok.
    status: str = ''
    # Series contains the matching timeseries.
    series: tuple[MetricSeries, ...] | None = None


setattr(MetricsQuery, '__module__', 'dex_connectors.datadog.models')


# ListMonitorsInput selects a zero-based monitor page.
@dataclass(frozen=True, slots=True)
class ListMonitorsInput:
    # Page is zero-based; zero selects the first page.
    page: int = 0
    # PageSize is the number of monitors requested; zero selects 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})


setattr(ListMonitorsInput, '__module__', 'dex_connectors.datadog.models')


# Monitor is a Datadog monitor summary.
@dataclass(frozen=True, slots=True)
class Monitor:
    # ID is the monitor's numeric identifier.
    id: int = 0
    # Name is the monitor's display name.
    name: str = ''
    # Type is the monitor kind, such as metric alert.
    type: str = ''
    # OverallState is the provider-reported alert state.
    overall_state: str = ''


setattr(Monitor, '__module__', 'dex_connectors.datadog.models')


# MonitorsPage contains the returned monitor page.
@dataclass(frozen=True, slots=True)
class MonitorsPage:
    # Monitors contains the requested monitor page, possibly empty.
    monitors: tuple[Monitor, ...] | None = None


setattr(MonitorsPage, '__module__', 'dex_connectors.datadog.models')
