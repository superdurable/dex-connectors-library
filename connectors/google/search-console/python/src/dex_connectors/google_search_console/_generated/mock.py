# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListSitesInput, Site, SitesPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_SITES_DEFINITION, PROVIDER_REJECTED

def list_sites_listed(value: SitesPage, failure: Failure | None = None) -> Case[SitesPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_sites_listed, '__module__', "dex_connectors.google_search_console.searchconsolemock")

def list_sites_provider_rejected(value: SitesPage, failure: Failure | None = None) -> Case[SitesPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_sites_provider_rejected, '__module__', "dex_connectors.google_search_console.searchconsolemock")

def list_sites_invalid_response(value: SitesPage, failure: Failure | None = None) -> Case[SitesPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_sites_invalid_response, '__module__', "dex_connectors.google_search_console.searchconsolemock")

def list_sites_defect(value: SitesPage, failure: Failure | None = None) -> Case[SitesPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_sites_defect, '__module__', "dex_connectors.google_search_console.searchconsolemock")

def list_sites_retry(failure: Failure, after_seconds: float = 0) -> Case[SitesPage]:
    return Case.retry(failure, after_seconds)

setattr(list_sites_retry, '__module__', "dex_connectors.google_search_console.searchconsolemock")

def list_sites_listed_default() -> Case[SitesPage]:
    return Case.branch_result(LISTED, SitesPage(sites=(Site(site_url='sc-domain:example.com', permission_level='siteOwner'),)))

setattr(list_sites_listed_default, '__module__', "dex_connectors.google_search_console.searchconsolemock")


class Mock:

    def __init__(self, connection_name: str = "google-search-console") -> None:
        self.list_sites_script: Query[ListSitesInput, SitesPage] = Query(LIST_SITES_DEFINITION)
        self.list_sites_script.default(list_sites_listed_default())
        self._connection = new_connection_with_operations(
            list_sites=self.list_sites_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_search_console.searchconsolemock")

__all__ = [
    "Mock",
    "list_sites_listed",
    "list_sites_provider_rejected",
    "list_sites_invalid_response",
    "list_sites_defect",
    "list_sites_retry",
    "list_sites_listed_default",
]
