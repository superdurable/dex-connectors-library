# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Dex Step factories for Algolia search and record operations."""

from collections.abc import Callable, Mapping
from typing import TypeVar

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

from .connection import Connection
from .models import SavedObject, SaveObjectInput, SearchInput, SearchPage

StepInput = TypeVar("StepInput")
BranchTarget = TypeVar("BranchTarget")


def new_save_object_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SaveObjectInput],
    saved: dex.StepTarget[MutationResult[SavedObject]],
    provider_rejected: dex.StepTarget[MutationResult[SavedObject]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[SavedObject]] | None = None,
    defect: dex.StepTarget[MutationResult[SavedObject]] | None = None,
    result_attribute: dex.Attribute[MutationResult[SavedObject]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, SaveObjectInput, SavedObject]:
    """Create the typed record replacement Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.save_object(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("saved", saved),
                ("providerRejected", provider_rejected),
                ("invalidResponse", invalid_response),
                ("defect", defect),
            ),
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def new_search_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SearchInput],
    searched: dex.StepTarget[QueryResult[SearchPage]],
    provider_rejected: dex.StepTarget[QueryResult[SearchPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[SearchPage]] | None = None,
    defect: dex.StepTarget[QueryResult[SearchPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[SearchPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, SearchInput, SearchPage]:
    """Create the typed index search Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.search(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("searched", searched),
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
        raise ValueError("Algolia Step connection name must match its connection reference")


def _step_annotations(annotations: StepAnnotations | Mapping[str, str]) -> StepAnnotations:
    if isinstance(annotations, StepAnnotations):
        return annotations
    if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
        raise ValueError("Algolia Step annotations require groupId, groupLabel, and explanation")
    if any(not isinstance(value, str) for value in annotations.values()):
        raise ValueError("Algolia Step annotations must be strings")
    return StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])


def _branches(*targets: tuple[str, BranchTarget | None]) -> dict[str, BranchTarget]:
    return {branch: target for branch, target in targets if target is not None}
