# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated operation-specific Dex Step factories."""

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
from ..models import QueryVectorsInput, QueryVectorsOutput, UpsertVectorsInput, UpsertVectorsOutput

StepInput = TypeVar("StepInput")

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
    """Create the upsert_vectors Step with the manifest branch identities."""
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Pinecone Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Pinecone Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Pinecone Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "upserted": upserted,
    }
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
            operation=_bound_operation(connection, "upsert_vectors"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_upsert_vectors_step, '__module__', 'dex_connectors.pinecone.steps')

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
    """Create the query_vectors Step with the manifest branch identities."""
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Pinecone Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Pinecone Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Pinecone Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "queried": queried,
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
            operation=_bound_operation(connection, "query_vectors"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_query_vectors_step, '__module__', 'dex_connectors.pinecone.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    """Resolve a bound operation from a connection or its operation collection."""
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
