# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable SQS consumer Flow: receive one batch, record it, and delete each message."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.amazon_sqs import (
    Connection,
    DeletedMessage,
    DeleteMessageInput,
    ReceivedMessages,
    ReceiveMessagesInput,
    new_delete_message_step,
    new_receive_messages_step,
)

FLOW_TYPE = "SQSQueueConsumer"
CONNECTION_NAME = "amazon-sqs-orders"
_RECEIVE_STEP_TYPE = "ReceiveOrderMessages"
_DELETE_STEP_TYPE = "DeleteOrderMessage"
_BATCH = dex.Attribute("sqs-received-batch", ReceivedMessages)
_DELETED_COUNT = dex.Attribute("sqs-deleted-count", int)


def map_to_receive(request: ReceiveMessagesInput) -> ReceiveMessagesInput:
    """Receive with the requested queue and bounds."""
    return request


def map_to_delete(request: DeleteMessageInput) -> DeleteMessageInput:
    """Delete the prepared message."""
    return request


# dex:group group-id:sqs group-label:"Amazon SQS"
# dex:explanation text:"Start by receiving one batch from the queue."
class _StartConsumer(dex.Step[ReceiveMessagesInput]):
    def get_step_type(self) -> str:
        return "StartQueueConsumer"

    def execute(self, context: dex.Context, input: ReceiveMessagesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ReceiveOrderMessages"), input)


# dex:group group-id:sqs group-label:"Amazon SQS"
# dex:explanation text:"Record the received batch and delete its messages one by one."
class _RecordBatch(dex.Step[MutationResult[ReceivedMessages]]):
    def get_step_type(self) -> str:
        return "RecordOrderBatch"

    def execute(self, context: dex.Context, input: MutationResult[ReceivedMessages]) -> dex.StepDecision:
        _BATCH.set(context, input.value)
        _DELETED_COUNT.set(context, 0)
        if not input.value.messages:
            return dex.graceful_complete(input.value)
        message = input.value.messages[0]
        return dex.go_to(
            dex.StepRef("DeleteOrderMessage"),
            DeleteMessageInput(input.value.queue_url, message.receipt_handle, message.message_id),
        )


# dex:group group-id:sqs group-label:"Amazon SQS"
# dex:explanation text:"Count the acknowledged message and delete the next one."
class _RecordDeletion(dex.Step[MutationResult[DeletedMessage]]):
    def get_step_type(self) -> str:
        return "RecordOrderDeletion"

    def execute(self, context: dex.Context, input: MutationResult[DeletedMessage]) -> dex.StepDecision:
        _DELETED_COUNT.set(context, _DELETED_COUNT.get(context) + 1)
        batch = _BATCH.get(context)
        deleted_count = _DELETED_COUNT.get(context)
        if deleted_count >= len(batch.messages):
            return dex.graceful_complete(batch)
        message = batch.messages[deleted_count]
        return dex.go_to(
            dex.StepRef("DeleteOrderMessage"),
            DeleteMessageInput(batch.queue_url, message.receipt_handle, message.message_id),
        )


# dex:group group-id:sqs group-label:"Amazon SQS"
# dex:explanation text:"Complete without work when no message arrived."
class _CompleteEmpty(dex.Step[MutationResult[ReceivedMessages]]):
    def get_step_type(self) -> str:
        return "CompleteEmptyReceive"

    def execute(self, context: dex.Context, input: MutationResult[ReceivedMessages]) -> dex.StepDecision:
        _BATCH.set(context, input.value)
        return dex.graceful_complete(input.value)


class QueueConsumerFlow(dex.Flow[ReceiveMessagesInput]):
    """Receive and acknowledge one batch through the static amazon-sqs-orders connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("SQS queue consumer requires the static amazon-sqs-orders connection")
        self._start = _StartConsumer()
        self._record_batch = _RecordBatch()
        self._record_deletion = _RecordDeletion()
        self._complete_empty = _CompleteEmpty()
        # dex:group group-id:sqs group-label:"Amazon SQS"
        # dex:explanation text:"Long-poll the queue for one batch."
        self._receive = new_receive_messages_step(
            step_type=_RECEIVE_STEP_TYPE,
            step_input_type=ReceiveMessagesInput,
            annotations={
                "groupId": "sqs",
                "groupLabel": "Amazon SQS",
                "explanation": "Long-poll the queue for one batch.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_receive,
            received=self._record_batch,
            # Business reaction: an empty long poll is a normal, successful run.
            empty=self._complete_empty,
        )
        # dex:group group-id:sqs group-label:"Amazon SQS"
        # dex:explanation text:"Acknowledge one message by deleting it."
        self._delete = new_delete_message_step(
            step_type=_DELETE_STEP_TYPE,
            step_input_type=DeleteMessageInput,
            annotations={
                "groupId": "sqs",
                "groupLabel": "Amazon SQS",
                "explanation": "Acknowledge one message by deleting it.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_delete,
            deleted=self._record_deletion,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "SQSQueueConsumer"

    def get_steps(self) -> dex.StepList[ReceiveMessagesInput]:
        """Register the receive, record, delete, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(
            self._receive, self._record_batch, self._delete, self._record_deletion, self._complete_empty
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the received batch and how many of its messages were deleted."""
        return dex.PersistenceSchema.of(_BATCH, _DELETED_COUNT)
