# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Form, FormAnswer, FormAnswerFile, FormAnswerGrade, FormChoice, FormItem, FormPublishState, FormQuestion, FormResponse, FormScale, GetFormInput, GetResponseInput, ListResponsesInput, ResponsePage
from .definitions import CONNECTOR_ID, PROVIDER, DEFECT, FOUND, GET_FORM_DEFINITION, GET_RESPONSE_DEFINITION, INVALID_RESPONSE, LISTED, LIST_RESPONSES_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def get_form_found(value: Form, failure: Failure | None = None) -> Case[Form]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_form_found, '__module__', "dex_connectors.google_forms.formsmock")

def get_form_not_found(value: Form, failure: Failure | None = None) -> Case[Form]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_form_not_found, '__module__', "dex_connectors.google_forms.formsmock")

def get_form_provider_rejected(value: Form, failure: Failure | None = None) -> Case[Form]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_form_provider_rejected, '__module__', "dex_connectors.google_forms.formsmock")

def get_form_invalid_response(value: Form, failure: Failure | None = None) -> Case[Form]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_form_invalid_response, '__module__', "dex_connectors.google_forms.formsmock")

def get_form_defect(value: Form, failure: Failure | None = None) -> Case[Form]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_form_defect, '__module__', "dex_connectors.google_forms.formsmock")

def get_form_retry(failure: Failure, after_seconds: float = 0) -> Case[Form]:
    return Case.retry(failure, after_seconds)

setattr(get_form_retry, '__module__', "dex_connectors.google_forms.formsmock")

def get_form_vendor_intake_form() -> Case[Form]:
    return Case.branch_result(FOUND, Form(form_id='1FAIpQLintake', title='Vendor intake', document_title='Vendor intake (responses)', items=(FormItem(item_id='it_name', kind='question', title='Company name', questions=(FormQuestion(question_id='q_name', item_id='it_name', title='Company name', type='shortText', is_required=True),)), FormItem(item_id='it_regions', kind='question', title='Regions', questions=(FormQuestion(question_id='q_regions', item_id='it_regions', title='Regions', type='checkbox', is_required=False, choices=(FormChoice(value='EU'), FormChoice(value='US'))),)))))

setattr(get_form_vendor_intake_form, '__module__', "dex_connectors.google_forms.formsmock")

def get_form_unknown_form() -> Case[Form]:
    return Case.branch_result(NOT_FOUND, Form(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getForm", "Google found no visible form with that ID."))

setattr(get_form_unknown_form, '__module__', "dex_connectors.google_forms.formsmock")

def get_form_form_access_denied() -> Case[Form]:
    return Case.branch_result(PROVIDER_REJECTED, Form(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getForm", "Google denied access to the form."))

setattr(get_form_form_access_denied, '__module__', "dex_connectors.google_forms.formsmock")

def list_responses_listed(value: ResponsePage, failure: Failure | None = None) -> Case[ResponsePage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_responses_listed, '__module__', "dex_connectors.google_forms.formsmock")

def list_responses_not_found(value: ResponsePage, failure: Failure | None = None) -> Case[ResponsePage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_responses_not_found, '__module__', "dex_connectors.google_forms.formsmock")

def list_responses_provider_rejected(value: ResponsePage, failure: Failure | None = None) -> Case[ResponsePage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_responses_provider_rejected, '__module__', "dex_connectors.google_forms.formsmock")

def list_responses_invalid_response(value: ResponsePage, failure: Failure | None = None) -> Case[ResponsePage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_responses_invalid_response, '__module__', "dex_connectors.google_forms.formsmock")

def list_responses_defect(value: ResponsePage, failure: Failure | None = None) -> Case[ResponsePage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_responses_defect, '__module__', "dex_connectors.google_forms.formsmock")

def list_responses_retry(failure: Failure, after_seconds: float = 0) -> Case[ResponsePage]:
    return Case.retry(failure, after_seconds)

setattr(list_responses_retry, '__module__', "dex_connectors.google_forms.formsmock")

def list_responses_vendor_intake_responses() -> Case[ResponsePage]:
    return Case.paged(LISTED, (ResponsePage(form_id='1FAIpQLintake', responses=(FormResponse(response_id='r_shared_b', form_id='1FAIpQLintake', created_at=datetime.fromisoformat("2026-09-30T09:15:00.123456Z".replace('Z', '+00:00')), last_submitted_at=datetime.fromisoformat("2026-09-30T09:15:00.123456Z".replace('Z', '+00:00')), answers=(FormAnswer(question_id='q_name', values=('Beta',)),)), FormResponse(response_id='r_acme', form_id='1FAIpQLintake', created_at=datetime.fromisoformat("2026-09-30T08:00:00Z".replace('Z', '+00:00')), last_submitted_at=datetime.fromisoformat("2026-09-30T08:00:00Z".replace('Z', '+00:00')), respondent_email='buyer@acme.example', answers=(FormAnswer(question_id='q_name', values=('Acme GmbH',)), FormAnswer(question_id='q_regions', values=('EU', 'US'))))), next_page_token='responses-page-2'), ResponsePage(form_id='1FAIpQLintake', responses=(FormResponse(response_id='r_acme', form_id='1FAIpQLintake', created_at=datetime.fromisoformat("2026-09-30T08:00:00Z".replace('Z', '+00:00')), last_submitted_at=datetime.fromisoformat("2026-09-30T08:00:00Z".replace('Z', '+00:00')), respondent_email='buyer@acme.example', answers=(FormAnswer(question_id='q_name', values=('Acme GmbH',)), FormAnswer(question_id='q_regions', values=('EU', 'US')))), FormResponse(response_id='r_early', form_id='1FAIpQLintake', created_at=datetime.fromisoformat("2026-09-30T09:00:00Z".replace('Z', '+00:00')), last_submitted_at=datetime.fromisoformat("2026-09-30T09:00:00Z".replace('Z', '+00:00')), answers=(FormAnswer(question_id='q_name', values=('Early',)),))), next_page_token='responses-page-3'), ResponsePage(form_id='1FAIpQLintake', responses=(FormResponse(response_id='r_shared_a', form_id='1FAIpQLintake', created_at=datetime.fromisoformat("2026-09-30T09:15:00.123456Z".replace('Z', '+00:00')), last_submitted_at=datetime.fromisoformat("2026-09-30T09:15:00.123456Z".replace('Z', '+00:00')), answers=(FormAnswer(question_id='q_name', values=('Alpha',)),)),))))

setattr(list_responses_vendor_intake_responses, '__module__', "dex_connectors.google_forms.formsmock")

def list_responses_unknown_form() -> Case[ResponsePage]:
    return Case.branch_result(NOT_FOUND, ResponsePage(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listResponses", "Google found no visible form with that ID."))

setattr(list_responses_unknown_form, '__module__', "dex_connectors.google_forms.formsmock")

def list_responses_response_list_denied() -> Case[ResponsePage]:
    return Case.branch_result(PROVIDER_REJECTED, ResponsePage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listResponses", "Google denied access to the form's responses."))

setattr(list_responses_response_list_denied, '__module__', "dex_connectors.google_forms.formsmock")

def get_response_found(value: FormResponse, failure: Failure | None = None) -> Case[FormResponse]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_response_found, '__module__', "dex_connectors.google_forms.formsmock")

def get_response_not_found(value: FormResponse, failure: Failure | None = None) -> Case[FormResponse]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_response_not_found, '__module__', "dex_connectors.google_forms.formsmock")

def get_response_provider_rejected(value: FormResponse, failure: Failure | None = None) -> Case[FormResponse]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_response_provider_rejected, '__module__', "dex_connectors.google_forms.formsmock")

def get_response_invalid_response(value: FormResponse, failure: Failure | None = None) -> Case[FormResponse]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_response_invalid_response, '__module__', "dex_connectors.google_forms.formsmock")

def get_response_defect(value: FormResponse, failure: Failure | None = None) -> Case[FormResponse]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_response_defect, '__module__', "dex_connectors.google_forms.formsmock")

def get_response_retry(failure: Failure, after_seconds: float = 0) -> Case[FormResponse]:
    return Case.retry(failure, after_seconds)

setattr(get_response_retry, '__module__', "dex_connectors.google_forms.formsmock")

def get_response_acme_submission() -> Case[FormResponse]:
    return Case.branch_result(FOUND, FormResponse(response_id='r_acme', form_id='1FAIpQLintake', created_at=datetime.fromisoformat("2026-09-30T08:00:00Z".replace('Z', '+00:00')), last_submitted_at=datetime.fromisoformat("2026-09-30T08:00:00Z".replace('Z', '+00:00')), respondent_email='buyer@acme.example', answers=(FormAnswer(question_id='q_name', values=('Acme GmbH',)), FormAnswer(question_id='q_regions', values=('EU', 'US')))))

setattr(get_response_acme_submission, '__module__', "dex_connectors.google_forms.formsmock")

def get_response_unknown_response() -> Case[FormResponse]:
    return Case.branch_result(NOT_FOUND, FormResponse(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getResponse", "Google found no visible response with that ID."))

setattr(get_response_unknown_response, '__module__', "dex_connectors.google_forms.formsmock")

def get_response_response_read_denied() -> Case[FormResponse]:
    return Case.branch_result(PROVIDER_REJECTED, FormResponse(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getResponse", "Google denied access to the response."))

setattr(get_response_response_read_denied, '__module__', "dex_connectors.google_forms.formsmock")


class Mock:

    def __init__(self, connection_name: str = "google-forms") -> None:
        self.get_form_script: Query[GetFormInput, Form] = Query(GET_FORM_DEFINITION)
        self.get_form_script.default(get_form_vendor_intake_form())
        self.list_responses_script: Query[ListResponsesInput, ResponsePage] = Query(LIST_RESPONSES_DEFINITION)
        self.list_responses_script.default(list_responses_vendor_intake_responses())
        self.get_response_script: Query[GetResponseInput, FormResponse] = Query(GET_RESPONSE_DEFINITION)
        self.get_response_script.default(get_response_acme_submission())
        self._connection = new_connection_with_operations(
            get_form=self.get_form_script,
            list_responses=self.list_responses_script,
            get_response=self.get_response_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_forms.formsmock")

__all__ = [
    "Mock",
    "get_form_found",
    "get_form_not_found",
    "get_form_provider_rejected",
    "get_form_invalid_response",
    "get_form_defect",
    "get_form_retry",
    "get_form_vendor_intake_form",
    "get_form_unknown_form",
    "get_form_form_access_denied",
    "list_responses_listed",
    "list_responses_not_found",
    "list_responses_provider_rejected",
    "list_responses_invalid_response",
    "list_responses_defect",
    "list_responses_retry",
    "list_responses_vendor_intake_responses",
    "list_responses_unknown_form",
    "list_responses_response_list_denied",
    "get_response_found",
    "get_response_not_found",
    "get_response_provider_rejected",
    "get_response_invalid_response",
    "get_response_defect",
    "get_response_retry",
    "get_response_acme_submission",
    "get_response_unknown_response",
    "get_response_response_read_denied",
]
