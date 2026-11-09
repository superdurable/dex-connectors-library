# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetTransferInput, TransferStatus
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_TRANSFER_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_transfer_found(value: TransferStatus, failure: Failure | None = None) -> Case[TransferStatus]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_transfer_found, '__module__', "dex_connectors.wise.wisemock")

def get_transfer_not_found(value: TransferStatus, failure: Failure | None = None) -> Case[TransferStatus]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_transfer_not_found, '__module__', "dex_connectors.wise.wisemock")

def get_transfer_provider_rejected(value: TransferStatus, failure: Failure | None = None) -> Case[TransferStatus]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_transfer_provider_rejected, '__module__', "dex_connectors.wise.wisemock")

def get_transfer_invalid_response(value: TransferStatus, failure: Failure | None = None) -> Case[TransferStatus]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_transfer_invalid_response, '__module__', "dex_connectors.wise.wisemock")

def get_transfer_defect(value: TransferStatus, failure: Failure | None = None) -> Case[TransferStatus]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_transfer_defect, '__module__', "dex_connectors.wise.wisemock")

def get_transfer_retry(failure: Failure, after_seconds: float = 0) -> Case[TransferStatus]:
    return Case.retry(failure, after_seconds)

setattr(get_transfer_retry, '__module__', "dex_connectors.wise.wisemock")

def get_transfer_found_default() -> Case[TransferStatus]:
    return Case.branch_result(FOUND, TransferStatus(id='16521632', source_currency='EUR', status='incoming_payment_waiting'))

setattr(get_transfer_found_default, '__module__', "dex_connectors.wise.wisemock")


class Mock:

    def __init__(self, connection_name: str = "wise") -> None:
        self.get_transfer_script: Query[GetTransferInput, TransferStatus] = Query(GET_TRANSFER_DEFINITION)
        self.get_transfer_script.default(get_transfer_found_default())
        self._connection = new_connection_with_operations(
            get_transfer=self.get_transfer_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.wise.wisemock")

__all__ = [
    "Mock",
    "get_transfer_found",
    "get_transfer_not_found",
    "get_transfer_provider_rejected",
    "get_transfer_invalid_response",
    "get_transfer_defect",
    "get_transfer_retry",
    "get_transfer_found_default",
]
