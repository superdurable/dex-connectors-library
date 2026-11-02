# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Datadog metric queries and monitor pages."""

from dataclasses import dataclass, field

from ._generated.models import ListMonitorsInput as ListMonitorsInput
from ._generated.models import MetricSeries as MetricSeries
from ._generated.models import MetricsQuery as MetricsQuery
from ._generated.models import Monitor as Monitor
from ._generated.models import MonitorsPage as MonitorsPage


@dataclass(frozen=True, slots=True)
class QueryMetricsInput:
    """Datadog metric query over [from_seconds, to_seconds) in Unix seconds."""

    from_seconds: int = field(default=0, metadata={"dex_json_name": "from"})
    to_seconds: int = field(default=0, metadata={"dex_json_name": "to"})
    query: str = ""


MetricSeries.__doc__ = "Metric name and [Unix milliseconds, value] points; a missing value is None."
MetricsQuery.__doc__ = "Query status, which is ok on success, and the returned series."
ListMonitorsInput.__doc__ = "Zero-based page and monitors per page; zero page_size means 100, at most 1000."
Monitor.__doc__ = "Monitor identity, name, type such as metric alert, and overall state."
MonitorsPage.__doc__ = "One page of monitors."
