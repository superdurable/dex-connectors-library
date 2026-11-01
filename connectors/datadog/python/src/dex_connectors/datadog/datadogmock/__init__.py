# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Case as _Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import list_monitors_listed as list_monitors_listed
from .._generated.mock import list_monitors_listed_default as _list_monitors_listed_default
from .._generated.mock import query_metrics_queried as query_metrics_queried
from .._generated.mock import query_metrics_queried_default as _query_metrics_queried_default
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import LIST_MONITORS_DEFINITION as LIST_MONITORS_DEFINITION
from ..client import LISTED as LISTED
from ..client import QUERIED as QUERIED
from ..client import QUERY_METRICS_DEFINITION as QUERY_METRICS_DEFINITION
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import ListMonitorsInput as ListMonitorsInput
from ..models import MetricSeries as MetricSeries
from ..models import MetricsQuery as MetricsQuery
from ..models import MetricsQuery as _MetricsQuery
from ..models import Monitor as Monitor
from ..models import MonitorsPage as MonitorsPage
from ..models import MonitorsPage as _MonitorsPage
from ..models import QueryMetricsInput as QueryMetricsInput


def list_monitors_default_answer() -> _Case[_MonitorsPage]:
    """Return the manifest's listMonitors default answer."""
    return _list_monitors_listed_default()


def query_metrics_default_answer() -> _Case[_MetricsQuery]:
    """Return the manifest's queryMetrics default answer."""
    return _query_metrics_queried_default()


__all__ = [
    "Mock",
    "query_metrics_queried",
    "query_metrics_default_answer",
    "list_monitors_listed",
    "list_monitors_default_answer",
]
