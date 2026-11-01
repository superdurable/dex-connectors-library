# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Cloudflare scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..client import (
    CONNECTOR_ID,
    LIST_ZONES_DEFINITION,
    LISTED,
    NOT_FOUND,
    PURGE_CACHE_BY_URLS_DEFINITION,
    PURGED,
)
from ..connection import Connection, new_connection_with_operations
from ..models import CachePurge, ListZonesInput, PurgeCacheByURLsInput, PurgeRequest, Zone, ZonesPage


def list_zones_listed(value: ZonesPage, failure: Failure | None = None) -> Case[ZonesPage]:
    """Script the listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_zones_default_answer() -> Case[ZonesPage]:
    """Return the manifest's listZones default answer."""
    return list_zones_listed(ZonesPage(True, (Zone("zone-meridian", "meridian.example", "active"),)))


def purge_cache_by_urls_purged(value: CachePurge, failure: Failure | None = None) -> Case[CachePurge]:
    """Script the purged route."""
    return Case.branch_result(PURGED, value, failure)


def purge_cache_by_urls_not_found(value: CachePurge, failure: Failure | None = None) -> Case[CachePurge]:
    """Script a missing or inaccessible zone."""
    return Case.branch_result(NOT_FOUND, value, failure)


def purge_cache_by_urls_default_answer() -> Case[CachePurge]:
    """Return the manifest's purgeCacheByURLs default answer."""
    return purge_cache_by_urls_purged(CachePurge(True, PurgeRequest("purge-meridian")))


class Mock:
    """Script Cloudflare operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "cloudflare") -> None:
        self.list_zones_script: Query[ListZonesInput, ZonesPage] = Query(LIST_ZONES_DEFINITION)
        self.list_zones_script.default(list_zones_default_answer())
        self.purge_cache_by_urls_script: Mutation[PurgeCacheByURLsInput, CachePurge] = Mutation(
            PURGE_CACHE_BY_URLS_DEFINITION
        )
        self.purge_cache_by_urls_script.default(purge_cache_by_urls_default_answer())
        self._connection = new_connection_with_operations(
            self.purge_cache_by_urls_script,
            self.list_zones_script,
            ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "list_zones_listed",
    "list_zones_default_answer",
    "purge_cache_by_urls_purged",
    "purge_cache_by_urls_not_found",
    "purge_cache_by_urls_default_answer",
]
