# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetSignFlowStatusInput, GetSignFlowStatusOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_SIGN_FLOW_STATUS_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_sign_flow_status_found(value: GetSignFlowStatusOutput, failure: Failure | None = None) -> Case[GetSignFlowStatusOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_sign_flow_status_found, '__module__', "dex_connectors.esign.esignmock")

def get_sign_flow_status_provider_rejected(value: GetSignFlowStatusOutput, failure: Failure | None = None) -> Case[GetSignFlowStatusOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_sign_flow_status_provider_rejected, '__module__', "dex_connectors.esign.esignmock")

def get_sign_flow_status_invalid_response(value: GetSignFlowStatusOutput, failure: Failure | None = None) -> Case[GetSignFlowStatusOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_sign_flow_status_invalid_response, '__module__', "dex_connectors.esign.esignmock")

def get_sign_flow_status_defect(value: GetSignFlowStatusOutput, failure: Failure | None = None) -> Case[GetSignFlowStatusOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_sign_flow_status_defect, '__module__', "dex_connectors.esign.esignmock")

def get_sign_flow_status_retry(failure: Failure, after_seconds: float = 0) -> Case[GetSignFlowStatusOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_sign_flow_status_retry, '__module__', "dex_connectors.esign.esignmock")

def get_sign_flow_status_signing() -> Case[GetSignFlowStatusOutput]:
    return Case.branch_result(FOUND, GetSignFlowStatusOutput(sign_flow_id='flow_example', sign_flow_status=1))

setattr(get_sign_flow_status_signing, '__module__', "dex_connectors.esign.esignmock")


class Mock:

    def __init__(self, connection_name: str = "esign") -> None:
        self.get_sign_flow_status_script: Query[GetSignFlowStatusInput, GetSignFlowStatusOutput] = Query(GET_SIGN_FLOW_STATUS_DEFINITION)
        self.get_sign_flow_status_script.default(get_sign_flow_status_signing())
        self._connection = new_connection_with_operations(
            get_sign_flow_status=self.get_sign_flow_status_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.esign.esignmock")

__all__ = [
    "Mock",
    "get_sign_flow_status_found",
    "get_sign_flow_status_provider_rejected",
    "get_sign_flow_status_invalid_response",
    "get_sign_flow_status_defect",
    "get_sign_flow_status_retry",
    "get_sign_flow_status_signing",
]
