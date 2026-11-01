# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Datadog scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import CONNECTOR_ID, LIST_MONITORS_DEFINITION, LISTED, QUERIED, QUERY_METRICS_DEFINITION
from ..connection import Connection, new_connection_with_operations
from ..models import ListMonitorsInput, MetricSeries, MetricsQuery, Monitor, MonitorsPage, QueryMetricsInput


def query_metrics_queried(value: MetricsQuery, failure: Failure | None = None) -> Case[MetricsQuery]:
    """Script the successful queried route."""
    return Case.branch_result(QUERIED, value, failure)


def query_metrics_default_answer() -> Case[MetricsQuery]:
    """Return the manifest's queryMetrics default answer."""
    return query_metrics_queried(
        MetricsQuery("ok", (MetricSeries("system.cpu.user", ((1761436800000, 0.23),)),))
    )


def list_monitors_listed(value: MonitorsPage, failure: Failure | None = None) -> Case[MonitorsPage]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_monitors_default_answer() -> Case[MonitorsPage]:
    """Return the manifest's listMonitors default answer."""
    return list_monitors_listed(MonitorsPage((Monitor(12345, "Meridian API latency", "metric alert", "OK"),)))


class Mock:
    """Script Datadog Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "datadog") -> None:
        self.query_metrics_script: Query[QueryMetricsInput, MetricsQuery] = Query(QUERY_METRICS_DEFINITION)
        self.query_metrics_script.default(query_metrics_default_answer())
        self.list_monitors_script: Query[ListMonitorsInput, MonitorsPage] = Query(LIST_MONITORS_DEFINITION)
        self.list_monitors_script.default(list_monitors_default_answer())
        self._connection = new_connection_with_operations(
            self.query_metrics_script, self.list_monitors_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "query_metrics_queried",
    "query_metrics_default_answer",
    "list_monitors_listed",
    "list_monitors_default_answer",
]
