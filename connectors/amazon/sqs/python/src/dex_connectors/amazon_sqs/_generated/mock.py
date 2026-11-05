# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation

from ..connection import Connection, new_connection_with_operations
from ..models import ChangeMessageVisibilityInput, ChangedMessageVisibility, DeleteMessageInput, DeletedMessage, Message, MessageAttributeValue, ReceiveMessagesInput, ReceivedMessages
from .definitions import CONNECTOR_ID, CHANGED, CHANGE_MESSAGE_VISIBILITY_DEFINITION, DEFECT, DELETED, DELETE_MESSAGE_DEFINITION, EMPTY, INVALID_RESPONSE, PROVIDER_REJECTED, RECEIPT_HANDLE_INVALID, RECEIVED, RECEIVE_MESSAGES_DEFINITION

def receive_messages_received(value: ReceivedMessages, failure: Failure | None = None) -> Case[ReceivedMessages]:
    return Case.branch_result(RECEIVED, value, failure)

setattr(receive_messages_received, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def receive_messages_empty(value: ReceivedMessages, failure: Failure | None = None) -> Case[ReceivedMessages]:
    return Case.branch_result(EMPTY, value, failure)

setattr(receive_messages_empty, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def receive_messages_provider_rejected(value: ReceivedMessages, failure: Failure | None = None) -> Case[ReceivedMessages]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(receive_messages_provider_rejected, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def receive_messages_invalid_response(value: ReceivedMessages, failure: Failure | None = None) -> Case[ReceivedMessages]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(receive_messages_invalid_response, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def receive_messages_defect(value: ReceivedMessages, failure: Failure | None = None) -> Case[ReceivedMessages]:
    return Case.branch_result(DEFECT, value, failure)

setattr(receive_messages_defect, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def receive_messages_retry(failure: Failure, after_seconds: float = 0) -> Case[ReceivedMessages]:
    return Case.retry(failure, after_seconds)

setattr(receive_messages_retry, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def receive_messages_paid_order() -> Case[ReceivedMessages]:
    return Case.branch_result(RECEIVED, ReceivedMessages(queue_url='https://sqs.eu-west-1.amazonaws.com/123456789012/orders', messages=(Message(message_id='order-1042', receipt_handle='receipt-order-1042', body='{"orderId":"1042","event":"paid"}', md5_of_body='457ba27172a5858e4fed7600ec948c39', sent_at=datetime.fromisoformat("2026-10-25T09:00:00Z".replace('Z', '+00:00')), first_received_at=datetime.fromisoformat("2026-10-25T09:00:01Z".replace('Z', '+00:00')), receive_count=1),)))

setattr(receive_messages_paid_order, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def receive_messages_no_order() -> Case[ReceivedMessages]:
    return Case.branch_result(EMPTY, ReceivedMessages(queue_url='https://sqs.eu-west-1.amazonaws.com/123456789012/orders', messages=()))

setattr(receive_messages_no_order, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def receive_messages_receive_denied() -> Case[ReceivedMessages]:
    return Case.branch_result(PROVIDER_REJECTED, ReceivedMessages(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "receiveMessages", "SQS denied the receive because the connection lacks permission."))

setattr(receive_messages_receive_denied, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def delete_message_deleted(value: DeletedMessage, failure: Failure | None = None) -> Case[DeletedMessage]:
    return Case.branch_result(DELETED, value, failure)

setattr(delete_message_deleted, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def delete_message_receipt_handle_invalid(value: DeletedMessage, failure: Failure | None = None) -> Case[DeletedMessage]:
    return Case.branch_result(RECEIPT_HANDLE_INVALID, value, failure)

setattr(delete_message_receipt_handle_invalid, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def delete_message_provider_rejected(value: DeletedMessage, failure: Failure | None = None) -> Case[DeletedMessage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(delete_message_provider_rejected, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def delete_message_defect(value: DeletedMessage, failure: Failure | None = None) -> Case[DeletedMessage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(delete_message_defect, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def delete_message_retry(failure: Failure, after_seconds: float = 0) -> Case[DeletedMessage]:
    return Case.retry(failure, after_seconds)

setattr(delete_message_retry, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def delete_message_acknowledged_order() -> Case[DeletedMessage]:
    return Case.branch_result(DELETED, DeletedMessage(queue_url='https://sqs.eu-west-1.amazonaws.com/123456789012/orders', message_id='order-1042'))

setattr(delete_message_acknowledged_order, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def delete_message_expired_receipt() -> Case[DeletedMessage]:
    return Case.branch_result(RECEIPT_HANDLE_INVALID, DeletedMessage(queue_url='https://sqs.eu-west-1.amazonaws.com/123456789012/orders.fifo', message_id='order-1042'), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "deleteMessage", "SQS no longer accepts the receipt handle."))

setattr(delete_message_expired_receipt, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def delete_message_delete_denied() -> Case[DeletedMessage]:
    return Case.branch_result(PROVIDER_REJECTED, DeletedMessage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "deleteMessage", "SQS denied the deletion because the connection lacks permission."))

setattr(delete_message_delete_denied, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def change_message_visibility_changed(value: ChangedMessageVisibility, failure: Failure | None = None) -> Case[ChangedMessageVisibility]:
    return Case.branch_result(CHANGED, value, failure)

setattr(change_message_visibility_changed, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def change_message_visibility_receipt_handle_invalid(value: ChangedMessageVisibility, failure: Failure | None = None) -> Case[ChangedMessageVisibility]:
    return Case.branch_result(RECEIPT_HANDLE_INVALID, value, failure)

setattr(change_message_visibility_receipt_handle_invalid, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def change_message_visibility_provider_rejected(value: ChangedMessageVisibility, failure: Failure | None = None) -> Case[ChangedMessageVisibility]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(change_message_visibility_provider_rejected, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def change_message_visibility_defect(value: ChangedMessageVisibility, failure: Failure | None = None) -> Case[ChangedMessageVisibility]:
    return Case.branch_result(DEFECT, value, failure)

setattr(change_message_visibility_defect, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def change_message_visibility_retry(failure: Failure, after_seconds: float = 0) -> Case[ChangedMessageVisibility]:
    return Case.retry(failure, after_seconds)

setattr(change_message_visibility_retry, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def change_message_visibility_released_order() -> Case[ChangedMessageVisibility]:
    return Case.branch_result(CHANGED, ChangedMessageVisibility(queue_url='https://sqs.eu-west-1.amazonaws.com/123456789012/orders', message_id='order-1042', visibility_timeout_seconds=0, visible_after=datetime.fromisoformat("2026-10-25T09:00:02Z".replace('Z', '+00:00'))))

setattr(change_message_visibility_released_order, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def change_message_visibility_expired_visibility_receipt() -> Case[ChangedMessageVisibility]:
    return Case.branch_result(RECEIPT_HANDLE_INVALID, ChangedMessageVisibility(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "changeMessageVisibility", "SQS no longer accepts the receipt handle."))

setattr(change_message_visibility_expired_visibility_receipt, '__module__', "dex_connectors.amazon_sqs.sqsmock")

def change_message_visibility_visibility_denied() -> Case[ChangedMessageVisibility]:
    return Case.branch_result(PROVIDER_REJECTED, ChangedMessageVisibility(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "changeMessageVisibility", "SQS denied the visibility change because the connection lacks permission."))

setattr(change_message_visibility_visibility_denied, '__module__', "dex_connectors.amazon_sqs.sqsmock")


class Mock:

    def __init__(self, connection_name: str = "amazon-sqs") -> None:
        self.receive_messages_script: Mutation[ReceiveMessagesInput, ReceivedMessages] = Mutation(RECEIVE_MESSAGES_DEFINITION)
        self.receive_messages_script.default(receive_messages_paid_order())
        self.delete_message_script: Mutation[DeleteMessageInput, DeletedMessage] = Mutation(DELETE_MESSAGE_DEFINITION)
        self.delete_message_script.default(delete_message_acknowledged_order())
        self.change_message_visibility_script: Mutation[ChangeMessageVisibilityInput, ChangedMessageVisibility] = Mutation(CHANGE_MESSAGE_VISIBILITY_DEFINITION)
        self.change_message_visibility_script.default(change_message_visibility_released_order())
        self._connection = new_connection_with_operations(
            receive_messages=self.receive_messages_script,
            delete_message=self.delete_message_script,
            change_message_visibility=self.change_message_visibility_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.amazon_sqs.sqsmock")

__all__ = [
    "Mock",
    "receive_messages_received",
    "receive_messages_empty",
    "receive_messages_provider_rejected",
    "receive_messages_invalid_response",
    "receive_messages_defect",
    "receive_messages_retry",
    "receive_messages_paid_order",
    "receive_messages_no_order",
    "receive_messages_receive_denied",
    "delete_message_deleted",
    "delete_message_receipt_handle_invalid",
    "delete_message_provider_rejected",
    "delete_message_defect",
    "delete_message_retry",
    "delete_message_acknowledged_order",
    "delete_message_expired_receipt",
    "delete_message_delete_denied",
    "change_message_visibility_changed",
    "change_message_visibility_receipt_handle_invalid",
    "change_message_visibility_provider_rejected",
    "change_message_visibility_defect",
    "change_message_visibility_retry",
    "change_message_visibility_released_order",
    "change_message_visibility_expired_visibility_receipt",
    "change_message_visibility_visibility_denied",
]
