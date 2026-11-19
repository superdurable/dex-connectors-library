# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListGatewayModelsInput, ListGatewayModelsOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, LIST_GATEWAY_MODELS_DEFINITION, PROVIDER_REJECTED

def list_gateway_models_found(value: ListGatewayModelsOutput, failure: Failure | None = None) -> Case[ListGatewayModelsOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(list_gateway_models_found, '__module__', "dex_connectors.opencsg.opencsgmock")

def list_gateway_models_provider_rejected(value: ListGatewayModelsOutput, failure: Failure | None = None) -> Case[ListGatewayModelsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_gateway_models_provider_rejected, '__module__', "dex_connectors.opencsg.opencsgmock")

def list_gateway_models_invalid_response(value: ListGatewayModelsOutput, failure: Failure | None = None) -> Case[ListGatewayModelsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_gateway_models_invalid_response, '__module__', "dex_connectors.opencsg.opencsgmock")

def list_gateway_models_defect(value: ListGatewayModelsOutput, failure: Failure | None = None) -> Case[ListGatewayModelsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_gateway_models_defect, '__module__', "dex_connectors.opencsg.opencsgmock")

def list_gateway_models_retry(failure: Failure, after_seconds: float = 0) -> Case[ListGatewayModelsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_gateway_models_retry, '__module__', "dex_connectors.opencsg.opencsgmock")

def list_gateway_models_one_model() -> Case[ListGatewayModelsOutput]:
    return Case.branch_result(FOUND, ListGatewayModelsOutput(total=1, model_ids=('OpenCSG/example-model',)))

setattr(list_gateway_models_one_model, '__module__', "dex_connectors.opencsg.opencsgmock")

def list_gateway_models_no_models() -> Case[ListGatewayModelsOutput]:
    return Case.branch_result(FOUND, ListGatewayModelsOutput(total=0, model_ids=()))

setattr(list_gateway_models_no_models, '__module__', "dex_connectors.opencsg.opencsgmock")


class Mock:

    def __init__(self, connection_name: str = "opencsg") -> None:
        self.list_gateway_models_script: Query[ListGatewayModelsInput, ListGatewayModelsOutput] = Query(LIST_GATEWAY_MODELS_DEFINITION)
        self.list_gateway_models_script.default(list_gateway_models_one_model())
        self._connection = new_connection_with_operations(
            list_gateway_models=self.list_gateway_models_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.opencsg.opencsgmock")

__all__ = [
    "Mock",
    "list_gateway_models_found",
    "list_gateway_models_provider_rejected",
    "list_gateway_models_invalid_response",
    "list_gateway_models_defect",
    "list_gateway_models_retry",
    "list_gateway_models_one_model",
    "list_gateway_models_no_models",
]
