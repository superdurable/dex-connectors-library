# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from collections.abc import Callable, Mapping
from typing import Any, TypeVar

import dex
from dex_connectors.sdk import (
    MutationResult,
    MutationStep,
    MutationStepConfig,
    StepAnnotations,
)

from ..connection import Connection
from ..models import ChangeMessageVisibilityInput, ChangedMessageVisibility, DeleteMessageInput, DeletedMessage, ReceiveMessagesInput, ReceivedMessages

StepInput = TypeVar("StepInput")

def new_receive_messages_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ReceiveMessagesInput],
    received: dex.StepTarget[MutationResult[ReceivedMessages]],
    empty: dex.StepTarget[MutationResult[ReceivedMessages]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[ReceivedMessages]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[ReceivedMessages]] | None = None,
    defect: dex.StepTarget[MutationResult[ReceivedMessages]] | None = None,
    result_attribute: dex.Attribute[MutationResult[ReceivedMessages]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, ReceiveMessagesInput, ReceivedMessages]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Amazon SQS Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Amazon SQS Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Amazon SQS Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "received": received,
    }
    if empty is not None:
        branches["empty"] = empty
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "receive_messages"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_receive_messages_step, '__module__', 'dex_connectors.amazon_sqs.steps')

def new_delete_message_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], DeleteMessageInput],
    deleted: dex.StepTarget[MutationResult[DeletedMessage]],
    receipt_handle_invalid: dex.StepTarget[MutationResult[DeletedMessage]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[DeletedMessage]] | None = None,
    defect: dex.StepTarget[MutationResult[DeletedMessage]] | None = None,
    result_attribute: dex.Attribute[MutationResult[DeletedMessage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, DeleteMessageInput, DeletedMessage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Amazon SQS Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Amazon SQS Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Amazon SQS Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "deleted": deleted,
    }
    if receipt_handle_invalid is not None:
        branches["receiptHandleInvalid"] = receipt_handle_invalid
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "delete_message"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_delete_message_step, '__module__', 'dex_connectors.amazon_sqs.steps')

def new_change_message_visibility_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ChangeMessageVisibilityInput],
    changed: dex.StepTarget[MutationResult[ChangedMessageVisibility]],
    receipt_handle_invalid: dex.StepTarget[MutationResult[ChangedMessageVisibility]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[ChangedMessageVisibility]] | None = None,
    defect: dex.StepTarget[MutationResult[ChangedMessageVisibility]] | None = None,
    result_attribute: dex.Attribute[MutationResult[ChangedMessageVisibility]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, ChangeMessageVisibilityInput, ChangedMessageVisibility]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Amazon SQS Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Amazon SQS Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Amazon SQS Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "changed": changed,
    }
    if receipt_handle_invalid is not None:
        branches["receiptHandleInvalid"] = receipt_handle_invalid
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "change_message_visibility"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_change_message_visibility_step, '__module__', 'dex_connectors.amazon_sqs.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
