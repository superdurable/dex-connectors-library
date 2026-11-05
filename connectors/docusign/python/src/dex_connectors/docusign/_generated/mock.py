# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CombinedDocument, CreateEnvelopeFromTemplateInput, CreatedEnvelope, DownloadCombinedDocumentInput, Envelope, EnvelopeCustomField, EnvelopeRecipients, GetEnvelopeInput, ListEnvelopeRecipientsInput, Recipient, TemplateRole, VoidEnvelopeInput, VoidedEnvelope
from .definitions import CONNECTOR_ID, CREATED, CREATE_ENVELOPE_FROM_TEMPLATE_DEFINITION, DEFECT, DOWNLOADED, DOWNLOAD_COMBINED_DOCUMENT_DEFINITION, FOUND, GET_ENVELOPE_DEFINITION, INVALID_RESPONSE, LISTED, LIST_ENVELOPE_RECIPIENTS_DEFINITION, NOT_FOUND, NOT_VOIDABLE, PROVIDER_REJECTED, TOO_LARGE, UNCERTAIN, VOIDED, VOID_ENVELOPE_DEFINITION

def create_envelope_from_template_created(value: CreatedEnvelope, failure: Failure | None = None) -> Case[CreatedEnvelope]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_envelope_from_template_created, '__module__', "dex_connectors.docusign.docusignmock")

def create_envelope_from_template_provider_rejected(value: CreatedEnvelope, failure: Failure | None = None) -> Case[CreatedEnvelope]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_envelope_from_template_provider_rejected, '__module__', "dex_connectors.docusign.docusignmock")

def create_envelope_from_template_uncertain(value: CreatedEnvelope, failure: Failure) -> Case[CreatedEnvelope]:
    return Case.uncertain(value, failure)

setattr(create_envelope_from_template_uncertain, '__module__', "dex_connectors.docusign.docusignmock")

def create_envelope_from_template_defect(value: CreatedEnvelope, failure: Failure | None = None) -> Case[CreatedEnvelope]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_envelope_from_template_defect, '__module__', "dex_connectors.docusign.docusignmock")

def create_envelope_from_template_retry(failure: Failure, after_seconds: float = 0) -> Case[CreatedEnvelope]:
    return Case.retry(failure, after_seconds)

setattr(create_envelope_from_template_retry, '__module__', "dex_connectors.docusign.docusignmock")

def create_envelope_from_template_meridian_agreement_sent() -> Case[CreatedEnvelope]:
    return Case.branch_result(CREATED, CreatedEnvelope(envelope_id='93be49ab-0000-0000-0000-000000000001', status='sent', status_changed_at=datetime.fromisoformat("2026-10-26T12:00:00Z".replace('Z', '+00:00'))))

setattr(create_envelope_from_template_meridian_agreement_sent, '__module__', "dex_connectors.docusign.docusignmock")

def create_envelope_from_template_unknown_template() -> Case[CreatedEnvelope]:
    return Case.branch_result(PROVIDER_REJECTED, CreatedEnvelope(status=''), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "createEnvelopeFromTemplate", "DocuSign rejected the unknown template ID."))

setattr(create_envelope_from_template_unknown_template, '__module__', "dex_connectors.docusign.docusignmock")

def get_envelope_found(value: Envelope, failure: Failure | None = None) -> Case[Envelope]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_envelope_found, '__module__', "dex_connectors.docusign.docusignmock")

def get_envelope_not_found(value: Envelope, failure: Failure | None = None) -> Case[Envelope]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_envelope_not_found, '__module__', "dex_connectors.docusign.docusignmock")

def get_envelope_provider_rejected(value: Envelope, failure: Failure | None = None) -> Case[Envelope]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_envelope_provider_rejected, '__module__', "dex_connectors.docusign.docusignmock")

def get_envelope_invalid_response(value: Envelope, failure: Failure | None = None) -> Case[Envelope]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_envelope_invalid_response, '__module__', "dex_connectors.docusign.docusignmock")

def get_envelope_defect(value: Envelope, failure: Failure | None = None) -> Case[Envelope]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_envelope_defect, '__module__', "dex_connectors.docusign.docusignmock")

def get_envelope_retry(failure: Failure, after_seconds: float = 0) -> Case[Envelope]:
    return Case.retry(failure, after_seconds)

setattr(get_envelope_retry, '__module__', "dex_connectors.docusign.docusignmock")

def get_envelope_completed_meridian_agreement() -> Case[Envelope]:
    return Case.branch_result(FOUND, Envelope(envelope_id='93be49ab-0000-0000-0000-000000000001', status='completed', email_subject='Meridian Corp: MSA', created_at=datetime.fromisoformat("2026-10-26T12:00:00Z".replace('Z', '+00:00')), sent_at=datetime.fromisoformat("2026-10-26T12:00:00Z".replace('Z', '+00:00')), completed_at=datetime.fromisoformat("2026-10-26T12:15:00Z".replace('Z', '+00:00'))))

setattr(get_envelope_completed_meridian_agreement, '__module__', "dex_connectors.docusign.docusignmock")

def get_envelope_unknown_envelope() -> Case[Envelope]:
    return Case.branch_result(NOT_FOUND, Envelope(status=''), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getEnvelope", "DocuSign found no envelope with that ID."))

setattr(get_envelope_unknown_envelope, '__module__', "dex_connectors.docusign.docusignmock")

def get_envelope_account_access_denied() -> Case[Envelope]:
    return Case.branch_result(PROVIDER_REJECTED, Envelope(status=''), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getEnvelope", "DocuSign denied access to the envelope."))

setattr(get_envelope_account_access_denied, '__module__', "dex_connectors.docusign.docusignmock")

def list_envelope_recipients_listed(value: EnvelopeRecipients, failure: Failure | None = None) -> Case[EnvelopeRecipients]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_envelope_recipients_listed, '__module__', "dex_connectors.docusign.docusignmock")

def list_envelope_recipients_not_found(value: EnvelopeRecipients, failure: Failure | None = None) -> Case[EnvelopeRecipients]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_envelope_recipients_not_found, '__module__', "dex_connectors.docusign.docusignmock")

def list_envelope_recipients_provider_rejected(value: EnvelopeRecipients, failure: Failure | None = None) -> Case[EnvelopeRecipients]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_envelope_recipients_provider_rejected, '__module__', "dex_connectors.docusign.docusignmock")

def list_envelope_recipients_invalid_response(value: EnvelopeRecipients, failure: Failure | None = None) -> Case[EnvelopeRecipients]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_envelope_recipients_invalid_response, '__module__', "dex_connectors.docusign.docusignmock")

def list_envelope_recipients_defect(value: EnvelopeRecipients, failure: Failure | None = None) -> Case[EnvelopeRecipients]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_envelope_recipients_defect, '__module__', "dex_connectors.docusign.docusignmock")

def list_envelope_recipients_retry(failure: Failure, after_seconds: float = 0) -> Case[EnvelopeRecipients]:
    return Case.retry(failure, after_seconds)

setattr(list_envelope_recipients_retry, '__module__', "dex_connectors.docusign.docusignmock")

def list_envelope_recipients_meridian_signer() -> Case[EnvelopeRecipients]:
    return Case.branch_result(LISTED, EnvelopeRecipients(envelope_id='93be49ab-0000-0000-0000-000000000001', current_routing_order=1, recipients=(Recipient(recipient_id='1', type='signer', name='Priya Raman', email='priya@meridian.example.com', role_name='Customer', routing_order=1, status='completed', sent_at=datetime.fromisoformat("2026-10-26T12:00:00Z".replace('Z', '+00:00')), signed_at=datetime.fromisoformat("2026-10-26T12:15:00Z".replace('Z', '+00:00'))),)))

setattr(list_envelope_recipients_meridian_signer, '__module__', "dex_connectors.docusign.docusignmock")

def list_envelope_recipients_unknown_envelope() -> Case[EnvelopeRecipients]:
    return Case.branch_result(NOT_FOUND, EnvelopeRecipients(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listEnvelopeRecipients", "DocuSign found no envelope with that ID."))

setattr(list_envelope_recipients_unknown_envelope, '__module__', "dex_connectors.docusign.docusignmock")

def list_envelope_recipients_account_access_denied() -> Case[EnvelopeRecipients]:
    return Case.branch_result(PROVIDER_REJECTED, EnvelopeRecipients(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listEnvelopeRecipients", "DocuSign denied access to the recipients."))

setattr(list_envelope_recipients_account_access_denied, '__module__', "dex_connectors.docusign.docusignmock")

def void_envelope_voided(value: VoidedEnvelope, failure: Failure | None = None) -> Case[VoidedEnvelope]:
    return Case.branch_result(VOIDED, value, failure)

setattr(void_envelope_voided, '__module__', "dex_connectors.docusign.docusignmock")

def void_envelope_not_voidable(value: VoidedEnvelope, failure: Failure | None = None) -> Case[VoidedEnvelope]:
    return Case.branch_result(NOT_VOIDABLE, value, failure)

setattr(void_envelope_not_voidable, '__module__', "dex_connectors.docusign.docusignmock")

def void_envelope_not_found(value: VoidedEnvelope, failure: Failure | None = None) -> Case[VoidedEnvelope]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(void_envelope_not_found, '__module__', "dex_connectors.docusign.docusignmock")

def void_envelope_provider_rejected(value: VoidedEnvelope, failure: Failure | None = None) -> Case[VoidedEnvelope]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(void_envelope_provider_rejected, '__module__', "dex_connectors.docusign.docusignmock")

def void_envelope_invalid_response(value: VoidedEnvelope, failure: Failure | None = None) -> Case[VoidedEnvelope]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(void_envelope_invalid_response, '__module__', "dex_connectors.docusign.docusignmock")

def void_envelope_defect(value: VoidedEnvelope, failure: Failure | None = None) -> Case[VoidedEnvelope]:
    return Case.branch_result(DEFECT, value, failure)

setattr(void_envelope_defect, '__module__', "dex_connectors.docusign.docusignmock")

def void_envelope_retry(failure: Failure, after_seconds: float = 0) -> Case[VoidedEnvelope]:
    return Case.retry(failure, after_seconds)

setattr(void_envelope_retry, '__module__', "dex_connectors.docusign.docusignmock")

def void_envelope_expired_agreement_voided() -> Case[VoidedEnvelope]:
    return Case.branch_result(VOIDED, VoidedEnvelope(envelope_id='93be49ab-0000-0000-0000-000000000002'))

setattr(void_envelope_expired_agreement_voided, '__module__', "dex_connectors.docusign.docusignmock")

def void_envelope_completed_agreement_cannot_be_voided() -> Case[VoidedEnvelope]:
    return Case.branch_result(NOT_VOIDABLE, VoidedEnvelope(envelope_id='93be49ab-0000-0000-0000-000000000001', status='completed'), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "voidEnvelope", "DocuSign cannot void a completed envelope."))

setattr(void_envelope_completed_agreement_cannot_be_voided, '__module__', "dex_connectors.docusign.docusignmock")

def void_envelope_unknown_envelope() -> Case[VoidedEnvelope]:
    return Case.branch_result(NOT_FOUND, VoidedEnvelope(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "voidEnvelope", "DocuSign found no envelope with that ID."))

setattr(void_envelope_unknown_envelope, '__module__', "dex_connectors.docusign.docusignmock")

def void_envelope_sender_permission_denied() -> Case[VoidedEnvelope]:
    return Case.branch_result(PROVIDER_REJECTED, VoidedEnvelope(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "voidEnvelope", "DocuSign denied permission to void the envelope."))

setattr(void_envelope_sender_permission_denied, '__module__', "dex_connectors.docusign.docusignmock")

def download_combined_document_downloaded(value: CombinedDocument, failure: Failure | None = None) -> Case[CombinedDocument]:
    return Case.branch_result(DOWNLOADED, value, failure)

setattr(download_combined_document_downloaded, '__module__', "dex_connectors.docusign.docusignmock")

def download_combined_document_not_found(value: CombinedDocument, failure: Failure | None = None) -> Case[CombinedDocument]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(download_combined_document_not_found, '__module__', "dex_connectors.docusign.docusignmock")

def download_combined_document_too_large(value: CombinedDocument, failure: Failure | None = None) -> Case[CombinedDocument]:
    return Case.branch_result(TOO_LARGE, value, failure)

setattr(download_combined_document_too_large, '__module__', "dex_connectors.docusign.docusignmock")

def download_combined_document_provider_rejected(value: CombinedDocument, failure: Failure | None = None) -> Case[CombinedDocument]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(download_combined_document_provider_rejected, '__module__', "dex_connectors.docusign.docusignmock")

def download_combined_document_invalid_response(value: CombinedDocument, failure: Failure | None = None) -> Case[CombinedDocument]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(download_combined_document_invalid_response, '__module__', "dex_connectors.docusign.docusignmock")

def download_combined_document_defect(value: CombinedDocument, failure: Failure | None = None) -> Case[CombinedDocument]:
    return Case.branch_result(DEFECT, value, failure)

setattr(download_combined_document_defect, '__module__', "dex_connectors.docusign.docusignmock")

def download_combined_document_retry(failure: Failure, after_seconds: float = 0) -> Case[CombinedDocument]:
    return Case.retry(failure, after_seconds)

setattr(download_combined_document_retry, '__module__', "dex_connectors.docusign.docusignmock")

def download_combined_document_signed_meridian_pdf() -> Case[CombinedDocument]:
    return Case.branch_result(DOWNLOADED, CombinedDocument(envelope_id='93be49ab-0000-0000-0000-000000000001', content_type='application/pdf', byte_count=54, sha256='0eafa3ee6557dd3e42fb1ff761e387c0e8dd8bd8475387ccd8d6191948369682', includes_certificate=True, stored_location='signed-meridian-msa.pdf'))

setattr(download_combined_document_signed_meridian_pdf, '__module__', "dex_connectors.docusign.docusignmock")

def download_combined_document_unknown_envelope() -> Case[CombinedDocument]:
    return Case.branch_result(NOT_FOUND, CombinedDocument(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "downloadCombinedDocument", "DocuSign found no document for that envelope."))

setattr(download_combined_document_unknown_envelope, '__module__', "dex_connectors.docusign.docusignmock")

def download_combined_document_oversized_pdf() -> Case[CombinedDocument]:
    return Case.branch_result(TOO_LARGE, CombinedDocument(), Failure(FailureKind.RESPONSE_TOO_LARGE, CONNECTOR_ID, "downloadCombinedDocument", "DocuSign's combined PDF exceeded the configured limit."))

setattr(download_combined_document_oversized_pdf, '__module__', "dex_connectors.docusign.docusignmock")

def download_combined_document_account_access_denied() -> Case[CombinedDocument]:
    return Case.branch_result(PROVIDER_REJECTED, CombinedDocument(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "downloadCombinedDocument", "DocuSign denied access to the document."))

setattr(download_combined_document_account_access_denied, '__module__', "dex_connectors.docusign.docusignmock")


class Mock:

    def __init__(self, connection_name: str = "docusign") -> None:
        self.create_envelope_from_template_script: Mutation[CreateEnvelopeFromTemplateInput, CreatedEnvelope] = Mutation(CREATE_ENVELOPE_FROM_TEMPLATE_DEFINITION)
        self.create_envelope_from_template_script.default(create_envelope_from_template_meridian_agreement_sent())
        self.get_envelope_script: Query[GetEnvelopeInput, Envelope] = Query(GET_ENVELOPE_DEFINITION)
        self.get_envelope_script.default(get_envelope_completed_meridian_agreement())
        self.list_envelope_recipients_script: Query[ListEnvelopeRecipientsInput, EnvelopeRecipients] = Query(LIST_ENVELOPE_RECIPIENTS_DEFINITION)
        self.list_envelope_recipients_script.default(list_envelope_recipients_meridian_signer())
        self.void_envelope_script: Mutation[VoidEnvelopeInput, VoidedEnvelope] = Mutation(VOID_ENVELOPE_DEFINITION)
        self.void_envelope_script.default(void_envelope_expired_agreement_voided())
        self.download_combined_document_script: Query[DownloadCombinedDocumentInput, CombinedDocument] = Query(DOWNLOAD_COMBINED_DOCUMENT_DEFINITION)
        self.download_combined_document_script.default(download_combined_document_signed_meridian_pdf())
        self._connection = new_connection_with_operations(
            create_envelope_from_template=self.create_envelope_from_template_script,
            get_envelope=self.get_envelope_script,
            list_envelope_recipients=self.list_envelope_recipients_script,
            void_envelope=self.void_envelope_script,
            download_combined_document=self.download_combined_document_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.docusign.docusignmock")

__all__ = [
    "Mock",
    "create_envelope_from_template_created",
    "create_envelope_from_template_provider_rejected",
    "create_envelope_from_template_uncertain",
    "create_envelope_from_template_defect",
    "create_envelope_from_template_retry",
    "create_envelope_from_template_meridian_agreement_sent",
    "create_envelope_from_template_unknown_template",
    "get_envelope_found",
    "get_envelope_not_found",
    "get_envelope_provider_rejected",
    "get_envelope_invalid_response",
    "get_envelope_defect",
    "get_envelope_retry",
    "get_envelope_completed_meridian_agreement",
    "get_envelope_unknown_envelope",
    "get_envelope_account_access_denied",
    "list_envelope_recipients_listed",
    "list_envelope_recipients_not_found",
    "list_envelope_recipients_provider_rejected",
    "list_envelope_recipients_invalid_response",
    "list_envelope_recipients_defect",
    "list_envelope_recipients_retry",
    "list_envelope_recipients_meridian_signer",
    "list_envelope_recipients_unknown_envelope",
    "list_envelope_recipients_account_access_denied",
    "void_envelope_voided",
    "void_envelope_not_voidable",
    "void_envelope_not_found",
    "void_envelope_provider_rejected",
    "void_envelope_invalid_response",
    "void_envelope_defect",
    "void_envelope_retry",
    "void_envelope_expired_agreement_voided",
    "void_envelope_completed_agreement_cannot_be_voided",
    "void_envelope_unknown_envelope",
    "void_envelope_sender_permission_denied",
    "download_combined_document_downloaded",
    "download_combined_document_not_found",
    "download_combined_document_too_large",
    "download_combined_document_provider_rejected",
    "download_combined_document_invalid_response",
    "download_combined_document_defect",
    "download_combined_document_retry",
    "download_combined_document_signed_meridian_pdf",
    "download_combined_document_unknown_envelope",
    "download_combined_document_oversized_pdf",
    "download_combined_document_account_access_denied",
]
