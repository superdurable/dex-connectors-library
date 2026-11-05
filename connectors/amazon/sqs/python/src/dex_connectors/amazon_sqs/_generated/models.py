# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime

# ReceiveMessagesInput selects one queue and how long and how many messages to wait for.
@dataclass(frozen=True, slots=True)
class ReceiveMessagesInput:
    # QueueURL is the queue URL, such as https://sqs.eu-west-1.amazonaws.com/123456789012/orders; blank uses
    # the connection's defaultQueueUrl.
    queue_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'queueUrl', 'dex_json_omit_default': True})
    # MaxMessages is the most messages to receive, from 1 to 10; zero uses the connection's maxMessagesPerReceive.
    max_messages: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxMessages', 'dex_json_omit_default': True})
    # WaitTimeSeconds is how long to wait for a message to arrive, from 0 to 20 seconds; nil uses the
    # connection's receiveWaitSeconds, and 0 returns at once.
    wait_time_seconds: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'waitTimeSeconds', 'dex_json_omit_default': True})
    # VisibilityTimeoutSeconds hides each received message from other consumers for this many seconds, from 0
    # to 43200; nil uses the queue's own visibility timeout.
    visibility_timeout_seconds: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'visibilityTimeoutSeconds', 'dex_json_omit_default': True})


setattr(ReceiveMessagesInput, '__module__', 'dex_connectors.amazon_sqs.models')


# MessageAttributeValue is one custom message attribute.
@dataclass(frozen=True, slots=True)
class MessageAttributeValue:
    # DataType is String, Number, or Binary, optionally followed by a custom type such as Number.int.
    data_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'dataType'})
    # StringValue holds a String or Number value.
    string_value: str = _dataclass_field(default='', metadata={'dex_json_name': 'stringValue', 'dex_json_omit_default': True})
    # BinaryValue holds a Binary value, encoded as base64 in JSON.
    binary_value: str = _dataclass_field(default='', metadata={'dex_json_name': 'binaryValue', 'dex_json_omit_default': True})


setattr(MessageAttributeValue, '__module__', 'dex_connectors.amazon_sqs.models')


# Message is one received message. Pass ReceiptHandle to deleteMessage or changeMessageVisibility.
@dataclass(frozen=True, slots=True)
class Message:
    # MessageID is the ID SQS assigned when the message was sent; it stays the same on every delivery.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId'})
    # ReceiptHandle identifies this delivery of the message. Each receive returns a new handle, and only the
    # latest one is sure to delete the message or change its visibility.
    receipt_handle: str = _dataclass_field(default='', metadata={'dex_json_name': 'receiptHandle'})
    # Body is the message body text, verified against the MD5 digest SQS sent with it.
    body: str = ''
    # MD5OfBody is the hex MD5 digest of Body.
    md5_of_body: str = _dataclass_field(default='', metadata={'dex_json_name': 'md5OfBody'})
    # SentAt is when SQS accepted the message.
    sent_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'sentAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # FirstReceivedAt is when any consumer first received the message.
    first_received_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'firstReceivedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # ReceiveCount is how many times the message has been received, including this delivery. A count above 1
    # means an earlier delivery was not deleted, so processing must tolerate a repeat.
    receive_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'receiveCount'})
    # MessageGroupID is the FIFO message group; empty for a standard queue.
    message_group_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageGroupId', 'dex_json_omit_default': True})
    # MessageDeduplicationID is the FIFO deduplication ID; empty for a standard queue.
    message_deduplication_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageDeduplicationId', 'dex_json_omit_default': True})
    # SequenceNumber is the FIFO sequence number, a decimal string of up to 128 bits; empty for a standard queue.
    sequence_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'sequenceNumber', 'dex_json_omit_default': True})
    # MessageAttributes are the sender's custom message attributes, keyed by name.
    message_attributes: dict[str, MessageAttributeValue] = _dataclass_field(default_factory=dict, metadata={'dex_json_name': 'messageAttributes', 'dex_json_omit_default': True})


setattr(Message, '__module__', 'dex_connectors.amazon_sqs.models')


# ReceivedMessages is one batch of received messages, in the order SQS returned them.
@dataclass(frozen=True, slots=True)
class ReceivedMessages:
    # QueueURL is the queue the batch came from.
    queue_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'queueUrl'})
    # IsFIFO reports a FIFO queue, whose messages carry MessageGroupID and SequenceNumber.
    is_fifo: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isFifo', 'dex_json_omit_default': True})
    # Messages lists the received messages; it is empty only on the empty branch.
    messages: tuple[Message, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(ReceivedMessages, '__module__', 'dex_connectors.amazon_sqs.models')


# DeleteMessageInput acknowledges one received message.
@dataclass(frozen=True, slots=True)
class DeleteMessageInput:
    # QueueURL is the queue the message came from; blank uses the connection's defaultQueueUrl.
    queue_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'queueUrl', 'dex_json_omit_default': True})
    # ReceiptHandle is the handle of the message's latest delivery, from receiveMessages.
    receipt_handle: str = _dataclass_field(default='', metadata={'dex_json_name': 'receiptHandle'})
    # MessageID optionally names the message in the Result and receipt; it is not sent to SQS.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId', 'dex_json_omit_default': True})


setattr(DeleteMessageInput, '__module__', 'dex_connectors.amazon_sqs.models')


# DeletedMessage identifies the acknowledged message.
@dataclass(frozen=True, slots=True)
class DeletedMessage:
    # QueueURL is the message's queue.
    queue_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'queueUrl'})
    # MessageID is the input MessageID.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId', 'dex_json_omit_default': True})


setattr(DeletedMessage, '__module__', 'dex_connectors.amazon_sqs.models')


# ChangeMessageVisibilityInput releases one received message or hides it for longer.
@dataclass(frozen=True, slots=True)
class ChangeMessageVisibilityInput:
    # QueueURL is the queue the message came from; blank uses the connection's defaultQueueUrl.
    queue_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'queueUrl', 'dex_json_omit_default': True})
    # ReceiptHandle is the handle of the message's latest delivery, from receiveMessages.
    receipt_handle: str = _dataclass_field(default='', metadata={'dex_json_name': 'receiptHandle'})
    # MessageID optionally names the message in the Result and receipt; it is not sent to SQS.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId', 'dex_json_omit_default': True})
    # VisibilityTimeoutSeconds hides the message for this many seconds from now, from 0 to 43200. Zero
    # releases it to other consumers at once. SQS caps a message's total in-flight time at 12 hours from its
    # first receive.
    visibility_timeout_seconds: int = _dataclass_field(default=0, metadata={'dex_json_name': 'visibilityTimeoutSeconds'})


setattr(ChangeMessageVisibilityInput, '__module__', 'dex_connectors.amazon_sqs.models')


# ChangedMessageVisibility records the visibility timeout SQS accepted.
@dataclass(frozen=True, slots=True)
class ChangedMessageVisibility:
    # QueueURL is the message's queue.
    queue_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'queueUrl'})
    # MessageID is the input MessageID.
    message_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'messageId', 'dex_json_omit_default': True})
    # VisibilityTimeoutSeconds is the timeout SQS accepted.
    visibility_timeout_seconds: int = _dataclass_field(default=0, metadata={'dex_json_name': 'visibilityTimeoutSeconds'})
    # VisibleAfter is when the message becomes visible again, measured from the Worker clock when SQS answered.
    visible_after: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'visibleAfter', 'dex_json_format': 'rfc3339'})


setattr(ChangedMessageVisibility, '__module__', 'dex_connectors.amazon_sqs.models')
