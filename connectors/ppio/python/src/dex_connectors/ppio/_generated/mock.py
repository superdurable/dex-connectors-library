# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListModelsInput, ListModelsOutput, Model
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, LIST_MODELS_DEFINITION, PROVIDER_REJECTED

def list_models_found(value: ListModelsOutput, failure: Failure | None = None) -> Case[ListModelsOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(list_models_found, '__module__', "dex_connectors.ppio.ppiomock")

def list_models_provider_rejected(value: ListModelsOutput, failure: Failure | None = None) -> Case[ListModelsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_models_provider_rejected, '__module__', "dex_connectors.ppio.ppiomock")

def list_models_invalid_response(value: ListModelsOutput, failure: Failure | None = None) -> Case[ListModelsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_models_invalid_response, '__module__', "dex_connectors.ppio.ppiomock")

def list_models_defect(value: ListModelsOutput, failure: Failure | None = None) -> Case[ListModelsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_models_defect, '__module__', "dex_connectors.ppio.ppiomock")

def list_models_retry(failure: Failure, after_seconds: float = 0) -> Case[ListModelsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_models_retry, '__module__', "dex_connectors.ppio.ppiomock")

def list_models_two_models() -> Case[ListModelsOutput]:
    return Case.branch_result(FOUND, ListModelsOutput(models=(Model(id='tencent/hy4', created_at_unix=1793772213, owned_by='ppio', display_name='Hy4', status=1, context_size=1000000, endpoints=('chat/completions', 'anthropic', 'responses')), Model(id='moonshotai/kimi-k3.1-pro-highspeed', created_at_unix=1792802645, owned_by='ppio', display_name='Kimi K3.1 Pro Highspeed', status=1, context_size=1048576, endpoints=('chat/completions', 'anthropic', 'responses')))))

setattr(list_models_two_models, '__module__', "dex_connectors.ppio.ppiomock")


class Mock:

    def __init__(self, connection_name: str = "ppio") -> None:
        self.list_models_script: Query[ListModelsInput, ListModelsOutput] = Query(LIST_MODELS_DEFINITION)
        self.list_models_script.default(list_models_two_models())
        self._connection = new_connection_with_operations(
            list_models=self.list_models_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.ppio.ppiomock")

__all__ = [
    "Mock",
    "list_models_found",
    "list_models_provider_rejected",
    "list_models_invalid_response",
    "list_models_defect",
    "list_models_retry",
    "list_models_two_models",
]
