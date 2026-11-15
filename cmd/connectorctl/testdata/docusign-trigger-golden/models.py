# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime

from .timestamps import parse_go_datetime, validate_go_datetime

# GetEnvelopeInput names the envelope to read.
@dataclass(frozen=True, slots=True)
class GetEnvelopeInput:
    # EnvelopeID is the envelope's GUID, such as a createEnvelopeFromTemplate Result's envelopeId.
    envelope_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'envelopeId'})


setattr(GetEnvelopeInput, '__module__', 'dex_connectors.docusign_fixture.models')


# EnvelopeCustomField is one envelope text custom field, hidden from recipients, such as a correlation
# ID that a Connect event carries back.
@dataclass(frozen=True, slots=True)
class EnvelopeCustomField:
    # Name is the field name, 1 to 50 characters.
    name: str = ''
    # Value is the field value, at most 100 characters.
    value: str = ''


setattr(EnvelopeCustomField, '__module__', 'dex_connectors.docusign_fixture.models')


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


setattr(Envelope, '__module__', 'dex_connectors.docusign_fixture.models')


# EnvelopeEvent is one HMAC-verified Connect envelope event in the JSON SIM format. Its Trigger event ID
# is the envelope ID and the event name, such as 93be49ab-0000-0000-0000-f752070d71ec:envelope-completed,
# so a Connect retry or a second Connect configuration delivering the same outcome keeps one ID.
@dataclass(frozen=True, slots=True)
class EnvelopeEvent:
    # Event is envelope-completed, envelope-declined, or envelope-voided.
    event: str = ''
    # EnvelopeID is the envelope's GUID.
    envelope_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'envelopeId'})
    # AccountID is the sender's DocuSign API account.
    account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountId'})
    # GeneratedAt is when Connect generated the message, or when the endpoint received it if Connect
    # left the time blank.
    generated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'generatedAt', 'dex_json_format': 'rfc3339'})
    # Status is the envelope summary's status; blank unless the Connect configuration includes envelope data.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # VoidedReason is the envelope summary's void reason for an envelope-voided event, when included.
    voided_reason: str = _dataclass_field(default='', metadata={'dex_json_name': 'voidedReason', 'dex_json_omit_default': True})
    # CustomFields are the envelope's text custom fields; empty unless the Connect configuration's
    # Include Data lists Custom Fields.
    custom_fields: tuple[EnvelopeCustomField, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'customFields', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.generated_at, str):
            object.__setattr__(self, 'generated_at', parse_go_datetime(self.generated_at, 'generated_at'))
        elif self.generated_at is not None:
            validate_go_datetime(self.generated_at, 'generated_at')


setattr(EnvelopeEvent, '__module__', 'dex_connectors.docusign_fixture.models')


# EnvelopeEventReceivedTriggerConfiguration filters the Connect events one binding records. The zero
# value records every envelope-completed, envelope-declined, and envelope-voided event. It reduces what a
# binding stores; the application's TriggerFilter remains its admission rule.
@dataclass(frozen=True, slots=True)
class EnvelopeEventReceivedTriggerConfiguration:
    # Events lists envelope-completed, envelope-declined, and envelope-voided; empty accepts all three.
    events: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(EnvelopeEventReceivedTriggerConfiguration, '__module__', 'dex_connectors.docusign_fixture.models')
