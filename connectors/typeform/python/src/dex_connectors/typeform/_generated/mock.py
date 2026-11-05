# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Form, FormAnswer, FormAnswerChoice, FormAnswerChoices, FormAnswerMultiFormat, FormAnswerPayment, FormAnswerSignature, FormField, FormFieldChoice, FormPage, FormResponse, FormSummary, FormVariable, GetFormInput, ListFormsInput, ListResponsesInput, ResponsePage, UpsertWebhookInput, Webhook
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_FORM_DEFINITION, INVALID_RESPONSE, LISTED, LIST_FORMS_DEFINITION, LIST_RESPONSES_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, UPSERTED, UPSERT_WEBHOOK_DEFINITION

def list_forms_listed(value: FormPage, failure: Failure | None = None) -> Case[FormPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_forms_listed, '__module__', "dex_connectors.typeform.typeformmock")

def list_forms_provider_rejected(value: FormPage, failure: Failure | None = None) -> Case[FormPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_forms_provider_rejected, '__module__', "dex_connectors.typeform.typeformmock")

def list_forms_invalid_response(value: FormPage, failure: Failure | None = None) -> Case[FormPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_forms_invalid_response, '__module__', "dex_connectors.typeform.typeformmock")

def list_forms_defect(value: FormPage, failure: Failure | None = None) -> Case[FormPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_forms_defect, '__module__', "dex_connectors.typeform.typeformmock")

def list_forms_retry(failure: Failure, after_seconds: float = 0) -> Case[FormPage]:
    return Case.retry(failure, after_seconds)

setattr(list_forms_retry, '__module__', "dex_connectors.typeform.typeformmock")

def list_forms_signup_forms() -> Case[FormPage]:
    return Case.branch_result(LISTED, FormPage(forms=(FormSummary(id='f_signup_1', title='Customer signup', is_public=True, display_url='https://form.typeform.com/to/f_signup_1'),), page=1, page_count=1, total_items=1))

setattr(list_forms_signup_forms, '__module__', "dex_connectors.typeform.typeformmock")

def list_forms_forms_read_denied() -> Case[FormPage]:
    return Case.branch_result(PROVIDER_REJECTED, FormPage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listForms", "Typeform denied the form listing."))

setattr(list_forms_forms_read_denied, '__module__', "dex_connectors.typeform.typeformmock")

def get_form_found(value: Form, failure: Failure | None = None) -> Case[Form]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_form_found, '__module__', "dex_connectors.typeform.typeformmock")

def get_form_not_found(value: Form, failure: Failure | None = None) -> Case[Form]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_form_not_found, '__module__', "dex_connectors.typeform.typeformmock")

def get_form_provider_rejected(value: Form, failure: Failure | None = None) -> Case[Form]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_form_provider_rejected, '__module__', "dex_connectors.typeform.typeformmock")

def get_form_invalid_response(value: Form, failure: Failure | None = None) -> Case[Form]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_form_invalid_response, '__module__', "dex_connectors.typeform.typeformmock")

def get_form_defect(value: Form, failure: Failure | None = None) -> Case[Form]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_form_defect, '__module__', "dex_connectors.typeform.typeformmock")

def get_form_retry(failure: Failure, after_seconds: float = 0) -> Case[Form]:
    return Case.retry(failure, after_seconds)

setattr(get_form_retry, '__module__', "dex_connectors.typeform.typeformmock")

def get_form_signup_form() -> Case[Form]:
    return Case.branch_result(FOUND, Form(id='f_signup_1', title='Customer signup', language='en', fields=(FormField(id='field_name', ref='customer_name', title='Your name', type='short_text'), FormField(id='field_email', ref='customer_email', title='Email address', type='email')), hidden_fields=('campaign',), display_url='https://form.typeform.com/to/f_signup_1'))

setattr(get_form_signup_form, '__module__', "dex_connectors.typeform.typeformmock")

def get_form_form_missing() -> Case[Form]:
    return Case.branch_result(NOT_FOUND, Form(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getForm", "The Typeform form was not found."))

setattr(get_form_form_missing, '__module__', "dex_connectors.typeform.typeformmock")

def get_form_form_read_denied() -> Case[Form]:
    return Case.branch_result(PROVIDER_REJECTED, Form(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getForm", "Typeform denied the form read."))

setattr(get_form_form_read_denied, '__module__', "dex_connectors.typeform.typeformmock")

def list_responses_listed(value: ResponsePage, failure: Failure | None = None) -> Case[ResponsePage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_responses_listed, '__module__', "dex_connectors.typeform.typeformmock")

def list_responses_not_found(value: ResponsePage, failure: Failure | None = None) -> Case[ResponsePage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_responses_not_found, '__module__', "dex_connectors.typeform.typeformmock")

def list_responses_provider_rejected(value: ResponsePage, failure: Failure | None = None) -> Case[ResponsePage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_responses_provider_rejected, '__module__', "dex_connectors.typeform.typeformmock")

def list_responses_invalid_response(value: ResponsePage, failure: Failure | None = None) -> Case[ResponsePage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_responses_invalid_response, '__module__', "dex_connectors.typeform.typeformmock")

def list_responses_defect(value: ResponsePage, failure: Failure | None = None) -> Case[ResponsePage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_responses_defect, '__module__', "dex_connectors.typeform.typeformmock")

def list_responses_retry(failure: Failure, after_seconds: float = 0) -> Case[ResponsePage]:
    return Case.retry(failure, after_seconds)

setattr(list_responses_retry, '__module__', "dex_connectors.typeform.typeformmock")

def list_responses_signup_responses() -> Case[ResponsePage]:
    return Case.branch_result(LISTED, ResponsePage(form_id='f_signup_1', responses=(FormResponse(token='response_ada_1', submitted_at=datetime.fromisoformat("2026-10-26T12:00:00Z".replace('Z', '+00:00')), answers=(FormAnswer(field_id='field_name', field_type='short_text', type='text', text='Ada Lovelace'), FormAnswer(field_id='field_email', field_type='email', type='email', email='ada@example.test')), hidden_fields={'campaign': 'autumn'}),), total_items=1, page_count=1))

setattr(list_responses_signup_responses, '__module__', "dex_connectors.typeform.typeformmock")

def list_responses_responses_form_missing() -> Case[ResponsePage]:
    return Case.branch_result(NOT_FOUND, ResponsePage(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listResponses", "The Typeform form was not found."))

setattr(list_responses_responses_form_missing, '__module__', "dex_connectors.typeform.typeformmock")

def list_responses_responses_read_denied() -> Case[ResponsePage]:
    return Case.branch_result(PROVIDER_REJECTED, ResponsePage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listResponses", "Typeform denied the response listing."))

setattr(list_responses_responses_read_denied, '__module__', "dex_connectors.typeform.typeformmock")

def upsert_webhook_upserted(value: Webhook, failure: Failure | None = None) -> Case[Webhook]:
    return Case.branch_result(UPSERTED, value, failure)

setattr(upsert_webhook_upserted, '__module__', "dex_connectors.typeform.typeformmock")

def upsert_webhook_not_found(value: Webhook, failure: Failure | None = None) -> Case[Webhook]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(upsert_webhook_not_found, '__module__', "dex_connectors.typeform.typeformmock")

def upsert_webhook_provider_rejected(value: Webhook, failure: Failure | None = None) -> Case[Webhook]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(upsert_webhook_provider_rejected, '__module__', "dex_connectors.typeform.typeformmock")

def upsert_webhook_invalid_response(value: Webhook, failure: Failure | None = None) -> Case[Webhook]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(upsert_webhook_invalid_response, '__module__', "dex_connectors.typeform.typeformmock")

def upsert_webhook_defect(value: Webhook, failure: Failure | None = None) -> Case[Webhook]:
    return Case.branch_result(DEFECT, value, failure)

setattr(upsert_webhook_defect, '__module__', "dex_connectors.typeform.typeformmock")

def upsert_webhook_retry(failure: Failure, after_seconds: float = 0) -> Case[Webhook]:
    return Case.retry(failure, after_seconds)

setattr(upsert_webhook_retry, '__module__', "dex_connectors.typeform.typeformmock")

def upsert_webhook_signup_webhook() -> Case[Webhook]:
    return Case.branch_result(UPSERTED, Webhook(id='webhook_signup_1', form_id='f_signup_1', tag='dex-response-recorder', url='https://example.test/webhooks/typeform', is_enabled=True, event_types=('form_response',), created_at=datetime.fromisoformat("2026-10-26T12:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-26T12:00:00Z".replace('Z', '+00:00'))))

setattr(upsert_webhook_signup_webhook, '__module__', "dex_connectors.typeform.typeformmock")

def upsert_webhook_webhook_form_missing() -> Case[Webhook]:
    return Case.branch_result(NOT_FOUND, Webhook(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "upsertWebhook", "The Typeform form was not found."))

setattr(upsert_webhook_webhook_form_missing, '__module__', "dex_connectors.typeform.typeformmock")

def upsert_webhook_webhook_write_denied() -> Case[Webhook]:
    return Case.branch_result(PROVIDER_REJECTED, Webhook(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "upsertWebhook", "Typeform denied the webhook upsert."))

setattr(upsert_webhook_webhook_write_denied, '__module__', "dex_connectors.typeform.typeformmock")


class Mock:

    def __init__(self, connection_name: str = "typeform") -> None:
        self.list_forms_script: Query[ListFormsInput, FormPage] = Query(LIST_FORMS_DEFINITION)
        self.list_forms_script.default(list_forms_signup_forms())
        self.get_form_script: Query[GetFormInput, Form] = Query(GET_FORM_DEFINITION)
        self.get_form_script.default(get_form_signup_form())
        self.list_responses_script: Query[ListResponsesInput, ResponsePage] = Query(LIST_RESPONSES_DEFINITION)
        self.list_responses_script.default(list_responses_signup_responses())
        self.upsert_webhook_script: Mutation[UpsertWebhookInput, Webhook] = Mutation(UPSERT_WEBHOOK_DEFINITION)
        self.upsert_webhook_script.default(upsert_webhook_signup_webhook())
        self._connection = new_connection_with_operations(
            list_forms=self.list_forms_script,
            get_form=self.get_form_script,
            list_responses=self.list_responses_script,
            upsert_webhook=self.upsert_webhook_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.typeform.typeformmock")

__all__ = [
    "Mock",
    "list_forms_listed",
    "list_forms_provider_rejected",
    "list_forms_invalid_response",
    "list_forms_defect",
    "list_forms_retry",
    "list_forms_signup_forms",
    "list_forms_forms_read_denied",
    "get_form_found",
    "get_form_not_found",
    "get_form_provider_rejected",
    "get_form_invalid_response",
    "get_form_defect",
    "get_form_retry",
    "get_form_signup_form",
    "get_form_form_missing",
    "get_form_form_read_denied",
    "list_responses_listed",
    "list_responses_not_found",
    "list_responses_provider_rejected",
    "list_responses_invalid_response",
    "list_responses_defect",
    "list_responses_retry",
    "list_responses_signup_responses",
    "list_responses_responses_form_missing",
    "list_responses_responses_read_denied",
    "upsert_webhook_upserted",
    "upsert_webhook_not_found",
    "upsert_webhook_provider_rejected",
    "upsert_webhook_invalid_response",
    "upsert_webhook_defect",
    "upsert_webhook_retry",
    "upsert_webhook_signup_webhook",
    "upsert_webhook_webhook_form_missing",
    "upsert_webhook_webhook_write_denied",
]
