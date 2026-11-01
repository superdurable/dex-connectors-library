# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CachePurge, CachePurgeResult, ListZonesInput, PurgeCacheByURLsInput, Zone, ZonesPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_ZONES_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, PURGED, PURGE_CACHE_BY_URLS_DEFINITION

def purge_cache_by_urls_purged(value: CachePurge, failure: Failure | None = None) -> Case[CachePurge]:
    return Case.branch_result(PURGED, value, failure)

setattr(purge_cache_by_urls_purged, '__module__', "dex_connectors.cloudflare.cloudflaremock")

def purge_cache_by_urls_not_found(value: CachePurge, failure: Failure | None = None) -> Case[CachePurge]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(purge_cache_by_urls_not_found, '__module__', "dex_connectors.cloudflare.cloudflaremock")

def purge_cache_by_urls_provider_rejected(value: CachePurge, failure: Failure | None = None) -> Case[CachePurge]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(purge_cache_by_urls_provider_rejected, '__module__', "dex_connectors.cloudflare.cloudflaremock")

def purge_cache_by_urls_invalid_response(value: CachePurge, failure: Failure | None = None) -> Case[CachePurge]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(purge_cache_by_urls_invalid_response, '__module__', "dex_connectors.cloudflare.cloudflaremock")

def purge_cache_by_urls_defect(value: CachePurge, failure: Failure | None = None) -> Case[CachePurge]:
    return Case.branch_result(DEFECT, value, failure)

setattr(purge_cache_by_urls_defect, '__module__', "dex_connectors.cloudflare.cloudflaremock")

def purge_cache_by_urls_retry(failure: Failure, after_seconds: float = 0) -> Case[CachePurge]:
    return Case.retry(failure, after_seconds)

setattr(purge_cache_by_urls_retry, '__module__', "dex_connectors.cloudflare.cloudflaremock")

def purge_cache_by_urls_purged_default() -> Case[CachePurge]:
    return Case.branch_result(PURGED, CachePurge(success=True, result=CachePurgeResult(id='purge-meridian')))

setattr(purge_cache_by_urls_purged_default, '__module__', "dex_connectors.cloudflare.cloudflaremock")

def list_zones_listed(value: ZonesPage, failure: Failure | None = None) -> Case[ZonesPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_zones_listed, '__module__', "dex_connectors.cloudflare.cloudflaremock")

def list_zones_provider_rejected(value: ZonesPage, failure: Failure | None = None) -> Case[ZonesPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_zones_provider_rejected, '__module__', "dex_connectors.cloudflare.cloudflaremock")

def list_zones_invalid_response(value: ZonesPage, failure: Failure | None = None) -> Case[ZonesPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_zones_invalid_response, '__module__', "dex_connectors.cloudflare.cloudflaremock")

def list_zones_defect(value: ZonesPage, failure: Failure | None = None) -> Case[ZonesPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_zones_defect, '__module__', "dex_connectors.cloudflare.cloudflaremock")

def list_zones_retry(failure: Failure, after_seconds: float = 0) -> Case[ZonesPage]:
    return Case.retry(failure, after_seconds)

setattr(list_zones_retry, '__module__', "dex_connectors.cloudflare.cloudflaremock")

def list_zones_listed_default() -> Case[ZonesPage]:
    return Case.branch_result(LISTED, ZonesPage(success=True, result=(Zone(id='zone-meridian', name='meridian.example', status='active'),)))

setattr(list_zones_listed_default, '__module__', "dex_connectors.cloudflare.cloudflaremock")


class Mock:

    def __init__(self, connection_name: str = "cloudflare") -> None:
        self.purge_cache_by_urls_script: Mutation[PurgeCacheByURLsInput, CachePurge] = Mutation(PURGE_CACHE_BY_URLS_DEFINITION)
        self.purge_cache_by_urls_script.default(purge_cache_by_urls_purged_default())
        self.list_zones_script: Query[ListZonesInput, ZonesPage] = Query(LIST_ZONES_DEFINITION)
        self.list_zones_script.default(list_zones_listed_default())
        self._connection = new_connection_with_operations(
            purge_cache_by_urls=self.purge_cache_by_urls_script,
            list_zones=self.list_zones_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.cloudflare.cloudflaremock")

__all__ = [
    "Mock",
    "purge_cache_by_urls_purged",
    "purge_cache_by_urls_not_found",
    "purge_cache_by_urls_provider_rejected",
    "purge_cache_by_urls_invalid_response",
    "purge_cache_by_urls_defect",
    "purge_cache_by_urls_retry",
    "purge_cache_by_urls_purged_default",
    "list_zones_listed",
    "list_zones_provider_rejected",
    "list_zones_invalid_response",
    "list_zones_defect",
    "list_zones_retry",
    "list_zones_listed_default",
]
