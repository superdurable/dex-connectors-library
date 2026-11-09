# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetLeadInput, LeadMetadata, LeadStatusMetadata, LeadStatusesList, ListLeadStatusesInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_LEAD_DEFINITION, INVALID_RESPONSE, LISTED, LIST_LEAD_STATUSES_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_lead_statuses_listed(value: LeadStatusesList, failure: Failure | None = None) -> Case[LeadStatusesList]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_lead_statuses_listed, '__module__', "dex_connectors.copper.coppermock")

def list_lead_statuses_provider_rejected(value: LeadStatusesList, failure: Failure | None = None) -> Case[LeadStatusesList]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_lead_statuses_provider_rejected, '__module__', "dex_connectors.copper.coppermock")

def list_lead_statuses_invalid_response(value: LeadStatusesList, failure: Failure | None = None) -> Case[LeadStatusesList]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_lead_statuses_invalid_response, '__module__', "dex_connectors.copper.coppermock")

def list_lead_statuses_defect(value: LeadStatusesList, failure: Failure | None = None) -> Case[LeadStatusesList]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_lead_statuses_defect, '__module__', "dex_connectors.copper.coppermock")

def list_lead_statuses_retry(failure: Failure, after_seconds: float = 0) -> Case[LeadStatusesList]:
    return Case.retry(failure, after_seconds)

setattr(list_lead_statuses_retry, '__module__', "dex_connectors.copper.coppermock")

def list_lead_statuses_listed_default() -> Case[LeadStatusesList]:
    return Case.branch_result(LISTED, LeadStatusesList(statuses=(LeadStatusMetadata(id=208231, name='New'),)))

setattr(list_lead_statuses_listed_default, '__module__', "dex_connectors.copper.coppermock")

def get_lead_found(value: LeadMetadata, failure: Failure | None = None) -> Case[LeadMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_lead_found, '__module__', "dex_connectors.copper.coppermock")

def get_lead_not_found(value: LeadMetadata, failure: Failure | None = None) -> Case[LeadMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_lead_not_found, '__module__', "dex_connectors.copper.coppermock")

def get_lead_provider_rejected(value: LeadMetadata, failure: Failure | None = None) -> Case[LeadMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_lead_provider_rejected, '__module__', "dex_connectors.copper.coppermock")

def get_lead_invalid_response(value: LeadMetadata, failure: Failure | None = None) -> Case[LeadMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_lead_invalid_response, '__module__', "dex_connectors.copper.coppermock")

def get_lead_defect(value: LeadMetadata, failure: Failure | None = None) -> Case[LeadMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_lead_defect, '__module__', "dex_connectors.copper.coppermock")

def get_lead_retry(failure: Failure, after_seconds: float = 0) -> Case[LeadMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_lead_retry, '__module__', "dex_connectors.copper.coppermock")

def get_lead_found_default() -> Case[LeadMetadata]:
    return Case.branch_result(FOUND, LeadMetadata(id=8894157, name='Sample Lead', status='New', company_name='Example Company'))

setattr(get_lead_found_default, '__module__', "dex_connectors.copper.coppermock")


class Mock:

    def __init__(self, connection_name: str = "copper") -> None:
        self.list_lead_statuses_script: Query[ListLeadStatusesInput, LeadStatusesList] = Query(LIST_LEAD_STATUSES_DEFINITION)
        self.list_lead_statuses_script.default(list_lead_statuses_listed_default())
        self.get_lead_script: Query[GetLeadInput, LeadMetadata] = Query(GET_LEAD_DEFINITION)
        self.get_lead_script.default(get_lead_found_default())
        self._connection = new_connection_with_operations(
            list_lead_statuses=self.list_lead_statuses_script,
            get_lead=self.get_lead_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.copper.coppermock")

__all__ = [
    "Mock",
    "list_lead_statuses_listed",
    "list_lead_statuses_provider_rejected",
    "list_lead_statuses_invalid_response",
    "list_lead_statuses_defect",
    "list_lead_statuses_retry",
    "list_lead_statuses_listed_default",
    "get_lead_found",
    "get_lead_not_found",
    "get_lead_provider_rejected",
    "get_lead_invalid_response",
    "get_lead_defect",
    "get_lead_retry",
    "get_lead_found_default",
]
