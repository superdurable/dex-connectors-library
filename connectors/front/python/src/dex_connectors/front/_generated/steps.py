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
from ..models import ConversationDetails, ConversationReply, FindContactByEmailInput, FindContactByEmailOutput, GetConversationInput, ReplyToConversationInput, SearchConversationsInput, SearchConversationsOutput, UpdateConversationInput, UpdateConversationOutput

StepInput = TypeVar("StepInput")

def new_search_conversations_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SearchConversationsInput],
    searched: dex.StepTarget[QueryResult[SearchConversationsOutput]],
    provider_rejected: dex.StepTarget[QueryResult[SearchConversationsOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[SearchConversationsOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[SearchConversationsOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[SearchConversationsOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, SearchConversationsInput, SearchConversationsOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Front Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Front Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Front Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "searched": searched,
    }
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
            operation=_bound_operation(connection, "search_conversations"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_search_conversations_step, '__module__', 'dex_connectors.front.steps')

def new_get_conversation_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetConversationInput],
    found: dex.StepTarget[QueryResult[ConversationDetails]],
    merged: dex.StepTarget[QueryResult[ConversationDetails]] | None = None,
    not_found: dex.StepTarget[QueryResult[ConversationDetails]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ConversationDetails]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ConversationDetails]] | None = None,
    defect: dex.StepTarget[QueryResult[ConversationDetails]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ConversationDetails]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetConversationInput, ConversationDetails]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Front Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Front Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Front Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "found": found,
    }
    if merged is not None:
        branches["merged"] = merged
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
            operation=_bound_operation(connection, "get_conversation"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_conversation_step, '__module__', 'dex_connectors.front.steps')

def new_reply_to_conversation_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ReplyToConversationInput],
    replied: dex.StepTarget[MutationResult[ConversationReply]],
    not_found: dex.StepTarget[MutationResult[ConversationReply]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[ConversationReply]] | None = None,
    uncertain: dex.StepTarget[MutationResult[ConversationReply]] | None = None,
    defect: dex.StepTarget[MutationResult[ConversationReply]] | None = None,
    result_attribute: dex.Attribute[MutationResult[ConversationReply]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, ReplyToConversationInput, ConversationReply]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Front Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Front Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Front Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "replied": replied,
    }
    if not_found is not None:
        branches["notFound"] = not_found
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
            operation=_bound_operation(connection, "reply_to_conversation"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_reply_to_conversation_step, '__module__', 'dex_connectors.front.steps')

def new_update_conversation_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdateConversationInput],
    updated: dex.StepTarget[MutationResult[UpdateConversationOutput]],
    not_found: dex.StepTarget[MutationResult[UpdateConversationOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpdateConversationOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UpdateConversationOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpdateConversationOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpdateConversationOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdateConversationInput, UpdateConversationOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Front Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Front Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Front Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "updated": updated,
    }
    if not_found is not None:
        branches["notFound"] = not_found
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
            operation=_bound_operation(connection, "update_conversation"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_update_conversation_step, '__module__', 'dex_connectors.front.steps')

def new_find_contact_by_email_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], FindContactByEmailInput],
    found: dex.StepTarget[QueryResult[FindContactByEmailOutput]],
    not_found: dex.StepTarget[QueryResult[FindContactByEmailOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[FindContactByEmailOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[FindContactByEmailOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[FindContactByEmailOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[FindContactByEmailOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, FindContactByEmailInput, FindContactByEmailOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Front Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Front Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Front Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "found": found,
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
            operation=_bound_operation(connection, "find_contact_by_email"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_find_contact_by_email_step, '__module__', 'dex_connectors.front.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
