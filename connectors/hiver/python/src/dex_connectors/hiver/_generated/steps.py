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
from ..models import AddNoteInput, AddNoteOutput, ConversationDetails, CreateSharedDraftInput, GetConversationInput, ListConversationsInput, ListConversationsOutput, ListInboxesInput, ListInboxesOutput, SharedDraft, UpdateConversationInput, UpdateConversationOutput

StepInput = TypeVar("StepInput")

def new_list_inboxes_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListInboxesInput],
    listed: dex.StepTarget[QueryResult[ListInboxesOutput]],
    provider_rejected: dex.StepTarget[QueryResult[ListInboxesOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ListInboxesOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[ListInboxesOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ListInboxesOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListInboxesInput, ListInboxesOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Hiver Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Hiver Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Hiver Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
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
            operation=_bound_operation(connection, "list_inboxes"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_inboxes_step, '__module__', 'dex_connectors.hiver.steps')

def new_list_conversations_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListConversationsInput],
    listed: dex.StepTarget[QueryResult[ListConversationsOutput]],
    not_found: dex.StepTarget[QueryResult[ListConversationsOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ListConversationsOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ListConversationsOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[ListConversationsOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ListConversationsOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListConversationsInput, ListConversationsOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Hiver Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Hiver Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Hiver Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
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
            operation=_bound_operation(connection, "list_conversations"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_conversations_step, '__module__', 'dex_connectors.hiver.steps')

def new_get_conversation_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetConversationInput],
    found: dex.StepTarget[QueryResult[ConversationDetails]],
    not_found: dex.StepTarget[QueryResult[ConversationDetails]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ConversationDetails]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ConversationDetails]] | None = None,
    defect: dex.StepTarget[QueryResult[ConversationDetails]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ConversationDetails]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetConversationInput, ConversationDetails]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Hiver Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Hiver Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Hiver Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_conversation"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_conversation_step, '__module__', 'dex_connectors.hiver.steps')

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
        raise ValueError("Hiver Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Hiver Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Hiver Step annotations must be strings")
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

setattr(new_update_conversation_step, '__module__', 'dex_connectors.hiver.steps')

def new_add_note_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], AddNoteInput],
    added: dex.StepTarget[MutationResult[AddNoteOutput]],
    not_found: dex.StepTarget[MutationResult[AddNoteOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[AddNoteOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[AddNoteOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[AddNoteOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[AddNoteOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, AddNoteInput, AddNoteOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Hiver Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Hiver Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Hiver Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "added": added,
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
            operation=_bound_operation(connection, "add_note"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_add_note_step, '__module__', 'dex_connectors.hiver.steps')

def new_create_shared_draft_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateSharedDraftInput],
    created: dex.StepTarget[MutationResult[SharedDraft]],
    not_found: dex.StepTarget[MutationResult[SharedDraft]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[SharedDraft]] | None = None,
    uncertain: dex.StepTarget[MutationResult[SharedDraft]] | None = None,
    defect: dex.StepTarget[MutationResult[SharedDraft]] | None = None,
    result_attribute: dex.Attribute[MutationResult[SharedDraft]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateSharedDraftInput, SharedDraft]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Hiver Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Hiver Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Hiver Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "created": created,
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
            operation=_bound_operation(connection, "create_shared_draft"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_shared_draft_step, '__module__', 'dex_connectors.hiver.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
