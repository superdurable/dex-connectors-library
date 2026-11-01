# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Dex Step factories for OpenAI Responses."""

from collections.abc import Callable, Mapping
from datetime import timedelta
from typing import TypeVar

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    MutationResult,
    MutationStep,
    MutationStepConfig,
    ProgressUpdate,
    QueryResult,
    QueryStep,
    QueryStepConfig,
    StepAnnotations,
)

from .connection import Connection
from .models import CreateRequest, Response, RetrieveRequest

StepInput = TypeVar("StepInput")
BranchTarget = TypeVar("BranchTarget")


def new_create_response_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateRequest],
    completed: dex.StepTarget[MutationResult[Response]],
    failed: dex.StepTarget[MutationResult[Response]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[Response]] | None = None,
    uncertain: dex.StepTarget[MutationResult[Response]] | None = None,
    defect: dex.StepTarget[MutationResult[Response]] | None = None,
    configuration_ui: ConnectorConfigurationUI | None = None,
    result_attribute: dex.Attribute[MutationResult[Response]] | None = None,
    progress_stream: dex.Stream[ProgressUpdate] | None = None,
    text_stream: dex.Stream[str] | None = None,
    text_flush_interval: timedelta = timedelta(seconds=1),
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateRequest, Response]:
    """Create the typed create Step; unwired optional outcomes fail the Flow.

    A progress or text Stream makes OpenAI stream the response. The Step must
    keep sync Execute durability because createResponse fences retries with
    its own heartbeat checkpoint.
    """
    _require_connection(connection, connection_name)
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.create_response(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("completed", completed),
                ("failed", failed),
                ("providerRejected", provider_rejected),
                ("uncertain", uncertain),
                ("defect", defect),
            ),
            configuration_ui=configuration_ui or ConnectorConfigurationUI(()),
            result_attribute=result_attribute,
            progress_stream=progress_stream,
            text_stream=text_stream,
            text_flush_interval=text_flush_interval,
            step_options_override=step_options_override,
        )
    )


def new_retrieve_response_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], RetrieveRequest],
    found: dex.StepTarget[QueryResult[Response]],
    not_found: dex.StepTarget[QueryResult[Response]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Response]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Response]] | None = None,
    defect: dex.StepTarget[QueryResult[Response]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Response]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, RetrieveRequest, Response]:
    """Create the typed stored response lookup Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.retrieve_response(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("found", found),
                ("notFound", not_found),
                ("providerRejected", provider_rejected),
                ("invalidResponse", invalid_response),
                ("defect", defect),
            ),
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def _require_connection(connection: Connection, connection_name: str) -> None:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("OpenAI Step connection name must match its connection reference")


def _step_annotations(annotations: StepAnnotations | Mapping[str, str]) -> StepAnnotations:
    if isinstance(annotations, StepAnnotations):
        return annotations
    if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
        raise ValueError("OpenAI Step annotations require groupId, groupLabel, and explanation")
    if any(not isinstance(value, str) for value in annotations.values()):
        raise ValueError("OpenAI Step annotations must be strings")
    return StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])


def _branches(*targets: tuple[str, BranchTarget | None]) -> dict[str, BranchTarget]:
    return {branch: target for branch, target in targets if target is not None}
