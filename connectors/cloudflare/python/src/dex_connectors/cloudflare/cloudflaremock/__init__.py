# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Case as _Case
from dex_connectors.sdk.connectormock import Mutation as Mutation
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import list_zones_listed as list_zones_listed
from .._generated.mock import list_zones_listed_default as _list_zones_listed_default
from .._generated.mock import purge_cache_by_urls_not_found as purge_cache_by_urls_not_found
from .._generated.mock import purge_cache_by_urls_purged as purge_cache_by_urls_purged
from .._generated.mock import purge_cache_by_urls_purged_default as _purge_cache_by_urls_purged_default
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import LIST_ZONES_DEFINITION as LIST_ZONES_DEFINITION
from ..client import LISTED as LISTED
from ..client import NOT_FOUND as NOT_FOUND
from ..client import PURGE_CACHE_BY_URLS_DEFINITION as PURGE_CACHE_BY_URLS_DEFINITION
from ..client import PURGED as PURGED
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import CachePurge as CachePurge
from ..models import CachePurge as _CachePurge
from ..models import ListZonesInput as ListZonesInput
from ..models import PurgeCacheByURLsInput as PurgeCacheByURLsInput
from ..models import PurgeRequest as PurgeRequest
from ..models import Zone as Zone
from ..models import ZonesPage as ZonesPage
from ..models import ZonesPage as _ZonesPage


def list_zones_default_answer() -> _Case[_ZonesPage]:
    """Return the manifest's listZones default answer."""
    return _list_zones_listed_default()


def purge_cache_by_urls_default_answer() -> _Case[_CachePurge]:
    """Return the manifest's purgeCacheByURLs default answer."""
    return _purge_cache_by_urls_purged_default()


__all__ = [
    "Mock",
    "list_zones_listed",
    "list_zones_default_answer",
    "purge_cache_by_urls_purged",
    "purge_cache_by_urls_not_found",
    "purge_cache_by_urls_default_answer",
]

Mock.__doc__ = "Script Cloudflare operations, defaulting to their manifest answers."

list_zones_listed.__doc__ = "Script the listed route."
purge_cache_by_urls_purged.__doc__ = "Script the purged route."
purge_cache_by_urls_not_found.__doc__ = "Script a missing or inaccessible zone."
