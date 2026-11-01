# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Datadog metric queries and monitor pages."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class QueryMetricsInput:
    """Datadog metric query over [from_seconds, to_seconds) in Unix seconds."""

    from_seconds: int = field(default=0, metadata={"dex_json_name": "from"})
    to_seconds: int = field(default=0, metadata={"dex_json_name": "to"})
    query: str = ""


@dataclass(frozen=True, slots=True)
class MetricSeries:
    """Metric name and [Unix milliseconds, value] points; a missing value is None."""

    metric: str = ""
    pointlist: tuple[tuple[float | None, ...], ...] = ()


@dataclass(frozen=True, slots=True)
class MetricsQuery:
    """Query status, which is ok on success, and the returned series."""

    status: str = ""
    series: tuple[MetricSeries, ...] = ()


@dataclass(frozen=True, slots=True)
class ListMonitorsInput:
    """Zero-based page and monitors per page; zero page_size means 100, at most 1000."""

    page: int = 0
    page_size: int = field(default=0, metadata={"dex_json_name": "pageSize"})


@dataclass(frozen=True, slots=True)
class Monitor:
    """Monitor identity, name, type such as metric alert, and overall state."""

    id: int = 0
    name: str = ""
    type: str = ""
    overall_state: str = ""


@dataclass(frozen=True, slots=True)
class MonitorsPage:
    """One page of monitors."""

    monitors: tuple[Monitor, ...] = ()
