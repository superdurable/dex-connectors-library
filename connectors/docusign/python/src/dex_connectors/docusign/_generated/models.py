# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from .timestamps import parse_go_datetime, validate_go_datetime

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# TemplateRole assigns one person to a template role.
@dataclass(frozen=True, slots=True)
class TemplateRole:
    # RoleName is the template's role name, such as Customer, matched as DocuSign matches it.
    role_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'roleName'})
    # Name is the recipient's full name, 1 to 100 characters.
    name: str = ''
    # Email is the recipient's email address, at most 100 characters.
    email: str = ''
    # RoutingOrder overrides the role's routing order, 1 to 999; zero keeps the template's order, which
    # decides who is asked to sign first.
    routing_order: int = _dataclass_field(default=0, metadata={'dex_json_name': 'routingOrder', 'dex_json_omit_default': True})


setattr(TemplateRole, '__module__', 'dex_connectors.docusign.models')


# EnvelopeCustomField is one envelope text custom field, hidden from recipients, such as a correlation
# ID that a Connect event carries back.
@dataclass(frozen=True, slots=True)
class EnvelopeCustomField:
    # Name is the field name, 1 to 50 characters.
    name: str = ''
    # Value is the field value, at most 100 characters.
    value: str = ''


setattr(EnvelopeCustomField, '__module__', 'dex_connectors.docusign.models')


# CreateEnvelopeFromTemplateInput describes one envelope created from a DocuSign server template.
@dataclass(frozen=True, slots=True)
class CreateEnvelopeFromTemplateInput:
    # TemplateID is the server template's GUID, shown in DocuSign under Templates > the template >
    # Template ID.
    template_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'templateId'})
    # TemplateRoles fills the template's roles by name, 1 to 100 of them. Every role the template
    # requires must be filled.
    template_roles: tuple[TemplateRole, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'templateRoles'})
    # EmailSubject overrides the template's email subject, at most 100 characters; blank keeps it.
    email_subject: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailSubject', 'dex_json_omit_default': True})
    # EmailBlurb overrides the template's email message, at most 10,000 characters; blank keeps it.
    email_blurb: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailBlurb', 'dex_json_omit_default': True})
    # IsDraft creates the envelope with status created and sends nothing; false sends it at once.
    is_draft: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDraft', 'dex_json_omit_default': True})
    # CustomFields adds hidden text custom fields, at most 10, such as a correlation ID that Connect
    # events return. Names are 1 to 50 characters and unique; dexIdempotencyKey is reserved.
    custom_fields: tuple[EnvelopeCustomField, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'customFields', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'template_roles', _replace_nil_slices(self.template_roles, 1))


CreateEnvelopeFromTemplateInput.__annotations__['template_roles'] = tuple[TemplateRole, ...] | None
CreateEnvelopeFromTemplateInput.__dataclass_fields__['template_roles'].type = tuple[TemplateRole, ...] | None
setattr(CreateEnvelopeFromTemplateInput, '__module__', 'dex_connectors.docusign.models')


# CreatedEnvelope identifies the envelope that createEnvelopeFromTemplate created.
@dataclass(frozen=True, slots=True)
class CreatedEnvelope:
    # EnvelopeID is the new envelope's GUID.
    envelope_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'envelopeId'})
    # Status is sent, or created for a draft.
    status: str = _dataclass_field(kw_only=True)
    # StatusChangedAt is when DocuSign recorded Status, when it said.
    status_changed_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'statusChangedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # WasRecovered is true when an attempt whose answer was lost created the envelope and this attempt
    # found it by its dexIdempotencyKey custom field instead of creating another.
    was_recovered: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasRecovered', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.status_changed_at, str):
            object.__setattr__(self, 'status_changed_at', parse_go_datetime(self.status_changed_at, 'status_changed_at'))
        elif self.status_changed_at is not None:
            validate_go_datetime(self.status_changed_at, 'status_changed_at')


setattr(CreatedEnvelope, '__module__', 'dex_connectors.docusign.models')


# GetEnvelopeInput names the envelope to read.
@dataclass(frozen=True, slots=True)
class GetEnvelopeInput:
    # EnvelopeID is the envelope's GUID, such as a createEnvelopeFromTemplate Result's envelopeId.
    envelope_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'envelopeId'})


setattr(GetEnvelopeInput, '__module__', 'dex_connectors.docusign.models')


# Envelope is one DocuSign envelope's status and timestamps. Each timestamp is nil until DocuSign
# records the matching transition.
@dataclass(frozen=True, slots=True)
class Envelope:
    # EnvelopeID is the envelope's GUID.
    envelope_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'envelopeId'})
    # Status is DocuSign's current status.
    status: str = _dataclass_field(kw_only=True)
    # EmailSubject is the subject of the envelope's email.
    email_subject: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailSubject', 'dex_json_omit_default': True})
    # CreatedAt is when the envelope was created.
    created_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # SentAt is when DocuSign sent the envelope.
    sent_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'sentAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # DeliveredAt is when every current recipient had opened it.
    delivered_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'deliveredAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # CompletedAt is when the last recipient finished.
    completed_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'completedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # DeclinedAt is when a recipient declined.
    declined_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'declinedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # VoidedAt is when the envelope was voided or expired.
    voided_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'voidedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # StatusChangedAt is when Status last changed.
    status_changed_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'statusChangedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # ExpiresAt is when an unfinished envelope expires and DocuSign voids it.
    expires_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'expiresAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # VoidedReason is the void reason the sender gave or DocuSign recorded for an expiry.
    voided_reason: str = _dataclass_field(default='', metadata={'dex_json_name': 'voidedReason', 'dex_json_omit_default': True})
    # CustomFields holds the envelope's text custom fields, including the connector's dexIdempotencyKey.
    custom_fields: tuple[EnvelopeCustomField, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'customFields', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')
        if isinstance(self.sent_at, str):
            object.__setattr__(self, 'sent_at', parse_go_datetime(self.sent_at, 'sent_at'))
        elif self.sent_at is not None:
            validate_go_datetime(self.sent_at, 'sent_at')
        if isinstance(self.delivered_at, str):
            object.__setattr__(self, 'delivered_at', parse_go_datetime(self.delivered_at, 'delivered_at'))
        elif self.delivered_at is not None:
            validate_go_datetime(self.delivered_at, 'delivered_at')
        if isinstance(self.completed_at, str):
            object.__setattr__(self, 'completed_at', parse_go_datetime(self.completed_at, 'completed_at'))
        elif self.completed_at is not None:
            validate_go_datetime(self.completed_at, 'completed_at')
        if isinstance(self.declined_at, str):
            object.__setattr__(self, 'declined_at', parse_go_datetime(self.declined_at, 'declined_at'))
        elif self.declined_at is not None:
            validate_go_datetime(self.declined_at, 'declined_at')
        if isinstance(self.voided_at, str):
            object.__setattr__(self, 'voided_at', parse_go_datetime(self.voided_at, 'voided_at'))
        elif self.voided_at is not None:
            validate_go_datetime(self.voided_at, 'voided_at')
        if isinstance(self.status_changed_at, str):
            object.__setattr__(self, 'status_changed_at', parse_go_datetime(self.status_changed_at, 'status_changed_at'))
        elif self.status_changed_at is not None:
            validate_go_datetime(self.status_changed_at, 'status_changed_at')
        if isinstance(self.expires_at, str):
            object.__setattr__(self, 'expires_at', parse_go_datetime(self.expires_at, 'expires_at'))
        elif self.expires_at is not None:
            validate_go_datetime(self.expires_at, 'expires_at')


setattr(Envelope, '__module__', 'dex_connectors.docusign.models')


# ListEnvelopeRecipientsInput names the envelope whose recipients to list.
@dataclass(frozen=True, slots=True)
class ListEnvelopeRecipientsInput:
    # EnvelopeID is the envelope's GUID.
    envelope_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'envelopeId'})


setattr(ListEnvelopeRecipientsInput, '__module__', 'dex_connectors.docusign.models')


# Recipient is one envelope recipient's identity, place in the routing order, and progress. Access
# codes and authentication details are never read.
@dataclass(frozen=True, slots=True)
class Recipient:
    # RecipientID is the recipient's ID within the envelope, such as 1.
    recipient_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'recipientId'})
    # Type is the recipient's collection, such as signer or carbonCopy.
    type: str = _dataclass_field(kw_only=True)
    # Name is the recipient's name.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Email is the recipient's email address.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # RoleName is the template role the recipient fills, if any.
    role_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'roleName', 'dex_json_omit_default': True})
    # RoutingOrder is the recipient's routing order; recipients with the same order receive it together.
    routing_order: int = _dataclass_field(default=0, metadata={'dex_json_name': 'routingOrder'})
    # Status is DocuSign's recipient status, such as created, sent, delivered, completed, or declined.
    status: str = ''
    # SentAt is when DocuSign sent the envelope to this recipient.
    sent_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'sentAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # DeliveredAt is when the recipient opened it.
    delivered_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'deliveredAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # SignedAt is when the recipient finished.
    signed_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'signedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # DeclinedAt is when the recipient declined.
    declined_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'declinedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # DeclinedReason is the reason the recipient gave for declining.
    declined_reason: str = _dataclass_field(default='', metadata={'dex_json_name': 'declinedReason', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.sent_at, str):
            object.__setattr__(self, 'sent_at', parse_go_datetime(self.sent_at, 'sent_at'))
        elif self.sent_at is not None:
            validate_go_datetime(self.sent_at, 'sent_at')
        if isinstance(self.delivered_at, str):
            object.__setattr__(self, 'delivered_at', parse_go_datetime(self.delivered_at, 'delivered_at'))
        elif self.delivered_at is not None:
            validate_go_datetime(self.delivered_at, 'delivered_at')
        if isinstance(self.signed_at, str):
            object.__setattr__(self, 'signed_at', parse_go_datetime(self.signed_at, 'signed_at'))
        elif self.signed_at is not None:
            validate_go_datetime(self.signed_at, 'signed_at')
        if isinstance(self.declined_at, str):
            object.__setattr__(self, 'declined_at', parse_go_datetime(self.declined_at, 'declined_at'))
        elif self.declined_at is not None:
            validate_go_datetime(self.declined_at, 'declined_at')


setattr(Recipient, '__module__', 'dex_connectors.docusign.models')


# EnvelopeRecipients is one envelope's recipients sorted by routing order and then recipient ID.
@dataclass(frozen=True, slots=True)
class EnvelopeRecipients:
    # EnvelopeID is the envelope's GUID.
    envelope_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'envelopeId'})
    # CurrentRoutingOrder is the routing order DocuSign is waiting on; zero when DocuSign did not say.
    current_routing_order: int = _dataclass_field(default=0, metadata={'dex_json_name': 'currentRoutingOrder', 'dex_json_omit_default': True})
    # Recipients lists every recipient of the types RecipientType names.
    recipients: tuple[Recipient, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'recipients', _replace_nil_slices(self.recipients, 1))


EnvelopeRecipients.__annotations__['recipients'] = tuple[Recipient, ...] | None
EnvelopeRecipients.__dataclass_fields__['recipients'].type = tuple[Recipient, ...] | None
setattr(EnvelopeRecipients, '__module__', 'dex_connectors.docusign.models')


# VoidEnvelopeInput names the envelope to void and why.
@dataclass(frozen=True, slots=True)
class VoidEnvelopeInput:
    # EnvelopeID is the envelope's GUID.
    envelope_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'envelopeId'})
    # VoidedReason is shown to the recipients, 1 to 200 characters, such as "Signing deadline passed".
    voided_reason: str = _dataclass_field(default='', metadata={'dex_json_name': 'voidedReason'})


setattr(VoidEnvelopeInput, '__module__', 'dex_connectors.docusign.models')


# VoidedEnvelope is the outcome of voidEnvelope.
@dataclass(frozen=True, slots=True)
class VoidedEnvelope:
    # EnvelopeID is the envelope's GUID.
    envelope_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'envelopeId'})
    # WasAlreadyVoided is true when the envelope was voided before this call, such as by an earlier
    # attempt of the same Step, by its sender, or by expiry.
    was_already_voided: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyVoided', 'dex_json_omit_default': True})
    # Status is the envelope's status read back after DocuSign refused the void: voided, or on the
    # notVoidable branch completed, declined, or created. It is empty when this call voided it.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # VoidedReason is the reason DocuSign holds for an envelope that was already voided.
    voided_reason: str = _dataclass_field(default='', metadata={'dex_json_name': 'voidedReason', 'dex_json_omit_default': True})


setattr(VoidedEnvelope, '__module__', 'dex_connectors.docusign.models')


# DownloadCombinedDocumentInput names the envelope whose documents to download.
@dataclass(frozen=True, slots=True)
class DownloadCombinedDocumentInput:
    # EnvelopeID is the envelope's GUID, normally of a completed envelope.
    envelope_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'envelopeId'})
    # IncludeCertificate appends DocuSign's certificate of completion to the combined PDF.
    include_certificate: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'includeCertificate', 'dex_json_omit_default': True})


setattr(DownloadCombinedDocumentInput, '__module__', 'dex_connectors.docusign.models')


# CombinedDocument describes one downloaded combined PDF; it never holds the PDF's bytes.
@dataclass(frozen=True, slots=True)
class CombinedDocument:
    # EnvelopeID is the envelope's GUID.
    envelope_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'envelopeId'})
    # ContentType is application/pdf.
    content_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentType'})
    # ByteCount is the PDF's size in bytes.
    byte_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'byteCount'})
    # SHA256 is the lowercase hexadecimal SHA-256 digest of the PDF's bytes.
    sha256: str = ''
    # IncludesCertificate is true when the PDF ends with the certificate of completion.
    includes_certificate: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'includesCertificate', 'dex_json_omit_default': True})
    # StoredLocation is where the CombinedDocumentStore put the PDF; blank without a store.
    stored_location: str = _dataclass_field(default='', metadata={'dex_json_name': 'storedLocation', 'dex_json_omit_default': True})


setattr(CombinedDocument, '__module__', 'dex_connectors.docusign.models')
