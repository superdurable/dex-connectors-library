# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Partner, PartnerSearchPage, SearchPartnersInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, PROVIDER_REJECTED, SEARCH_PARTNERS_DEFINITION

def search_partners_found(value: PartnerSearchPage, failure: Failure | None = None) -> Case[PartnerSearchPage]:
    return Case.branch_result(FOUND, value, failure)

setattr(search_partners_found, '__module__', "dex_connectors.odoo.odoomock")

def search_partners_provider_rejected(value: PartnerSearchPage, failure: Failure | None = None) -> Case[PartnerSearchPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_partners_provider_rejected, '__module__', "dex_connectors.odoo.odoomock")

def search_partners_invalid_response(value: PartnerSearchPage, failure: Failure | None = None) -> Case[PartnerSearchPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_partners_invalid_response, '__module__', "dex_connectors.odoo.odoomock")

def search_partners_defect(value: PartnerSearchPage, failure: Failure | None = None) -> Case[PartnerSearchPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_partners_defect, '__module__', "dex_connectors.odoo.odoomock")

def search_partners_retry(failure: Failure, after_seconds: float = 0) -> Case[PartnerSearchPage]:
    return Case.retry(failure, after_seconds)

setattr(search_partners_retry, '__module__', "dex_connectors.odoo.odoomock")

def search_partners_found_default() -> Case[PartnerSearchPage]:
    return Case.branch_result(FOUND, PartnerSearchPage(partners=(Partner(id=42, name='Acme Company'),)))

setattr(search_partners_found_default, '__module__', "dex_connectors.odoo.odoomock")


class Mock:

    def __init__(self, connection_name: str = "odoo") -> None:
        self.search_partners_script: Query[SearchPartnersInput, PartnerSearchPage] = Query(SEARCH_PARTNERS_DEFINITION)
        self.search_partners_script.default(search_partners_found_default())
        self._connection = new_connection_with_operations(
            search_partners=self.search_partners_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.odoo.odoomock")

__all__ = [
    "Mock",
    "search_partners_found",
    "search_partners_provider_rejected",
    "search_partners_invalid_response",
    "search_partners_defect",
    "search_partners_retry",
    "search_partners_found_default",
]
