# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetLeadInput, LeadMetadata, LeadsPage, ListLeadsInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_LEAD_DEFINITION, INVALID_RESPONSE, LISTED, LIST_LEADS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_leads_listed(value: LeadsPage, failure: Failure | None = None) -> Case[LeadsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_leads_listed, '__module__', "dex_connectors.zendesk_sell.sellmock")

def list_leads_provider_rejected(value: LeadsPage, failure: Failure | None = None) -> Case[LeadsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_leads_provider_rejected, '__module__', "dex_connectors.zendesk_sell.sellmock")

def list_leads_invalid_response(value: LeadsPage, failure: Failure | None = None) -> Case[LeadsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_leads_invalid_response, '__module__', "dex_connectors.zendesk_sell.sellmock")

def list_leads_defect(value: LeadsPage, failure: Failure | None = None) -> Case[LeadsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_leads_defect, '__module__', "dex_connectors.zendesk_sell.sellmock")

def list_leads_retry(failure: Failure, after_seconds: float = 0) -> Case[LeadsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_leads_retry, '__module__', "dex_connectors.zendesk_sell.sellmock")

def list_leads_listed_default() -> Case[LeadsPage]:
    return Case.branch_result(LISTED, LeadsPage(leads=(LeadMetadata(id=144, first_name='Ada', last_name='Lovelace', email_address='ada@example.com', organization_name='Example Co'),), page=1, per_page=20))

setattr(list_leads_listed_default, '__module__', "dex_connectors.zendesk_sell.sellmock")

def get_lead_found(value: LeadMetadata, failure: Failure | None = None) -> Case[LeadMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_lead_found, '__module__', "dex_connectors.zendesk_sell.sellmock")

def get_lead_not_found(value: LeadMetadata, failure: Failure | None = None) -> Case[LeadMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_lead_not_found, '__module__', "dex_connectors.zendesk_sell.sellmock")

def get_lead_provider_rejected(value: LeadMetadata, failure: Failure | None = None) -> Case[LeadMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_lead_provider_rejected, '__module__', "dex_connectors.zendesk_sell.sellmock")

def get_lead_invalid_response(value: LeadMetadata, failure: Failure | None = None) -> Case[LeadMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_lead_invalid_response, '__module__', "dex_connectors.zendesk_sell.sellmock")

def get_lead_defect(value: LeadMetadata, failure: Failure | None = None) -> Case[LeadMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_lead_defect, '__module__', "dex_connectors.zendesk_sell.sellmock")

def get_lead_retry(failure: Failure, after_seconds: float = 0) -> Case[LeadMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_lead_retry, '__module__', "dex_connectors.zendesk_sell.sellmock")

def get_lead_found_default() -> Case[LeadMetadata]:
    return Case.branch_result(FOUND, LeadMetadata(id=144, first_name='Ada', last_name='Lovelace', email_address='ada@example.com', organization_name='Example Co'))

setattr(get_lead_found_default, '__module__', "dex_connectors.zendesk_sell.sellmock")


class Mock:

    def __init__(self, connection_name: str = "zendesk-sell") -> None:
        self.list_leads_script: Query[ListLeadsInput, LeadsPage] = Query(LIST_LEADS_DEFINITION)
        self.list_leads_script.default(list_leads_listed_default())
        self.get_lead_script: Query[GetLeadInput, LeadMetadata] = Query(GET_LEAD_DEFINITION)
        self.get_lead_script.default(get_lead_found_default())
        self._connection = new_connection_with_operations(
            list_leads=self.list_leads_script,
            get_lead=self.get_lead_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.zendesk_sell.sellmock")

__all__ = [
    "Mock",
    "list_leads_listed",
    "list_leads_provider_rejected",
    "list_leads_invalid_response",
    "list_leads_defect",
    "list_leads_retry",
    "list_leads_listed_default",
    "get_lead_found",
    "get_lead_not_found",
    "get_lead_provider_rejected",
    "get_lead_invalid_response",
    "get_lead_defect",
    "get_lead_retry",
    "get_lead_found_default",
]
