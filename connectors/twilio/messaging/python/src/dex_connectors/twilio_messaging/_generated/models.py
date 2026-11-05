# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime

from dex_connectors.sdk.timestamps import parse_go_datetime_input

# SendMessageInput describes one message to one recipient.
@dataclass(frozen=True, slots=True)
class SendMessageInput:
    # To is the recipient: an E.164 number such as +14155550100, or whatsapp:+14155550100.
    to: str = ''
    # Body is the message text, at most 1600 characters. It may be empty only when MediaURLs is set.
    body: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # MediaURLs lists at most 10 public HTTPS URLs that Twilio fetches and attaches, which makes an SMS an MMS.
    media_urls: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'mediaUrls', 'dex_json_omit_default': True})
    # Sender overrides the connection's defaultSender with an E.164 number, a whatsapp: address, or
    # an MG Messaging Service SID. Blank uses defaultSender.
    sender: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(SendMessageInput, '__module__', 'dex_connectors.twilio_messaging.models')


# Message is the connector-safe subset of a Twilio Message resource.
#
# It never contains the message body, media URLs, or Twilio's error message
# text. After sendMessage selects providerRejected or uncertain, SID and
# Status are empty and the value echoes the requested account, sender, and
# recipient; ErrorCode then holds the rejection's Twilio error code.
@dataclass(frozen=True, slots=True)
class Message:
    # SID is Twilio's message identifier: SM or MM followed by 32 hexadecimal characters.
    sid: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # AccountSID is the AC account that owns the message.
    account_sid: str = _dataclass_field(default='', metadata={'dex_json_name': 'accountSid'})
    # MessagingServiceSID is the MG Messaging Service that sent the message, when one was used.
    messaging_service_sid: str = _dataclass_field(default='', metadata={'dex_json_name': 'messagingServiceSid', 'dex_json_omit_default': True})
    # From is the sending number, WhatsApp address, short code, or alphanumeric sender ID.
    # It is empty until a Messaging Service chooses a sender.
    from_: str = _dataclass_field(default='', metadata={'dex_json_name': 'from', 'dex_json_omit_default': True})
    # To is the recipient in E.164 form, or a whatsapp: address.
    to: str = ''
    # Status is Twilio's delivery status.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Direction is Twilio's direction value, such as outbound-api.
    direction: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ErrorCode is Twilio's numeric error code, documented at https://www.twilio.com/docs/api/errors; zero means none.
    error_code: int = _dataclass_field(default=0, metadata={'dex_json_name': 'errorCode', 'dex_json_omit_default': True})
    # SegmentCount is the number of SMS segments Twilio billed or will bill.
    segment_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'segmentCount', 'dex_json_omit_default': True})
    # MediaCount is the number of media items attached to the message.
    media_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'mediaCount', 'dex_json_omit_default': True})
    # Price is Twilio's decimal price, usually negative, once known.
    price: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PriceUnit is the ISO 4217 currency of Price.
    price_unit: str = _dataclass_field(default='', metadata={'dex_json_name': 'priceUnit', 'dex_json_omit_default': True})
    # CreatedAt is when Twilio created the message; zero means unknown.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_format': 'rfc3339'})
    # SentAt is when Twilio sent the message; zero means not yet sent.
    sent_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'sentAt', 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when Twilio last changed the message; zero means unknown.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_format': 'rfc3339'})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime_input(self.created_at, 'created_at'))
        if isinstance(self.sent_at, str):
            object.__setattr__(self, 'sent_at', parse_go_datetime_input(self.sent_at, 'sent_at'))
        if isinstance(self.updated_at, str):
            object.__setattr__(self, 'updated_at', parse_go_datetime_input(self.updated_at, 'updated_at'))


setattr(Message, '__module__', 'dex_connectors.twilio_messaging.models')


# GetMessageInput identifies one message.
@dataclass(frozen=True, slots=True)
class GetMessageInput:
    # MessageSID is the SM or MM message SID returned by sendMessage.
    message_sid: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageSid'})


setattr(GetMessageInput, '__module__', 'dex_connectors.twilio_messaging.models')
