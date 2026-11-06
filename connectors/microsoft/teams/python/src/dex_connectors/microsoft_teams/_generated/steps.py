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
    QueryResult,
    QueryStep,
    QueryStepConfig,
    StepAnnotations,
)

from ..connection import Connection
from ..models import ListThreadRepliesInput, ListThreadRepliesOutput, PostChannelMessageInput, PostChatMessageInput, PostMessageOutput, PostThreadReplyInput

StepInput = TypeVar("StepInput")

def new_list_thread_replies_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListThreadRepliesInput],
    read: dex.StepTarget[QueryResult[ListThreadRepliesOutput]],
    not_found: dex.StepTarget[QueryResult[ListThreadRepliesOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ListThreadRepliesOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ListThreadRepliesOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[ListThreadRepliesOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ListThreadRepliesOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListThreadRepliesInput, ListThreadRepliesOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Teams Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Teams Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Teams Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "read": read,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "list_thread_replies"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_thread_replies_step, '__module__', 'dex_connectors.microsoft_teams.steps')

def new_post_channel_message_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], PostChannelMessageInput],
    sent: dex.StepTarget[MutationResult[PostMessageOutput]],
    provider_rejected: dex.StepTarget[MutationResult[PostMessageOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[PostMessageOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[PostMessageOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[PostMessageOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, PostChannelMessageInput, PostMessageOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Teams Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Teams Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Teams Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "sent": sent,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "post_channel_message"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_post_channel_message_step, '__module__', 'dex_connectors.microsoft_teams.steps')

def new_post_thread_reply_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], PostThreadReplyInput],
    sent: dex.StepTarget[MutationResult[PostMessageOutput]],
    provider_rejected: dex.StepTarget[MutationResult[PostMessageOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[PostMessageOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[PostMessageOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[PostMessageOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, PostThreadReplyInput, PostMessageOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Teams Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Teams Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Teams Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "sent": sent,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "post_thread_reply"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_post_thread_reply_step, '__module__', 'dex_connectors.microsoft_teams.steps')

def new_post_chat_message_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], PostChatMessageInput],
    sent: dex.StepTarget[MutationResult[PostMessageOutput]],
    provider_rejected: dex.StepTarget[MutationResult[PostMessageOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[PostMessageOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[PostMessageOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[PostMessageOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, PostChatMessageInput, PostMessageOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Teams Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Teams Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Teams Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "sent": sent,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "post_chat_message"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_post_chat_message_step, '__module__', 'dex_connectors.microsoft_teams.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
