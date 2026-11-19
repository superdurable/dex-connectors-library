# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetMessageInput, Message, SendMessageInput
from .definitions import CONNECTOR_ID, ACCEPTED, DEFECT, FOUND, GET_MESSAGE_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, SEND_MESSAGE_DEFINITION, UNCERTAIN

def send_message_accepted(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(ACCEPTED, value, failure)

setattr(send_message_accepted, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def send_message_provider_rejected(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(send_message_provider_rejected, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def send_message_uncertain(value: Message, failure: Failure) -> Case[Message]:
    return Case.uncertain(value, failure)

setattr(send_message_uncertain, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def send_message_defect(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(DEFECT, value, failure)

setattr(send_message_defect, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def send_message_retry(failure: Failure, after_seconds: float = 0) -> Case[Message]:
    return Case.retry(failure, after_seconds)

setattr(send_message_retry, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def send_message_queued_text() -> Case[Message]:
    return Case.branch_result(ACCEPTED, Message(sid='SMfedcba9876543210fedcba9876543210', account_sid='ACmockAccount', from_='+14155550100', to='+14155550123', status='queued', direction='outbound-api', segment_count=1, created_at=datetime.fromisoformat("2026-09-30T17:04:05Z".replace('Z', '+00:00'))))

setattr(send_message_queued_text, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def send_message_trial_recipient_rejected() -> Case[Message]:
    return Case.branch_result(PROVIDER_REJECTED, Message(account_sid='ACmockAccount', from_='+14155550100', to='+14155550123', error_code=21608), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "sendMessage", "Twilio rejected the message with error 21608"))

setattr(send_message_trial_recipient_rejected, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def get_message_found(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_message_found, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def get_message_not_found(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_message_not_found, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def get_message_provider_rejected(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_message_provider_rejected, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def get_message_invalid_response(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_message_invalid_response, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def get_message_defect(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_message_defect, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def get_message_retry(failure: Failure, after_seconds: float = 0) -> Case[Message]:
    return Case.retry(failure, after_seconds)

setattr(get_message_retry, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def get_message_delivered_text() -> Case[Message]:
    return Case.branch_result(FOUND, Message(sid='SMfedcba9876543210fedcba9876543210', account_sid='ACmockAccount', from_='+14155550100', to='+14155550123', status='delivered', direction='outbound-api', segment_count=1, created_at=datetime.fromisoformat("2026-09-30T17:04:05Z".replace('Z', '+00:00')), sent_at=datetime.fromisoformat("2026-09-30T17:04:06Z".replace('Z', '+00:00'))))

setattr(get_message_delivered_text, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def get_message_message_missing() -> Case[Message]:
    return Case.branch_result(NOT_FOUND, Message(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getMessage", "Twilio has no message with that SID."))

setattr(get_message_message_missing, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def get_message_message_read_rejected() -> Case[Message]:
    return Case.branch_result(PROVIDER_REJECTED, Message(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getMessage", "Twilio rejected the message query."))

setattr(get_message_message_read_rejected, '__module__', "dex_connectors.twilio_messaging.messagingmock")

def get_message_invalid_message_response() -> Case[Message]:
    return Case.branch_result(INVALID_RESPONSE, Message(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getMessage", "Twilio returned an invalid message."))

setattr(get_message_invalid_message_response, '__module__', "dex_connectors.twilio_messaging.messagingmock")


class Mock:

    def __init__(self, connection_name: str = "twilio-messaging") -> None:
        self.send_message_script: Mutation[SendMessageInput, Message] = Mutation(SEND_MESSAGE_DEFINITION)
        self.send_message_script.default(send_message_queued_text())
        self.get_message_script: Query[GetMessageInput, Message] = Query(GET_MESSAGE_DEFINITION)
        self.get_message_script.default(get_message_delivered_text())
        self._connection = new_connection_with_operations(
            send_message=self.send_message_script,
            get_message=self.get_message_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.twilio_messaging.messagingmock")

__all__ = [
    "Mock",
    "send_message_accepted",
    "send_message_provider_rejected",
    "send_message_uncertain",
    "send_message_defect",
    "send_message_retry",
    "send_message_queued_text",
    "send_message_trial_recipient_rejected",
    "get_message_found",
    "get_message_not_found",
    "get_message_provider_rejected",
    "get_message_invalid_response",
    "get_message_defect",
    "get_message_retry",
    "get_message_delivered_text",
    "get_message_message_missing",
    "get_message_message_read_rejected",
    "get_message_invalid_message_response",
]
