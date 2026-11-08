# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import FormMetadata, FormsPage, GetSubmissionInput, ListFormsInput, SubmissionMetadata
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_SUBMISSION_DEFINITION, INVALID_RESPONSE, LISTED, LIST_FORMS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_forms_listed(value: FormsPage, failure: Failure | None = None) -> Case[FormsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_forms_listed, '__module__', "dex_connectors.jotform.jotformmock")

def list_forms_provider_rejected(value: FormsPage, failure: Failure | None = None) -> Case[FormsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_forms_provider_rejected, '__module__', "dex_connectors.jotform.jotformmock")

def list_forms_invalid_response(value: FormsPage, failure: Failure | None = None) -> Case[FormsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_forms_invalid_response, '__module__', "dex_connectors.jotform.jotformmock")

def list_forms_defect(value: FormsPage, failure: Failure | None = None) -> Case[FormsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_forms_defect, '__module__', "dex_connectors.jotform.jotformmock")

def list_forms_retry(failure: Failure, after_seconds: float = 0) -> Case[FormsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_forms_retry, '__module__', "dex_connectors.jotform.jotformmock")

def list_forms_listed_default() -> Case[FormsPage]:
    return Case.branch_result(LISTED, FormsPage(forms=(FormMetadata(id='241234567890123', title='Contact request', status='ENABLED'),), offset=0, limit=20))

setattr(list_forms_listed_default, '__module__', "dex_connectors.jotform.jotformmock")

def get_submission_found(value: SubmissionMetadata, failure: Failure | None = None) -> Case[SubmissionMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_submission_found, '__module__', "dex_connectors.jotform.jotformmock")

def get_submission_not_found(value: SubmissionMetadata, failure: Failure | None = None) -> Case[SubmissionMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_submission_not_found, '__module__', "dex_connectors.jotform.jotformmock")

def get_submission_provider_rejected(value: SubmissionMetadata, failure: Failure | None = None) -> Case[SubmissionMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_submission_provider_rejected, '__module__', "dex_connectors.jotform.jotformmock")

def get_submission_invalid_response(value: SubmissionMetadata, failure: Failure | None = None) -> Case[SubmissionMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_submission_invalid_response, '__module__', "dex_connectors.jotform.jotformmock")

def get_submission_defect(value: SubmissionMetadata, failure: Failure | None = None) -> Case[SubmissionMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_submission_defect, '__module__', "dex_connectors.jotform.jotformmock")

def get_submission_retry(failure: Failure, after_seconds: float = 0) -> Case[SubmissionMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_submission_retry, '__module__', "dex_connectors.jotform.jotformmock")

def get_submission_found_default() -> Case[SubmissionMetadata]:
    return Case.branch_result(FOUND, SubmissionMetadata(id='241234567890123456', form_id='241234567890123', status='ACTIVE', created_at='2026-11-07 10:00:00'))

setattr(get_submission_found_default, '__module__', "dex_connectors.jotform.jotformmock")


class Mock:

    def __init__(self, connection_name: str = "jotform") -> None:
        self.list_forms_script: Query[ListFormsInput, FormsPage] = Query(LIST_FORMS_DEFINITION)
        self.list_forms_script.default(list_forms_listed_default())
        self.get_submission_script: Query[GetSubmissionInput, SubmissionMetadata] = Query(GET_SUBMISSION_DEFINITION)
        self.get_submission_script.default(get_submission_found_default())
        self._connection = new_connection_with_operations(
            list_forms=self.list_forms_script,
            get_submission=self.get_submission_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.jotform.jotformmock")

__all__ = [
    "Mock",
    "list_forms_listed",
    "list_forms_provider_rejected",
    "list_forms_invalid_response",
    "list_forms_defect",
    "list_forms_retry",
    "list_forms_listed_default",
    "get_submission_found",
    "get_submission_not_found",
    "get_submission_provider_rejected",
    "get_submission_invalid_response",
    "get_submission_defect",
    "get_submission_retry",
    "get_submission_found_default",
]
