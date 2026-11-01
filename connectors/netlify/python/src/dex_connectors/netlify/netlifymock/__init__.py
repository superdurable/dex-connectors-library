# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Netlify scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import CONNECTOR_ID, LIST_DEPLOYS_DEFINITION, LIST_SITES_DEFINITION, LISTED, NOT_FOUND
from ..connection import Connection, new_connection_with_operations
from ..models import Deploy, DeploysPage, ListDeploysInput, ListSitesInput, Site, SitesPage


def list_sites_listed(value: SitesPage, failure: Failure | None = None) -> Case[SitesPage]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_sites_listed_default() -> Case[SitesPage]:
    """Return the manifest's listSites listedDefault answer."""
    site = Site(
        "site-meridian",
        "meridian-web",
        "https://meridian.netlify.app",
        "https://app.netlify.com/sites/meridian-web",
    )
    return list_sites_listed(SitesPage((site,)))


def list_deploys_listed(value: DeploysPage, failure: Failure | None = None) -> Case[DeploysPage]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_deploys_not_found(value: DeploysPage, failure: Failure | None = None) -> Case[DeploysPage]:
    """Script a missing or inaccessible site."""
    return Case.branch_result(NOT_FOUND, value, failure)


def list_deploys_listed_default() -> Case[DeploysPage]:
    """Return the manifest's listDeploys listedDefault answer."""
    deploy = Deploy("deploy-meridian", "site-meridian", "ready", "https://meridian.netlify.app")
    return list_deploys_listed(DeploysPage((deploy,)))


class Mock:
    """Script Netlify Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "netlify") -> None:
        self.list_sites_script: Query[ListSitesInput, SitesPage] = Query(LIST_SITES_DEFINITION)
        self.list_sites_script.default(list_sites_listed_default())
        self.list_deploys_script: Query[ListDeploysInput, DeploysPage] = Query(LIST_DEPLOYS_DEFINITION)
        self.list_deploys_script.default(list_deploys_listed_default())
        self._connection = new_connection_with_operations(
            self.list_sites_script, self.list_deploys_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "list_sites_listed",
    "list_sites_listed_default",
    "list_deploys_listed",
    "list_deploys_not_found",
    "list_deploys_listed_default",
]
