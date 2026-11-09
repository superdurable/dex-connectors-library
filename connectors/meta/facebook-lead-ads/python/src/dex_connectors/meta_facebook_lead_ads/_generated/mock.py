# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetLeadInput, LeadFormMetadata, LeadFormsPage, LeadMetadata, ListLeadFormsInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_LEAD_DEFINITION, INVALID_RESPONSE, LISTED, LIST_LEAD_FORMS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_lead_forms_listed(value: LeadFormsPage, failure: Failure | None = None) -> Case[LeadFormsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_lead_forms_listed, '__module__', "dex_connectors.meta_facebook_lead_ads.facebookleadadsmock")

def list_lead_forms_not_found(value: LeadFormsPage, failure: Failure | None = None) -> Case[LeadFormsPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_lead_forms_not_found, '__module__', "dex_connectors.meta_facebook_lead_ads.facebookleadadsmock")

def list_lead_forms_provider_rejected(value: LeadFormsPage, failure: Failure | None = None) -> Case[LeadFormsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_lead_forms_provider_rejected, '__module__', "dex_connectors.meta_facebook_lead_ads.facebookleadadsmock")

def list_lead_forms_invalid_response(value: LeadFormsPage, failure: Failure | None = None) -> Case[LeadFormsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_lead_forms_invalid_response, '__module__', "dex_connectors.meta_facebook_lead_ads.facebookleadadsmock")

def list_lead_forms_defect(value: LeadFormsPage, failure: Failure | None = None) -> Case[LeadFormsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_lead_forms_defect, '__module__', "dex_connectors.meta_facebook_lead_ads.facebookleadadsmock")

def list_lead_forms_retry(failure: Failure, after_seconds: float = 0) -> Case[LeadFormsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_lead_forms_retry, '__module__', "dex_connectors.meta_facebook_lead_ads.facebookleadadsmock")

def list_lead_forms_listed_default() -> Case[LeadFormsPage]:
    return Case.branch_result(LISTED, LeadFormsPage(forms=(LeadFormMetadata(id='123456789012345', name='Contact request', status='ACTIVE'),), next_cursor=''))

setattr(list_lead_forms_listed_default, '__module__', "dex_connectors.meta_facebook_lead_ads.facebookleadadsmock")

def get_lead_found(value: LeadMetadata, failure: Failure | None = None) -> Case[LeadMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_lead_found, '__module__', "dex_connectors.meta_facebook_lead_ads.facebookleadadsmock")

def get_lead_not_found(value: LeadMetadata, failure: Failure | None = None) -> Case[LeadMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_lead_not_found, '__module__', "dex_connectors.meta_facebook_lead_ads.facebookleadadsmock")

def get_lead_provider_rejected(value: LeadMetadata, failure: Failure | None = None) -> Case[LeadMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_lead_provider_rejected, '__module__', "dex_connectors.meta_facebook_lead_ads.facebookleadadsmock")

def get_lead_invalid_response(value: LeadMetadata, failure: Failure | None = None) -> Case[LeadMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_lead_invalid_response, '__module__', "dex_connectors.meta_facebook_lead_ads.facebookleadadsmock")

def get_lead_defect(value: LeadMetadata, failure: Failure | None = None) -> Case[LeadMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_lead_defect, '__module__', "dex_connectors.meta_facebook_lead_ads.facebookleadadsmock")

def get_lead_retry(failure: Failure, after_seconds: float = 0) -> Case[LeadMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_lead_retry, '__module__', "dex_connectors.meta_facebook_lead_ads.facebookleadadsmock")

def get_lead_found_default() -> Case[LeadMetadata]:
    return Case.branch_result(FOUND, LeadMetadata(id='987654321012345', form_id='123456789012345', ad_id='112233445566778', created_time='2026-11-08T10:00:00+0000'))

setattr(get_lead_found_default, '__module__', "dex_connectors.meta_facebook_lead_ads.facebookleadadsmock")


class Mock:

    def __init__(self, connection_name: str = "facebook-lead-ads") -> None:
        self.list_lead_forms_script: Query[ListLeadFormsInput, LeadFormsPage] = Query(LIST_LEAD_FORMS_DEFINITION)
        self.list_lead_forms_script.default(list_lead_forms_listed_default())
        self.get_lead_script: Query[GetLeadInput, LeadMetadata] = Query(GET_LEAD_DEFINITION)
        self.get_lead_script.default(get_lead_found_default())
        self._connection = new_connection_with_operations(
            list_lead_forms=self.list_lead_forms_script,
            get_lead=self.get_lead_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.meta_facebook_lead_ads.facebookleadadsmock")

__all__ = [
    "Mock",
    "list_lead_forms_listed",
    "list_lead_forms_not_found",
    "list_lead_forms_provider_rejected",
    "list_lead_forms_invalid_response",
    "list_lead_forms_defect",
    "list_lead_forms_retry",
    "list_lead_forms_listed_default",
    "get_lead_found",
    "get_lead_not_found",
    "get_lead_provider_rejected",
    "get_lead_invalid_response",
    "get_lead_defect",
    "get_lead_retry",
    "get_lead_found_default",
]
