# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetSignatureRequestInput, SignatureRequestStatus
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_SIGNATURE_REQUEST_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_signature_request_found(value: SignatureRequestStatus, failure: Failure | None = None) -> Case[SignatureRequestStatus]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_signature_request_found, '__module__', "dex_connectors.dropbox_sign.dropboxsignmock")

def get_signature_request_not_found(value: SignatureRequestStatus, failure: Failure | None = None) -> Case[SignatureRequestStatus]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_signature_request_not_found, '__module__', "dex_connectors.dropbox_sign.dropboxsignmock")

def get_signature_request_provider_rejected(value: SignatureRequestStatus, failure: Failure | None = None) -> Case[SignatureRequestStatus]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_signature_request_provider_rejected, '__module__', "dex_connectors.dropbox_sign.dropboxsignmock")

def get_signature_request_invalid_response(value: SignatureRequestStatus, failure: Failure | None = None) -> Case[SignatureRequestStatus]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_signature_request_invalid_response, '__module__', "dex_connectors.dropbox_sign.dropboxsignmock")

def get_signature_request_defect(value: SignatureRequestStatus, failure: Failure | None = None) -> Case[SignatureRequestStatus]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_signature_request_defect, '__module__', "dex_connectors.dropbox_sign.dropboxsignmock")

def get_signature_request_retry(failure: Failure, after_seconds: float = 0) -> Case[SignatureRequestStatus]:
    return Case.retry(failure, after_seconds)

setattr(get_signature_request_retry, '__module__', "dex_connectors.dropbox_sign.dropboxsignmock")

def get_signature_request_found_default() -> Case[SignatureRequestStatus]:
    return Case.branch_result(FOUND, SignatureRequestStatus(id='fa5c8a0b0f492d768749333ad6fcc214c111e967', title='Example agreement', is_complete=True, is_declined=False, has_error=False))

setattr(get_signature_request_found_default, '__module__', "dex_connectors.dropbox_sign.dropboxsignmock")


class Mock:

    def __init__(self, connection_name: str = "dropbox-sign") -> None:
        self.get_signature_request_script: Query[GetSignatureRequestInput, SignatureRequestStatus] = Query(GET_SIGNATURE_REQUEST_DEFINITION)
        self.get_signature_request_script.default(get_signature_request_found_default())
        self._connection = new_connection_with_operations(
            get_signature_request=self.get_signature_request_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.dropbox_sign.dropboxsignmock")

__all__ = [
    "Mock",
    "get_signature_request_found",
    "get_signature_request_not_found",
    "get_signature_request_provider_rejected",
    "get_signature_request_invalid_response",
    "get_signature_request_defect",
    "get_signature_request_retry",
    "get_signature_request_found_default",
]
