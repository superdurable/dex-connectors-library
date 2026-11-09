# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListPhoneNumbersInput selects one WhatsApp Business Account and an optional page cursor.
@dataclass(frozen=True, slots=True)
class ListPhoneNumbersInput:
    # BusinessAccountID is the numeric WABA ID in Meta Business Settings.
    business_account_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'businessAccountId'})
    # After is the opaque cursor from a prior PhoneNumbersPage; blank reads the first page.
    after: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListPhoneNumbersInput, '__module__', 'dex_connectors.meta_whatsapp_business.models')


# BusinessPhoneNumber identifies one registered business phone number.
@dataclass(frozen=True, slots=True)
class BusinessPhoneNumber:
    # ID is the stable Meta phone-number ID for later messaging operations.
    id: str = ''
    # VerifiedName is the provider-verified business display name.
    verified_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'verifiedName'})
    # DisplayPhoneNumber is the provider-formatted business telephone number.
    display_phone_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayPhoneNumber'})


setattr(BusinessPhoneNumber, '__module__', 'dex_connectors.meta_whatsapp_business.models')


# PhoneNumbersPage contains one bounded Graph API page and its continuation cursor.
@dataclass(frozen=True, slots=True)
class PhoneNumbersPage:
    # Numbers are the visible business phone numbers on this page.
    numbers: tuple[BusinessPhoneNumber, ...] = ()
    # NextCursor is blank when Meta did not report a subsequent page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'numbers', _replace_nil_slices(self.numbers, 1))


PhoneNumbersPage.__annotations__['numbers'] = tuple[BusinessPhoneNumber, ...] | None
PhoneNumbersPage.__dataclass_fields__['numbers'].type = tuple[BusinessPhoneNumber, ...] | None
setattr(PhoneNumbersPage, '__module__', 'dex_connectors.meta_whatsapp_business.models')


# SendTextMessageInput selects a registered sender, recipient, and plain-text message.
@dataclass(frozen=True, slots=True)
class SendTextMessageInput:
    # PhoneNumberID is the numeric sender ID returned by listPhoneNumbers.
    phone_number_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'phoneNumberId'})
    # Recipient is the customer's E.164 phone number without the leading plus sign.
    recipient: str = ''
    # Text is nonempty UTF-8 text, sent only inside the customer service window.
    text: str = ''


setattr(SendTextMessageInput, '__module__', 'dex_connectors.meta_whatsapp_business.models')


# SentMessage identifies a WhatsApp message accepted by Meta.
@dataclass(frozen=True, slots=True)
class SentMessage:
    # MessageID is the provider's wamid identifier; acceptance does not imply delivery.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})


setattr(SentMessage, '__module__', 'dex_connectors.meta_whatsapp_business.models')


# SendTemplateMessageInput selects an approved template and optional text body parameters.
@dataclass(frozen=True, slots=True)
class SendTemplateMessageInput:
    # PhoneNumberID is the numeric sender ID returned by listPhoneNumbers.
    phone_number_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'phoneNumberId'})
    # Recipient is the customer's E.164 phone number without the leading plus sign.
    recipient: str = ''
    # TemplateName is the approved template name in WhatsApp Manager.
    template_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'templateName'})
    # LanguageCode is the approved template language code, such as en_US.
    language_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'languageCode'})
    # BodyParameters replace text placeholders in template body order.
    body_parameters: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'bodyParameters', 'dex_json_omit_default': True})


setattr(SendTemplateMessageInput, '__module__', 'dex_connectors.meta_whatsapp_business.models')
