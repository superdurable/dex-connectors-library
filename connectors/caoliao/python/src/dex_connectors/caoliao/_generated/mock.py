# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListQRCodesInput, ListQRCodesOutput, QRCodeSummary
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_QR_CODES_DEFINITION, PROVIDER_REJECTED

def list_qr_codes_listed(value: ListQRCodesOutput, failure: Failure | None = None) -> Case[ListQRCodesOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_qr_codes_listed, '__module__', "dex_connectors.caoliao.caoliaomock")

def list_qr_codes_provider_rejected(value: ListQRCodesOutput, failure: Failure | None = None) -> Case[ListQRCodesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_qr_codes_provider_rejected, '__module__', "dex_connectors.caoliao.caoliaomock")

def list_qr_codes_invalid_response(value: ListQRCodesOutput, failure: Failure | None = None) -> Case[ListQRCodesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_qr_codes_invalid_response, '__module__', "dex_connectors.caoliao.caoliaomock")

def list_qr_codes_defect(value: ListQRCodesOutput, failure: Failure | None = None) -> Case[ListQRCodesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_qr_codes_defect, '__module__', "dex_connectors.caoliao.caoliaomock")

def list_qr_codes_retry(failure: Failure, after_seconds: float = 0) -> Case[ListQRCodesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_qr_codes_retry, '__module__', "dex_connectors.caoliao.caoliaomock")

def list_qr_codes_sample_qr_codes() -> Case[ListQRCodesOutput]:
    return Case.branch_result(LISTED, ListQRCodesOutput(qr_codes=(QRCodeSummary(name='Example inspection point', coding='qk3mJHJ'),), next_page_token='', total=1))

setattr(list_qr_codes_sample_qr_codes, '__module__', "dex_connectors.caoliao.caoliaomock")


class Mock:

    def __init__(self, connection_name: str = "caoliao") -> None:
        self.list_qr_codes_script: Query[ListQRCodesInput, ListQRCodesOutput] = Query(LIST_QR_CODES_DEFINITION)
        self.list_qr_codes_script.default(list_qr_codes_sample_qr_codes())
        self._connection = new_connection_with_operations(
            list_qr_codes=self.list_qr_codes_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.caoliao.caoliaomock")

__all__ = [
    "Mock",
    "list_qr_codes_listed",
    "list_qr_codes_provider_rejected",
    "list_qr_codes_invalid_response",
    "list_qr_codes_defect",
    "list_qr_codes_retry",
    "list_qr_codes_sample_qr_codes",
]
