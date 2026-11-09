# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation

from ..connection import Connection, new_connection_with_operations
from ..models import SetUserPropertiesInput, UserPropertiesResult
from .definitions import CONNECTOR_ID, ACCEPTED, DEFECT, INVALID_RESPONSE, PROVIDER_REJECTED, SET_USER_PROPERTIES_DEFINITION

def set_user_properties_accepted(value: UserPropertiesResult, failure: Failure | None = None) -> Case[UserPropertiesResult]:
    return Case.branch_result(ACCEPTED, value, failure)

setattr(set_user_properties_accepted, '__module__', "dex_connectors.heap.heapmock")

def set_user_properties_provider_rejected(value: UserPropertiesResult, failure: Failure | None = None) -> Case[UserPropertiesResult]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(set_user_properties_provider_rejected, '__module__', "dex_connectors.heap.heapmock")

def set_user_properties_invalid_response(value: UserPropertiesResult, failure: Failure | None = None) -> Case[UserPropertiesResult]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(set_user_properties_invalid_response, '__module__', "dex_connectors.heap.heapmock")

def set_user_properties_defect(value: UserPropertiesResult, failure: Failure | None = None) -> Case[UserPropertiesResult]:
    return Case.branch_result(DEFECT, value, failure)

setattr(set_user_properties_defect, '__module__', "dex_connectors.heap.heapmock")

def set_user_properties_retry(failure: Failure, after_seconds: float = 0) -> Case[UserPropertiesResult]:
    return Case.retry(failure, after_seconds)

setattr(set_user_properties_retry, '__module__', "dex_connectors.heap.heapmock")

def set_user_properties_accepted_default() -> Case[UserPropertiesResult]:
    return Case.branch_result(ACCEPTED, UserPropertiesResult(accepted=True))

setattr(set_user_properties_accepted_default, '__module__', "dex_connectors.heap.heapmock")


class Mock:

    def __init__(self, connection_name: str = "heap") -> None:
        self.set_user_properties_script: Mutation[SetUserPropertiesInput, UserPropertiesResult] = Mutation(SET_USER_PROPERTIES_DEFINITION)
        self.set_user_properties_script.default(set_user_properties_accepted_default())
        self._connection = new_connection_with_operations(
            set_user_properties=self.set_user_properties_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.heap.heapmock")

__all__ = [
    "Mock",
    "set_user_properties_accepted",
    "set_user_properties_provider_rejected",
    "set_user_properties_invalid_response",
    "set_user_properties_defect",
    "set_user_properties_retry",
    "set_user_properties_accepted_default",
]
