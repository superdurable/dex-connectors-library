# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetLeadInput, LeadProfile
from .definitions import CONNECTOR_ID, PROVIDER, DEFECT, FOUND, GET_LEAD_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_lead_found(value: LeadProfile, failure: Failure | None = None) -> Case[LeadProfile]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_lead_found, '__module__', "dex_connectors.adobe_marketo.adobe_marketomock")

def get_lead_not_found(value: LeadProfile, failure: Failure | None = None) -> Case[LeadProfile]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_lead_not_found, '__module__', "dex_connectors.adobe_marketo.adobe_marketomock")

def get_lead_provider_rejected(value: LeadProfile, failure: Failure | None = None) -> Case[LeadProfile]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_lead_provider_rejected, '__module__', "dex_connectors.adobe_marketo.adobe_marketomock")

def get_lead_invalid_response(value: LeadProfile, failure: Failure | None = None) -> Case[LeadProfile]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_lead_invalid_response, '__module__', "dex_connectors.adobe_marketo.adobe_marketomock")

def get_lead_defect(value: LeadProfile, failure: Failure | None = None) -> Case[LeadProfile]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_lead_defect, '__module__', "dex_connectors.adobe_marketo.adobe_marketomock")

def get_lead_retry(failure: Failure, after_seconds: float = 0) -> Case[LeadProfile]:
    return Case.retry(failure, after_seconds)

setattr(get_lead_retry, '__module__', "dex_connectors.adobe_marketo.adobe_marketomock")

def get_lead_found_default() -> Case[LeadProfile]:
    return Case.branch_result(FOUND, LeadProfile(id=318581, first_name='Sample', last_name='Lead'))

setattr(get_lead_found_default, '__module__', "dex_connectors.adobe_marketo.adobe_marketomock")


class Mock:

    def __init__(self, connection_name: str = "adobe-marketo") -> None:
        self.get_lead_script: Query[GetLeadInput, LeadProfile] = Query(GET_LEAD_DEFINITION)
        self.get_lead_script.default(get_lead_found_default())
        self._connection = new_connection_with_operations(
            get_lead=self.get_lead_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.adobe_marketo.adobe_marketomock")

__all__ = [
    "Mock",
    "get_lead_found",
    "get_lead_not_found",
    "get_lead_provider_rejected",
    "get_lead_invalid_response",
    "get_lead_defect",
    "get_lead_retry",
    "get_lead_found_default",
]
