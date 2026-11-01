# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Case as _Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import get_stats_found as get_stats_found
from .._generated.mock import get_stats_found_default as _get_stats_found_default
from .._generated.mock import list_templates_listed as list_templates_listed
from .._generated.mock import list_templates_listed_default as _list_templates_listed_default
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import FOUND as FOUND
from ..client import GET_STATS_DEFINITION as GET_STATS_DEFINITION
from ..client import LIST_TEMPLATES_DEFINITION as LIST_TEMPLATES_DEFINITION
from ..client import LISTED as LISTED
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import GetStatsInput as GetStatsInput
from ..models import ListTemplatesInput as ListTemplatesInput
from ..models import StatsDay as StatsDay
from ..models import StatsOutput as StatsOutput
from ..models import StatsOutput as _StatsOutput
from ..models import Template as Template
from ..models import TemplatesOutput as TemplatesOutput
from ..models import TemplatesOutput as _TemplatesOutput


def get_stats_default_answer() -> _Case[_StatsOutput]:
    """Return the manifest's getStats default answer."""
    return _get_stats_found_default()


def list_templates_default_answer() -> _Case[_TemplatesOutput]:
    """Return the manifest's listTemplates default answer."""
    return _list_templates_listed_default()


__all__ = [
    "Mock",
    "get_stats_found",
    "get_stats_default_answer",
    "list_templates_listed",
    "list_templates_default_answer",
]
