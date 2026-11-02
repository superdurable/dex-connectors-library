# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetLeadInput, Lead, LeadPage, ListLeadsInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_LEAD_DEFINITION, INVALID_RESPONSE, LISTED, LIST_LEADS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_leads_listed(value: LeadPage, failure: Failure | None = None) -> Case[LeadPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_leads_listed, '__module__', "dex_connectors.close.closemock")

def list_leads_provider_rejected(value: LeadPage, failure: Failure | None = None) -> Case[LeadPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_leads_provider_rejected, '__module__', "dex_connectors.close.closemock")

def list_leads_invalid_response(value: LeadPage, failure: Failure | None = None) -> Case[LeadPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_leads_invalid_response, '__module__', "dex_connectors.close.closemock")

def list_leads_defect(value: LeadPage, failure: Failure | None = None) -> Case[LeadPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_leads_defect, '__module__', "dex_connectors.close.closemock")

def list_leads_retry(failure: Failure, after_seconds: float = 0) -> Case[LeadPage]:
    return Case.retry(failure, after_seconds)

setattr(list_leads_retry, '__module__', "dex_connectors.close.closemock")

def list_leads_default_answer() -> Case[LeadPage]:
    return Case.branch_result(LISTED, LeadPage(leads=(Lead(id='lead_meridian1', name='Meridian Labs'),), has_more=False))

setattr(list_leads_default_answer, '__module__', "dex_connectors.close.closemock")

def list_leads_invalid_key() -> Case[LeadPage]:
    return Case.branch_result(PROVIDER_REJECTED, LeadPage(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listLeads", "Close rejected the token."))

setattr(list_leads_invalid_key, '__module__', "dex_connectors.close.closemock")

def get_lead_found(value: Lead, failure: Failure | None = None) -> Case[Lead]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_lead_found, '__module__', "dex_connectors.close.closemock")

def get_lead_not_found(value: Lead, failure: Failure | None = None) -> Case[Lead]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_lead_not_found, '__module__', "dex_connectors.close.closemock")

def get_lead_provider_rejected(value: Lead, failure: Failure | None = None) -> Case[Lead]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_lead_provider_rejected, '__module__', "dex_connectors.close.closemock")

def get_lead_invalid_response(value: Lead, failure: Failure | None = None) -> Case[Lead]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_lead_invalid_response, '__module__', "dex_connectors.close.closemock")

def get_lead_defect(value: Lead, failure: Failure | None = None) -> Case[Lead]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_lead_defect, '__module__', "dex_connectors.close.closemock")

def get_lead_retry(failure: Failure, after_seconds: float = 0) -> Case[Lead]:
    return Case.retry(failure, after_seconds)

setattr(get_lead_retry, '__module__', "dex_connectors.close.closemock")

def get_lead_default_answer() -> Case[Lead]:
    return Case.branch_result(FOUND, Lead(id='lead_meridian1', name='Meridian Labs'))

setattr(get_lead_default_answer, '__module__', "dex_connectors.close.closemock")

def get_lead_invalid_key() -> Case[Lead]:
    return Case.branch_result(PROVIDER_REJECTED, Lead(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getLead", "Close rejected the token."))

setattr(get_lead_invalid_key, '__module__', "dex_connectors.close.closemock")


class Mock:

    def __init__(self, connection_name: str = "close") -> None:
        self.list_leads_script: Query[ListLeadsInput, LeadPage] = Query(LIST_LEADS_DEFINITION)
        self.list_leads_script.default(list_leads_default_answer())
        self.get_lead_script: Query[GetLeadInput, Lead] = Query(GET_LEAD_DEFINITION)
        self.get_lead_script.default(get_lead_default_answer())
        self._connection = new_connection_with_operations(
            list_leads=self.list_leads_script,
            get_lead=self.get_lead_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.close.closemock")

__all__ = [
    "Mock",
    "list_leads_listed",
    "list_leads_provider_rejected",
    "list_leads_invalid_response",
    "list_leads_defect",
    "list_leads_retry",
    "list_leads_default_answer",
    "list_leads_invalid_key",
    "get_lead_found",
    "get_lead_not_found",
    "get_lead_provider_rejected",
    "get_lead_invalid_response",
    "get_lead_defect",
    "get_lead_retry",
    "get_lead_default_answer",
    "get_lead_invalid_key",
]
