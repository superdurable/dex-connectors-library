# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import DomainAsset, SearchDomainAssetsInput, SearchDomainAssetsOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, PROVIDER_REJECTED, SEARCH_DOMAIN_ASSETS_DEFINITION

def search_domain_assets_found(value: SearchDomainAssetsOutput, failure: Failure | None = None) -> Case[SearchDomainAssetsOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(search_domain_assets_found, '__module__', "dex_connectors.fofa.fofamock")

def search_domain_assets_provider_rejected(value: SearchDomainAssetsOutput, failure: Failure | None = None) -> Case[SearchDomainAssetsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_domain_assets_provider_rejected, '__module__', "dex_connectors.fofa.fofamock")

def search_domain_assets_invalid_response(value: SearchDomainAssetsOutput, failure: Failure | None = None) -> Case[SearchDomainAssetsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_domain_assets_invalid_response, '__module__', "dex_connectors.fofa.fofamock")

def search_domain_assets_defect(value: SearchDomainAssetsOutput, failure: Failure | None = None) -> Case[SearchDomainAssetsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_domain_assets_defect, '__module__', "dex_connectors.fofa.fofamock")

def search_domain_assets_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchDomainAssetsOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_domain_assets_retry, '__module__', "dex_connectors.fofa.fofamock")

def search_domain_assets_one_host() -> Case[SearchDomainAssetsOutput]:
    return Case.branch_result(FOUND, SearchDomainAssetsOutput(total=1, assets=(DomainAsset(host='https://www.example.com', ip_address='93.184.215.14', port=443),)))

setattr(search_domain_assets_one_host, '__module__', "dex_connectors.fofa.fofamock")

def search_domain_assets_no_hosts() -> Case[SearchDomainAssetsOutput]:
    return Case.branch_result(FOUND, SearchDomainAssetsOutput(total=0, assets=()))

setattr(search_domain_assets_no_hosts, '__module__', "dex_connectors.fofa.fofamock")


class Mock:

    def __init__(self, connection_name: str = "fofa") -> None:
        self.search_domain_assets_script: Query[SearchDomainAssetsInput, SearchDomainAssetsOutput] = Query(SEARCH_DOMAIN_ASSETS_DEFINITION)
        self.search_domain_assets_script.default(search_domain_assets_one_host())
        self._connection = new_connection_with_operations(
            search_domain_assets=self.search_domain_assets_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.fofa.fofamock")

__all__ = [
    "Mock",
    "search_domain_assets_found",
    "search_domain_assets_provider_rejected",
    "search_domain_assets_invalid_response",
    "search_domain_assets_defect",
    "search_domain_assets_retry",
    "search_domain_assets_one_host",
    "search_domain_assets_no_hosts",
]
