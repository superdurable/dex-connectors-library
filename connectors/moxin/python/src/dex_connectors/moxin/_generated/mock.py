# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListModelsInput, Model, ModelPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_MODELS_DEFINITION, PROVIDER_REJECTED

def list_models_listed(value: ModelPage, failure: Failure | None = None) -> Case[ModelPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_models_listed, '__module__', "dex_connectors.moxin.moxinmock")

def list_models_provider_rejected(value: ModelPage, failure: Failure | None = None) -> Case[ModelPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_models_provider_rejected, '__module__', "dex_connectors.moxin.moxinmock")

def list_models_invalid_response(value: ModelPage, failure: Failure | None = None) -> Case[ModelPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_models_invalid_response, '__module__', "dex_connectors.moxin.moxinmock")

def list_models_defect(value: ModelPage, failure: Failure | None = None) -> Case[ModelPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_models_defect, '__module__', "dex_connectors.moxin.moxinmock")

def list_models_retry(failure: Failure, after_seconds: float = 0) -> Case[ModelPage]:
    return Case.retry(failure, after_seconds)

setattr(list_models_retry, '__module__', "dex_connectors.moxin.moxinmock")

def list_models_listed_model() -> Case[ModelPage]:
    return Case.branch_result(LISTED, ModelPage(models=(Model(id='example-model'),)))

setattr(list_models_listed_model, '__module__', "dex_connectors.moxin.moxinmock")


class Mock:

    def __init__(self, connection_name: str = "moxin") -> None:
        self.list_models_script: Query[ListModelsInput, ModelPage] = Query(LIST_MODELS_DEFINITION)
        self.list_models_script.default(list_models_listed_model())
        self._connection = new_connection_with_operations(
            list_models=self.list_models_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.moxin.moxinmock")

__all__ = [
    "Mock",
    "list_models_listed",
    "list_models_provider_rejected",
    "list_models_invalid_response",
    "list_models_defect",
    "list_models_retry",
    "list_models_listed_model",
]
