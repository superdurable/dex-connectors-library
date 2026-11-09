# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import BusinessPhoneNumber, ListPhoneNumbersInput, PhoneNumbersPage, SendTemplateMessageInput, SendTextMessageInput, SentMessage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_PHONE_NUMBERS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, SEND_TEMPLATE_MESSAGE_DEFINITION, SEND_TEXT_MESSAGE_DEFINITION, SENT, UNCERTAIN

def list_phone_numbers_listed(value: PhoneNumbersPage, failure: Failure | None = None) -> Case[PhoneNumbersPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_phone_numbers_listed, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def list_phone_numbers_not_found(value: PhoneNumbersPage, failure: Failure | None = None) -> Case[PhoneNumbersPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_phone_numbers_not_found, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def list_phone_numbers_provider_rejected(value: PhoneNumbersPage, failure: Failure | None = None) -> Case[PhoneNumbersPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_phone_numbers_provider_rejected, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def list_phone_numbers_invalid_response(value: PhoneNumbersPage, failure: Failure | None = None) -> Case[PhoneNumbersPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_phone_numbers_invalid_response, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def list_phone_numbers_defect(value: PhoneNumbersPage, failure: Failure | None = None) -> Case[PhoneNumbersPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_phone_numbers_defect, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def list_phone_numbers_retry(failure: Failure, after_seconds: float = 0) -> Case[PhoneNumbersPage]:
    return Case.retry(failure, after_seconds)

setattr(list_phone_numbers_retry, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def list_phone_numbers_listed_default() -> Case[PhoneNumbersPage]:
    return Case.branch_result(LISTED, PhoneNumbersPage(numbers=(BusinessPhoneNumber(id='1906385232743451', verified_name='Example Store', display_phone_number='+1 631-555-5555'),), next_cursor=''))

setattr(list_phone_numbers_listed_default, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def send_text_message_sent(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(SENT, value, failure)

setattr(send_text_message_sent, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def send_text_message_provider_rejected(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(send_text_message_provider_rejected, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def send_text_message_uncertain(value: SentMessage, failure: Failure) -> Case[SentMessage]:
    return Case.uncertain(value, failure)

setattr(send_text_message_uncertain, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def send_text_message_defect(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(send_text_message_defect, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def send_text_message_retry(failure: Failure, after_seconds: float = 0) -> Case[SentMessage]:
    return Case.retry(failure, after_seconds)

setattr(send_text_message_retry, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def send_text_message_sent_default() -> Case[SentMessage]:
    return Case.branch_result(SENT, SentMessage(message_id='wamid.example-text-message'))

setattr(send_text_message_sent_default, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def send_template_message_sent(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(SENT, value, failure)

setattr(send_template_message_sent, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def send_template_message_provider_rejected(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(send_template_message_provider_rejected, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def send_template_message_uncertain(value: SentMessage, failure: Failure) -> Case[SentMessage]:
    return Case.uncertain(value, failure)

setattr(send_template_message_uncertain, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def send_template_message_defect(value: SentMessage, failure: Failure | None = None) -> Case[SentMessage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(send_template_message_defect, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def send_template_message_retry(failure: Failure, after_seconds: float = 0) -> Case[SentMessage]:
    return Case.retry(failure, after_seconds)

setattr(send_template_message_retry, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

def send_template_message_sent_default() -> Case[SentMessage]:
    return Case.branch_result(SENT, SentMessage(message_id='wamid.example-template-message'))

setattr(send_template_message_sent_default, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")


class Mock:

    def __init__(self, connection_name: str = "whatsapp-business") -> None:
        self.list_phone_numbers_script: Query[ListPhoneNumbersInput, PhoneNumbersPage] = Query(LIST_PHONE_NUMBERS_DEFINITION)
        self.list_phone_numbers_script.default(list_phone_numbers_listed_default())
        self.send_text_message_script: Mutation[SendTextMessageInput, SentMessage] = Mutation(SEND_TEXT_MESSAGE_DEFINITION)
        self.send_text_message_script.default(send_text_message_sent_default())
        self.send_template_message_script: Mutation[SendTemplateMessageInput, SentMessage] = Mutation(SEND_TEMPLATE_MESSAGE_DEFINITION)
        self.send_template_message_script.default(send_template_message_sent_default())
        self._connection = new_connection_with_operations(
            list_phone_numbers=self.list_phone_numbers_script,
            send_text_message=self.send_text_message_script,
            send_template_message=self.send_template_message_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.meta_whatsapp_business.whatsappbusinessmock")

__all__ = [
    "Mock",
    "list_phone_numbers_listed",
    "list_phone_numbers_not_found",
    "list_phone_numbers_provider_rejected",
    "list_phone_numbers_invalid_response",
    "list_phone_numbers_defect",
    "list_phone_numbers_retry",
    "list_phone_numbers_listed_default",
    "send_text_message_sent",
    "send_text_message_provider_rejected",
    "send_text_message_uncertain",
    "send_text_message_defect",
    "send_text_message_retry",
    "send_text_message_sent_default",
    "send_template_message_sent",
    "send_template_message_provider_rejected",
    "send_template_message_uncertain",
    "send_template_message_defect",
    "send_template_message_retry",
    "send_template_message_sent_default",
]
