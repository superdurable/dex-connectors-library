# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Case as _Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import get_global_quote_found as get_global_quote_found
from .._generated.mock import get_global_quote_found_default as _get_global_quote_found_default
from .._generated.mock import get_global_quote_not_found as get_global_quote_not_found
from .._generated.mock import get_time_series_daily_found as get_time_series_daily_found
from .._generated.mock import get_time_series_daily_found_default as _get_time_series_daily_found_default
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import FOUND as FOUND
from ..client import GET_GLOBAL_QUOTE_DEFINITION as GET_GLOBAL_QUOTE_DEFINITION
from ..client import GET_TIME_SERIES_DAILY_DEFINITION as GET_TIME_SERIES_DAILY_DEFINITION
from ..client import NOT_FOUND as NOT_FOUND
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import DailyPrice as DailyPrice
from ..models import DailySeriesOutput as DailySeriesOutput
from ..models import DailySeriesOutput as _DailySeriesOutput
from ..models import GlobalQuoteOutput as GlobalQuoteOutput
from ..models import GlobalQuoteOutput as _GlobalQuoteOutput
from ..models import StockInput as StockInput


def get_global_quote_default_answer() -> _Case[_GlobalQuoteOutput]:
    """Return the manifest's getGlobalQuote default answer."""
    return _get_global_quote_found_default()


def get_time_series_daily_default_answer() -> _Case[_DailySeriesOutput]:
    """Return the manifest's getTimeSeriesDaily default answer."""
    return _get_time_series_daily_found_default()


__all__ = [
    "Mock",
    "get_global_quote_found",
    "get_global_quote_not_found",
    "get_global_quote_default_answer",
    "get_time_series_daily_found",
    "get_time_series_daily_default_answer",
]
