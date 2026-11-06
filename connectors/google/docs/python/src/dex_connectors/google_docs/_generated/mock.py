# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AppendTextInput, AppendTextOutput, CreateDocumentInput, CreateDocumentOutput, CreatedDocument, GetDocumentTextInput, GetDocumentTextOutput, PlaceholderReplacement, PlaceholderReplacementResult, ReplaceDocumentTextInput, ReplaceDocumentTextOutput
from .definitions import CONNECTOR_ID, PROVIDER, APPENDED, APPEND_TEXT_DEFINITION, CREATED, CREATE_DOCUMENT_DEFINITION, DEFECT, GET_DOCUMENT_TEXT_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PLACEHOLDER_NOT_FOUND, PROVIDER_REJECTED, READ, REPLACED, REPLACE_DOCUMENT_TEXT_DEFINITION, REVISION_CHANGED, TOO_LARGE, UNCERTAIN

def get_document_text_read(value: GetDocumentTextOutput, failure: Failure | None = None) -> Case[GetDocumentTextOutput]:
    return Case.branch_result(READ, value, failure)

setattr(get_document_text_read, '__module__', "dex_connectors.google_docs.docsmock")

def get_document_text_not_found(value: GetDocumentTextOutput, failure: Failure | None = None) -> Case[GetDocumentTextOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_document_text_not_found, '__module__', "dex_connectors.google_docs.docsmock")

def get_document_text_too_large(value: GetDocumentTextOutput, failure: Failure | None = None) -> Case[GetDocumentTextOutput]:
    return Case.branch_result(TOO_LARGE, value, failure)

setattr(get_document_text_too_large, '__module__', "dex_connectors.google_docs.docsmock")

def get_document_text_provider_rejected(value: GetDocumentTextOutput, failure: Failure | None = None) -> Case[GetDocumentTextOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_document_text_provider_rejected, '__module__', "dex_connectors.google_docs.docsmock")

def get_document_text_invalid_response(value: GetDocumentTextOutput, failure: Failure | None = None) -> Case[GetDocumentTextOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_document_text_invalid_response, '__module__', "dex_connectors.google_docs.docsmock")

def get_document_text_defect(value: GetDocumentTextOutput, failure: Failure | None = None) -> Case[GetDocumentTextOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_document_text_defect, '__module__', "dex_connectors.google_docs.docsmock")

def get_document_text_retry(failure: Failure, after_seconds: float = 0) -> Case[GetDocumentTextOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_document_text_retry, '__module__', "dex_connectors.google_docs.docsmock")

def get_document_text_policy_template() -> Case[GetDocumentTextOutput]:
    return Case.branch_result(READ, GetDocumentTextOutput(document_id='policyTemplate1', title='Refund Policy Template', revision_id='rev-template-1', format='markdown', text='# Refund Policy\n\nEffective {{effectiveDate}} for {{companyName}} customers.\n', byte_count=76))

setattr(get_document_text_policy_template, '__module__', "dex_connectors.google_docs.docsmock")

def get_document_text_not_found_failure() -> Case[GetDocumentTextOutput]:
    return Case.branch_result(NOT_FOUND, GetDocumentTextOutput(format=''), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getDocumentText", "The document does not exist or the connection cannot see it."))

setattr(get_document_text_not_found_failure, '__module__', "dex_connectors.google_docs.docsmock")

def get_document_text_too_large_failure() -> Case[GetDocumentTextOutput]:
    return Case.branch_result(TOO_LARGE, GetDocumentTextOutput(format=''), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "getDocumentText", "The Google Docs response exceeds the configured limit."))

setattr(get_document_text_too_large_failure, '__module__', "dex_connectors.google_docs.docsmock")

def get_document_text_provider_rejected_failure() -> Case[GetDocumentTextOutput]:
    return Case.branch_result(PROVIDER_REJECTED, GetDocumentTextOutput(format=''), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getDocumentText", "The connection cannot read the document."))

setattr(get_document_text_provider_rejected_failure, '__module__', "dex_connectors.google_docs.docsmock")

def get_document_text_invalid_response_failure() -> Case[GetDocumentTextOutput]:
    return Case.branch_result(INVALID_RESPONSE, GetDocumentTextOutput(format=''), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getDocumentText", "Google returned an invalid document response."))

setattr(get_document_text_invalid_response_failure, '__module__', "dex_connectors.google_docs.docsmock")

def create_document_created(value: CreateDocumentOutput, failure: Failure | None = None) -> Case[CreateDocumentOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_document_created, '__module__', "dex_connectors.google_docs.docsmock")

def create_document_provider_rejected(value: CreateDocumentOutput, failure: Failure | None = None) -> Case[CreateDocumentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_document_provider_rejected, '__module__', "dex_connectors.google_docs.docsmock")

def create_document_invalid_response(value: CreateDocumentOutput, failure: Failure | None = None) -> Case[CreateDocumentOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(create_document_invalid_response, '__module__', "dex_connectors.google_docs.docsmock")

def create_document_uncertain(value: CreateDocumentOutput, failure: Failure) -> Case[CreateDocumentOutput]:
    return Case.uncertain(value, failure)

setattr(create_document_uncertain, '__module__', "dex_connectors.google_docs.docsmock")

def create_document_defect(value: CreateDocumentOutput, failure: Failure | None = None) -> Case[CreateDocumentOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_document_defect, '__module__', "dex_connectors.google_docs.docsmock")

def create_document_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateDocumentOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_document_retry, '__module__', "dex_connectors.google_docs.docsmock")

def create_document_created_policy_draft() -> Case[CreateDocumentOutput]:
    return Case.branch_result(CREATED, CreateDocumentOutput(document=CreatedDocument(document_id='policyDraft1', title='Acme Refund Policy', parents=('fld_policies',), web_view_link='https://docs.google.com/document/d/policyDraft1/edit', created_time=datetime.fromisoformat("2026-10-26T09:00:00Z".replace('Z', '+00:00')))))

setattr(create_document_created_policy_draft, '__module__', "dex_connectors.google_docs.docsmock")

def create_document_provider_rejected_failure() -> Case[CreateDocumentOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateDocumentOutput(document=CreatedDocument()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "createDocument", "The parent Drive folder was not found."))

setattr(create_document_provider_rejected_failure, '__module__', "dex_connectors.google_docs.docsmock")

def create_document_invalid_response_failure() -> Case[CreateDocumentOutput]:
    return Case.branch_result(INVALID_RESPONSE, CreateDocumentOutput(document=CreatedDocument()), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "createDocument", "The duplicate lookup returned an invalid response."))

setattr(create_document_invalid_response_failure, '__module__', "dex_connectors.google_docs.docsmock")

def replace_document_text_replaced(value: ReplaceDocumentTextOutput, failure: Failure | None = None) -> Case[ReplaceDocumentTextOutput]:
    return Case.branch_result(REPLACED, value, failure)

setattr(replace_document_text_replaced, '__module__', "dex_connectors.google_docs.docsmock")

def replace_document_text_placeholder_not_found(value: ReplaceDocumentTextOutput, failure: Failure | None = None) -> Case[ReplaceDocumentTextOutput]:
    return Case.branch_result(PLACEHOLDER_NOT_FOUND, value, failure)

setattr(replace_document_text_placeholder_not_found, '__module__', "dex_connectors.google_docs.docsmock")

def replace_document_text_revision_changed(value: ReplaceDocumentTextOutput, failure: Failure | None = None) -> Case[ReplaceDocumentTextOutput]:
    return Case.branch_result(REVISION_CHANGED, value, failure)

setattr(replace_document_text_revision_changed, '__module__', "dex_connectors.google_docs.docsmock")

def replace_document_text_not_found(value: ReplaceDocumentTextOutput, failure: Failure | None = None) -> Case[ReplaceDocumentTextOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(replace_document_text_not_found, '__module__', "dex_connectors.google_docs.docsmock")

def replace_document_text_provider_rejected(value: ReplaceDocumentTextOutput, failure: Failure | None = None) -> Case[ReplaceDocumentTextOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(replace_document_text_provider_rejected, '__module__', "dex_connectors.google_docs.docsmock")

def replace_document_text_invalid_response(value: ReplaceDocumentTextOutput, failure: Failure | None = None) -> Case[ReplaceDocumentTextOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(replace_document_text_invalid_response, '__module__', "dex_connectors.google_docs.docsmock")

def replace_document_text_defect(value: ReplaceDocumentTextOutput, failure: Failure | None = None) -> Case[ReplaceDocumentTextOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(replace_document_text_defect, '__module__', "dex_connectors.google_docs.docsmock")

def replace_document_text_retry(failure: Failure, after_seconds: float = 0) -> Case[ReplaceDocumentTextOutput]:
    return Case.retry(failure, after_seconds)

setattr(replace_document_text_retry, '__module__', "dex_connectors.google_docs.docsmock")

def replace_document_text_filled_policy_draft() -> Case[ReplaceDocumentTextOutput]:
    return Case.branch_result(REPLACED, ReplaceDocumentTextOutput(document_id='policyDraft1', revision_id='rev-draft-2', replacements=(PlaceholderReplacementResult(placeholder='{{effectiveDate}}', occurrences_changed=1), PlaceholderReplacementResult(placeholder='{{companyName}}', occurrences_changed=1))))

setattr(replace_document_text_filled_policy_draft, '__module__', "dex_connectors.google_docs.docsmock")

def replace_document_text_placeholder_not_found_failure() -> Case[ReplaceDocumentTextOutput]:
    return Case.branch_result(PLACEHOLDER_NOT_FOUND, ReplaceDocumentTextOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "replaceDocumentText", "A requested placeholder is absent from the document body."))

setattr(replace_document_text_placeholder_not_found_failure, '__module__', "dex_connectors.google_docs.docsmock")

def replace_document_text_revision_changed_failure() -> Case[ReplaceDocumentTextOutput]:
    return Case.branch_result(REVISION_CHANGED, ReplaceDocumentTextOutput(), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "replaceDocumentText", "The document revision changed before the replacement."))

setattr(replace_document_text_revision_changed_failure, '__module__', "dex_connectors.google_docs.docsmock")

def replace_document_text_not_found_failure() -> Case[ReplaceDocumentTextOutput]:
    return Case.branch_result(NOT_FOUND, ReplaceDocumentTextOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "replaceDocumentText", "The document does not exist or the connection cannot see it."))

setattr(replace_document_text_not_found_failure, '__module__', "dex_connectors.google_docs.docsmock")

def replace_document_text_provider_rejected_failure() -> Case[ReplaceDocumentTextOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ReplaceDocumentTextOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "replaceDocumentText", "The connection cannot edit the document."))

setattr(replace_document_text_provider_rejected_failure, '__module__', "dex_connectors.google_docs.docsmock")

def replace_document_text_invalid_response_failure() -> Case[ReplaceDocumentTextOutput]:
    return Case.branch_result(INVALID_RESPONSE, ReplaceDocumentTextOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "replaceDocumentText", "Google returned a malformed document response."))

setattr(replace_document_text_invalid_response_failure, '__module__', "dex_connectors.google_docs.docsmock")

def append_text_appended(value: AppendTextOutput, failure: Failure | None = None) -> Case[AppendTextOutput]:
    return Case.branch_result(APPENDED, value, failure)

setattr(append_text_appended, '__module__', "dex_connectors.google_docs.docsmock")

def append_text_revision_changed(value: AppendTextOutput, failure: Failure | None = None) -> Case[AppendTextOutput]:
    return Case.branch_result(REVISION_CHANGED, value, failure)

setattr(append_text_revision_changed, '__module__', "dex_connectors.google_docs.docsmock")

def append_text_not_found(value: AppendTextOutput, failure: Failure | None = None) -> Case[AppendTextOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(append_text_not_found, '__module__', "dex_connectors.google_docs.docsmock")

def append_text_provider_rejected(value: AppendTextOutput, failure: Failure | None = None) -> Case[AppendTextOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(append_text_provider_rejected, '__module__', "dex_connectors.google_docs.docsmock")

def append_text_invalid_response(value: AppendTextOutput, failure: Failure | None = None) -> Case[AppendTextOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(append_text_invalid_response, '__module__', "dex_connectors.google_docs.docsmock")

def append_text_defect(value: AppendTextOutput, failure: Failure | None = None) -> Case[AppendTextOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(append_text_defect, '__module__', "dex_connectors.google_docs.docsmock")

def append_text_retry(failure: Failure, after_seconds: float = 0) -> Case[AppendTextOutput]:
    return Case.retry(failure, after_seconds)

setattr(append_text_retry, '__module__', "dex_connectors.google_docs.docsmock")

def append_text_stamped_policy_draft() -> Case[AppendTextOutput]:
    return Case.branch_result(APPENDED, AppendTextOutput(document_id='policyDraft1', revision_id='rev-draft-3'))

setattr(append_text_stamped_policy_draft, '__module__', "dex_connectors.google_docs.docsmock")

def append_text_revision_changed_failure() -> Case[AppendTextOutput]:
    return Case.branch_result(REVISION_CHANGED, AppendTextOutput(), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "appendText", "The document revision changed before the append."))

setattr(append_text_revision_changed_failure, '__module__', "dex_connectors.google_docs.docsmock")

def append_text_not_found_failure() -> Case[AppendTextOutput]:
    return Case.branch_result(NOT_FOUND, AppendTextOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "appendText", "The document does not exist or the connection cannot see it."))

setattr(append_text_not_found_failure, '__module__', "dex_connectors.google_docs.docsmock")

def append_text_provider_rejected_failure() -> Case[AppendTextOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AppendTextOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "appendText", "The connection cannot edit the document."))

setattr(append_text_provider_rejected_failure, '__module__', "dex_connectors.google_docs.docsmock")

def append_text_invalid_response_failure() -> Case[AppendTextOutput]:
    return Case.branch_result(INVALID_RESPONSE, AppendTextOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "appendText", "Google returned a malformed document response."))

setattr(append_text_invalid_response_failure, '__module__', "dex_connectors.google_docs.docsmock")


class Mock:

    def __init__(self, connection_name: str = "google-docs") -> None:
        self.get_document_text_script: Query[GetDocumentTextInput, GetDocumentTextOutput] = Query(GET_DOCUMENT_TEXT_DEFINITION)
        self.get_document_text_script.default(get_document_text_policy_template())
        self.create_document_script: Mutation[CreateDocumentInput, CreateDocumentOutput] = Mutation(CREATE_DOCUMENT_DEFINITION)
        self.create_document_script.default(create_document_created_policy_draft())
        self.replace_document_text_script: Mutation[ReplaceDocumentTextInput, ReplaceDocumentTextOutput] = Mutation(REPLACE_DOCUMENT_TEXT_DEFINITION)
        self.replace_document_text_script.default(replace_document_text_filled_policy_draft())
        self.append_text_script: Mutation[AppendTextInput, AppendTextOutput] = Mutation(APPEND_TEXT_DEFINITION)
        self.append_text_script.default(append_text_stamped_policy_draft())
        self._connection = new_connection_with_operations(
            get_document_text=self.get_document_text_script,
            create_document=self.create_document_script,
            replace_document_text=self.replace_document_text_script,
            append_text=self.append_text_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_docs.docsmock")

__all__ = [
    "Mock",
    "get_document_text_read",
    "get_document_text_not_found",
    "get_document_text_too_large",
    "get_document_text_provider_rejected",
    "get_document_text_invalid_response",
    "get_document_text_defect",
    "get_document_text_retry",
    "get_document_text_policy_template",
    "get_document_text_not_found_failure",
    "get_document_text_too_large_failure",
    "get_document_text_provider_rejected_failure",
    "get_document_text_invalid_response_failure",
    "create_document_created",
    "create_document_provider_rejected",
    "create_document_invalid_response",
    "create_document_uncertain",
    "create_document_defect",
    "create_document_retry",
    "create_document_created_policy_draft",
    "create_document_provider_rejected_failure",
    "create_document_invalid_response_failure",
    "replace_document_text_replaced",
    "replace_document_text_placeholder_not_found",
    "replace_document_text_revision_changed",
    "replace_document_text_not_found",
    "replace_document_text_provider_rejected",
    "replace_document_text_invalid_response",
    "replace_document_text_defect",
    "replace_document_text_retry",
    "replace_document_text_filled_policy_draft",
    "replace_document_text_placeholder_not_found_failure",
    "replace_document_text_revision_changed_failure",
    "replace_document_text_not_found_failure",
    "replace_document_text_provider_rejected_failure",
    "replace_document_text_invalid_response_failure",
    "append_text_appended",
    "append_text_revision_changed",
    "append_text_not_found",
    "append_text_provider_rejected",
    "append_text_invalid_response",
    "append_text_defect",
    "append_text_retry",
    "append_text_stamped_policy_draft",
    "append_text_revision_changed_failure",
    "append_text_not_found_failure",
    "append_text_provider_rejected_failure",
    "append_text_invalid_response_failure",
]
