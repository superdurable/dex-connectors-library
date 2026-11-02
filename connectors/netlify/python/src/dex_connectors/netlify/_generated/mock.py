# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Deploy, DeploysPage, ListDeploysInput, ListSitesInput, Site, SitesPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_DEPLOYS_DEFINITION, LIST_SITES_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_sites_listed(value: SitesPage, failure: Failure | None = None) -> Case[SitesPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_sites_listed, '__module__', "dex_connectors.netlify.netlifymock")

def list_sites_provider_rejected(value: SitesPage, failure: Failure | None = None) -> Case[SitesPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_sites_provider_rejected, '__module__', "dex_connectors.netlify.netlifymock")

def list_sites_invalid_response(value: SitesPage, failure: Failure | None = None) -> Case[SitesPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_sites_invalid_response, '__module__', "dex_connectors.netlify.netlifymock")

def list_sites_defect(value: SitesPage, failure: Failure | None = None) -> Case[SitesPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_sites_defect, '__module__', "dex_connectors.netlify.netlifymock")

def list_sites_retry(failure: Failure, after_seconds: float = 0) -> Case[SitesPage]:
    return Case.retry(failure, after_seconds)

setattr(list_sites_retry, '__module__', "dex_connectors.netlify.netlifymock")

def list_sites_listed_default() -> Case[SitesPage]:
    return Case.branch_result(LISTED, SitesPage(sites=(Site(id='site-meridian', name='meridian-web', url='https://meridian.netlify.app', admin_url='https://app.netlify.com/sites/meridian-web'),)))

setattr(list_sites_listed_default, '__module__', "dex_connectors.netlify.netlifymock")

def list_deploys_listed(value: DeploysPage, failure: Failure | None = None) -> Case[DeploysPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_deploys_listed, '__module__', "dex_connectors.netlify.netlifymock")

def list_deploys_not_found(value: DeploysPage, failure: Failure | None = None) -> Case[DeploysPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_deploys_not_found, '__module__', "dex_connectors.netlify.netlifymock")

def list_deploys_provider_rejected(value: DeploysPage, failure: Failure | None = None) -> Case[DeploysPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_deploys_provider_rejected, '__module__', "dex_connectors.netlify.netlifymock")

def list_deploys_invalid_response(value: DeploysPage, failure: Failure | None = None) -> Case[DeploysPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_deploys_invalid_response, '__module__', "dex_connectors.netlify.netlifymock")

def list_deploys_defect(value: DeploysPage, failure: Failure | None = None) -> Case[DeploysPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_deploys_defect, '__module__', "dex_connectors.netlify.netlifymock")

def list_deploys_retry(failure: Failure, after_seconds: float = 0) -> Case[DeploysPage]:
    return Case.retry(failure, after_seconds)

setattr(list_deploys_retry, '__module__', "dex_connectors.netlify.netlifymock")

def list_deploys_listed_default() -> Case[DeploysPage]:
    return Case.branch_result(LISTED, DeploysPage(deploys=(Deploy(id='deploy-meridian', site_id='site-meridian', state='ready', url='https://meridian.netlify.app'),)))

setattr(list_deploys_listed_default, '__module__', "dex_connectors.netlify.netlifymock")


class Mock:

    def __init__(self, connection_name: str = "netlify") -> None:
        self.list_sites_script: Query[ListSitesInput, SitesPage] = Query(LIST_SITES_DEFINITION)
        self.list_sites_script.default(list_sites_listed_default())
        self.list_deploys_script: Query[ListDeploysInput, DeploysPage] = Query(LIST_DEPLOYS_DEFINITION)
        self.list_deploys_script.default(list_deploys_listed_default())
        self._connection = new_connection_with_operations(
            list_sites=self.list_sites_script,
            list_deploys=self.list_deploys_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.netlify.netlifymock")

__all__ = [
    "Mock",
    "list_sites_listed",
    "list_sites_provider_rejected",
    "list_sites_invalid_response",
    "list_sites_defect",
    "list_sites_retry",
    "list_sites_listed_default",
    "list_deploys_listed",
    "list_deploys_not_found",
    "list_deploys_provider_rejected",
    "list_deploys_invalid_response",
    "list_deploys_defect",
    "list_deploys_retry",
    "list_deploys_listed_default",
]
