# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import FunctionDetails, GetFunctionInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_FUNCTION_DEFINITION, INVALID_RESPONSE, MISSING, PROVIDER_REJECTED

def get_function_found(value: FunctionDetails, failure: Failure | None = None) -> Case[FunctionDetails]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_function_found, '__module__', "dex_connectors.amazon_lambda.lambdamock")

def get_function_missing(value: FunctionDetails, failure: Failure | None = None) -> Case[FunctionDetails]:
    return Case.branch_result(MISSING, value, failure)

setattr(get_function_missing, '__module__', "dex_connectors.amazon_lambda.lambdamock")

def get_function_provider_rejected(value: FunctionDetails, failure: Failure | None = None) -> Case[FunctionDetails]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_function_provider_rejected, '__module__', "dex_connectors.amazon_lambda.lambdamock")

def get_function_invalid_response(value: FunctionDetails, failure: Failure | None = None) -> Case[FunctionDetails]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_function_invalid_response, '__module__', "dex_connectors.amazon_lambda.lambdamock")

def get_function_defect(value: FunctionDetails, failure: Failure | None = None) -> Case[FunctionDetails]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_function_defect, '__module__', "dex_connectors.amazon_lambda.lambdamock")

def get_function_retry(failure: Failure, after_seconds: float = 0) -> Case[FunctionDetails]:
    return Case.retry(failure, after_seconds)

setattr(get_function_retry, '__module__', "dex_connectors.amazon_lambda.lambdamock")

def get_function_found_default() -> Case[FunctionDetails]:
    return Case.branch_result(FOUND, FunctionDetails(name='example-function', arn='arn:aws:lambda:us-east-1:123456789012:function:example-function'))

setattr(get_function_found_default, '__module__', "dex_connectors.amazon_lambda.lambdamock")


class Mock:

    def __init__(self, connection_name: str = "amazon-lambda") -> None:
        self.get_function_script: Query[GetFunctionInput, FunctionDetails] = Query(GET_FUNCTION_DEFINITION)
        self.get_function_script.default(get_function_found_default())
        self._connection = new_connection_with_operations(
            get_function=self.get_function_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.amazon_lambda.lambdamock")

__all__ = [
    "Mock",
    "get_function_found",
    "get_function_missing",
    "get_function_provider_rejected",
    "get_function_invalid_response",
    "get_function_defect",
    "get_function_retry",
    "get_function_found_default",
]
