# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListModelsInput, ListModelsOutput, Model
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_MODELS_DEFINITION, PROVIDER_REJECTED

def list_models_listed(value: ListModelsOutput, failure: Failure | None = None) -> Case[ListModelsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_models_listed, '__module__', "dex_connectors.tokengp.tokengpmock")

def list_models_provider_rejected(value: ListModelsOutput, failure: Failure | None = None) -> Case[ListModelsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_models_provider_rejected, '__module__', "dex_connectors.tokengp.tokengpmock")

def list_models_invalid_response(value: ListModelsOutput, failure: Failure | None = None) -> Case[ListModelsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_models_invalid_response, '__module__', "dex_connectors.tokengp.tokengpmock")

def list_models_defect(value: ListModelsOutput, failure: Failure | None = None) -> Case[ListModelsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_models_defect, '__module__', "dex_connectors.tokengp.tokengpmock")

def list_models_retry(failure: Failure, after_seconds: float = 0) -> Case[ListModelsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_models_retry, '__module__', "dex_connectors.tokengp.tokengpmock")

def list_models_sample_models() -> Case[ListModelsOutput]:
    return Case.branch_result(LISTED, ListModelsOutput(models=(Model(id='qwen3.8-max'),)))

setattr(list_models_sample_models, '__module__', "dex_connectors.tokengp.tokengpmock")


class Mock:

    def __init__(self, connection_name: str = "tokengp") -> None:
        self.list_models_script: Query[ListModelsInput, ListModelsOutput] = Query(LIST_MODELS_DEFINITION)
        self.list_models_script.default(list_models_sample_models())
        self._connection = new_connection_with_operations(
            list_models=self.list_models_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.tokengp.tokengpmock")

__all__ = [
    "Mock",
    "list_models_listed",
    "list_models_provider_rejected",
    "list_models_invalid_response",
    "list_models_defect",
    "list_models_retry",
    "list_models_sample_models",
]
