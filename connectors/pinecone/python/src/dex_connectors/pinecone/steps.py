# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Dex Step factories for Pinecone vector operations."""

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
from .models import QueryVectorsInput, QueryVectorsOutput, UpsertVectorsInput, UpsertVectorsOutput

StepInput = TypeVar("StepInput")
BranchTarget = TypeVar("BranchTarget")


def new_upsert_vectors_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpsertVectorsInput],
    upserted: dex.StepTarget[MutationResult[UpsertVectorsOutput]],
    provider_rejected: dex.StepTarget[MutationResult[UpsertVectorsOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UpsertVectorsOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpsertVectorsOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpsertVectorsOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpsertVectorsInput, UpsertVectorsOutput]:
    """Create the typed vector upsert Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.upsert_vectors(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("upserted", upserted),
                ("providerRejected", provider_rejected),
                ("invalidResponse", invalid_response),
                ("defect", defect),
            ),
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def new_query_vectors_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], QueryVectorsInput],
    queried: dex.StepTarget[QueryResult[QueryVectorsOutput]],
    provider_rejected: dex.StepTarget[QueryResult[QueryVectorsOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[QueryVectorsOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[QueryVectorsOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[QueryVectorsOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, QueryVectorsInput, QueryVectorsOutput]:
    """Create the typed vector query Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.query_vectors(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("queried", queried),
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
        raise ValueError("Pinecone Step connection name must match its connection reference")


def _step_annotations(annotations: StepAnnotations | Mapping[str, str]) -> StepAnnotations:
    if isinstance(annotations, StepAnnotations):
        return annotations
    if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
        raise ValueError("Pinecone Step annotations require groupId, groupLabel, and explanation")
    if any(not isinstance(value, str) for value in annotations.values()):
        raise ValueError("Pinecone Step annotations must be strings")
    return StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])


def _branches(*targets: tuple[str, BranchTarget | None]) -> dict[str, BranchTarget]:
    return {branch: target for branch, target in targets if target is not None}
