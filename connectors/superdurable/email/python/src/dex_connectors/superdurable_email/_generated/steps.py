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
from ..models import GetMessageInput, Message, MessageFlags, MoveMessageInput, MovedMessage, ReplyToMessageInput, SearchMessagesInput, SearchMessagesOutput, SendMessageInput, SentMessage, SetFlagsInput

StepInput = TypeVar("StepInput")

def new_search_messages_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SearchMessagesInput],
    searched: dex.StepTarget[QueryResult[SearchMessagesOutput]],
    provider_rejected: dex.StepTarget[QueryResult[SearchMessagesOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[SearchMessagesOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[SearchMessagesOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[SearchMessagesOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, SearchMessagesInput, SearchMessagesOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Email (IMAP and SMTP) Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Email (IMAP and SMTP) Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Email (IMAP and SMTP) Step annotations must be strings")
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
            operation=_bound_operation(connection, "search_messages"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_search_messages_step, '__module__', 'dex_connectors.superdurable_email.steps')

def new_get_message_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetMessageInput],
    found: dex.StepTarget[QueryResult[Message]],
    not_found: dex.StepTarget[QueryResult[Message]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Message]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Message]] | None = None,
    defect: dex.StepTarget[QueryResult[Message]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Message]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetMessageInput, Message]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Email (IMAP and SMTP) Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Email (IMAP and SMTP) Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Email (IMAP and SMTP) Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_message"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_message_step, '__module__', 'dex_connectors.superdurable_email.steps')

def new_send_message_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SendMessageInput],
    sent: dex.StepTarget[MutationResult[SentMessage]],
    provider_rejected: dex.StepTarget[MutationResult[SentMessage]] | None = None,
    uncertain: dex.StepTarget[MutationResult[SentMessage]] | None = None,
    defect: dex.StepTarget[MutationResult[SentMessage]] | None = None,
    result_attribute: dex.Attribute[MutationResult[SentMessage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, SendMessageInput, SentMessage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Email (IMAP and SMTP) Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Email (IMAP and SMTP) Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Email (IMAP and SMTP) Step annotations must be strings")
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
            operation=_bound_operation(connection, "send_message"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_send_message_step, '__module__', 'dex_connectors.superdurable_email.steps')

def new_reply_to_message_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ReplyToMessageInput],
    sent: dex.StepTarget[MutationResult[SentMessage]],
    not_found: dex.StepTarget[MutationResult[SentMessage]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[SentMessage]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[SentMessage]] | None = None,
    uncertain: dex.StepTarget[MutationResult[SentMessage]] | None = None,
    defect: dex.StepTarget[MutationResult[SentMessage]] | None = None,
    result_attribute: dex.Attribute[MutationResult[SentMessage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, ReplyToMessageInput, SentMessage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Email (IMAP and SMTP) Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Email (IMAP and SMTP) Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Email (IMAP and SMTP) Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "sent": sent,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "reply_to_message"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_reply_to_message_step, '__module__', 'dex_connectors.superdurable_email.steps')

def new_move_message_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], MoveMessageInput],
    moved: dex.StepTarget[MutationResult[MovedMessage]],
    not_found: dex.StepTarget[MutationResult[MovedMessage]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[MovedMessage]] | None = None,
    defect: dex.StepTarget[MutationResult[MovedMessage]] | None = None,
    result_attribute: dex.Attribute[MutationResult[MovedMessage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, MoveMessageInput, MovedMessage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Email (IMAP and SMTP) Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Email (IMAP and SMTP) Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Email (IMAP and SMTP) Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "moved": moved,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "move_message"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_move_message_step, '__module__', 'dex_connectors.superdurable_email.steps')

def new_set_flags_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SetFlagsInput],
    updated: dex.StepTarget[MutationResult[MessageFlags]],
    not_found: dex.StepTarget[MutationResult[MessageFlags]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[MessageFlags]] | None = None,
    defect: dex.StepTarget[MutationResult[MessageFlags]] | None = None,
    result_attribute: dex.Attribute[MutationResult[MessageFlags]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, SetFlagsInput, MessageFlags]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Email (IMAP and SMTP) Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Email (IMAP and SMTP) Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Email (IMAP and SMTP) Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "updated": updated,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "set_flags"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_set_flags_step, '__module__', 'dex_connectors.superdurable_email.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
