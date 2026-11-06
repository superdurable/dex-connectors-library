# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation

from ..connection import Connection, new_connection_with_operations
from ..models import SendRequestInput, SendRequestOutput
from .definitions import CONNECTOR_ID, COMPLETED, DEFECT, REJECTED, SEND_REQUEST_DEFINITION, UNCERTAIN

def send_request_completed(value: SendRequestOutput, failure: Failure | None = None) -> Case[SendRequestOutput]:
    return Case.branch_result(COMPLETED, value, failure)

setattr(send_request_completed, '__module__', "dex_connectors.superdurable_oauth2_http.oauth2httpmock")

def send_request_rejected(value: SendRequestOutput, failure: Failure | None = None) -> Case[SendRequestOutput]:
    return Case.branch_result(REJECTED, value, failure)

setattr(send_request_rejected, '__module__', "dex_connectors.superdurable_oauth2_http.oauth2httpmock")

def send_request_uncertain(value: SendRequestOutput, failure: Failure) -> Case[SendRequestOutput]:
    return Case.uncertain(value, failure)

setattr(send_request_uncertain, '__module__', "dex_connectors.superdurable_oauth2_http.oauth2httpmock")

def send_request_defect(value: SendRequestOutput, failure: Failure | None = None) -> Case[SendRequestOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(send_request_defect, '__module__', "dex_connectors.superdurable_oauth2_http.oauth2httpmock")

def send_request_retry(failure: Failure, after_seconds: float = 0) -> Case[SendRequestOutput]:
    return Case.retry(failure, after_seconds)

setattr(send_request_retry, '__module__', "dex_connectors.superdurable_oauth2_http.oauth2httpmock")

def send_request_user_profile() -> Case[SendRequestOutput]:
    return Case.branch_result(COMPLETED, SendRequestOutput(status_code=200, content_type='application/json', body='{"sub":"user-1","preferred_username":"alice"}'))

setattr(send_request_user_profile, '__module__', "dex_connectors.superdurable_oauth2_http.oauth2httpmock")

def send_request_not_found() -> Case[SendRequestOutput]:
    return Case.branch_result(REJECTED, SendRequestOutput(status_code=404, content_type='application/json', body='{"error":"not_found"}'), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "sendRequest", "The API answered HTTP 404."))

setattr(send_request_not_found, '__module__', "dex_connectors.superdurable_oauth2_http.oauth2httpmock")

def send_request_authorization_expired() -> Case[SendRequestOutput]:
    return Case.branch_result(REJECTED, SendRequestOutput(status_code=0), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "sendRequest", "The OAuth authorization must be renewed in Dex Web Connections."))

setattr(send_request_authorization_expired, '__module__', "dex_connectors.superdurable_oauth2_http.oauth2httpmock")

def send_request_connection_lost() -> Case[SendRequestOutput]:
    return Case.uncertain(SendRequestOutput(), Failure(FailureKind.TRANSPORT, CONNECTOR_ID, "sendRequest", "The connection closed before the API answered."))

setattr(send_request_connection_lost, '__module__', "dex_connectors.superdurable_oauth2_http.oauth2httpmock")

def send_request_invalid_path() -> Case[SendRequestOutput]:
    return Case.branch_result(DEFECT, SendRequestOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "sendRequest", "The request path must start with a slash."))

setattr(send_request_invalid_path, '__module__', "dex_connectors.superdurable_oauth2_http.oauth2httpmock")

def send_request_throttled() -> Case[SendRequestOutput]:
    return Case.retry(Failure(FailureKind.RATE_LIMIT, CONNECTOR_ID, "sendRequest", "The API answered HTTP 429."), 2)

setattr(send_request_throttled, '__module__', "dex_connectors.superdurable_oauth2_http.oauth2httpmock")


class Mock:

    def __init__(self, connection_name: str = "oauth2-http") -> None:
        self.send_request_script: Mutation[SendRequestInput, SendRequestOutput] = Mutation(SEND_REQUEST_DEFINITION)
        self.send_request_script.default(send_request_user_profile())
        self._connection = new_connection_with_operations(
            send_request=self.send_request_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.superdurable_oauth2_http.oauth2httpmock")

__all__ = [
    "Mock",
    "send_request_completed",
    "send_request_rejected",
    "send_request_uncertain",
    "send_request_defect",
    "send_request_retry",
    "send_request_user_profile",
    "send_request_not_found",
    "send_request_authorization_expired",
    "send_request_connection_lost",
    "send_request_invalid_path",
    "send_request_throttled",
]
