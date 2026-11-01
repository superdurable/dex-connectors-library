# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Dex Step factories for Cohere embedding and rerank operations."""

from collections.abc import Callable, Mapping
from typing import TypeVar

import dex
from dex_connectors.sdk import QueryResult, QueryStep, QueryStepConfig, StepAnnotations

from .connection import Connection
from .models import EmbedInput, EmbedOutput, RerankInput, RerankOutput

StepInput = TypeVar("StepInput")
BranchTarget = TypeVar("BranchTarget")


def new_embed_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], EmbedInput],
    embedded: dex.StepTarget[QueryResult[EmbedOutput]],
    provider_rejected: dex.StepTarget[QueryResult[EmbedOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[EmbedOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[EmbedOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[EmbedOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, EmbedInput, EmbedOutput]:
    """Create the typed embedding Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.embed(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("embedded", embedded),
                ("providerRejected", provider_rejected),
                ("invalidResponse", invalid_response),
                ("defect", defect),
            ),
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def new_rerank_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], RerankInput],
    ranked: dex.StepTarget[QueryResult[RerankOutput]],
    provider_rejected: dex.StepTarget[QueryResult[RerankOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[RerankOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[RerankOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[RerankOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, RerankInput, RerankOutput]:
    """Create the typed rerank Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.rerank(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("ranked", ranked),
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
        raise ValueError("Cohere Step connection name must match its connection reference")


def _step_annotations(annotations: StepAnnotations | Mapping[str, str]) -> StepAnnotations:
    if isinstance(annotations, StepAnnotations):
        return annotations
    if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
        raise ValueError("Cohere Step annotations require groupId, groupLabel, and explanation")
    if any(not isinstance(value, str) for value in annotations.values()):
        raise ValueError("Cohere Step annotations must be strings")
    return StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])


def _branches(*targets: tuple[str, BranchTarget | None]) -> dict[str, BranchTarget]:
    return {branch: target for branch, target in targets if target is not None}
