# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ReadWebPageInput, ReadWebPageOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, PROVIDER_REJECTED, READ_WEB_PAGE_DEFINITION

def read_web_page_found(value: ReadWebPageOutput, failure: Failure | None = None) -> Case[ReadWebPageOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(read_web_page_found, '__module__', "dex_connectors.dataeyes.dataeyesmock")

def read_web_page_provider_rejected(value: ReadWebPageOutput, failure: Failure | None = None) -> Case[ReadWebPageOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(read_web_page_provider_rejected, '__module__', "dex_connectors.dataeyes.dataeyesmock")

def read_web_page_invalid_response(value: ReadWebPageOutput, failure: Failure | None = None) -> Case[ReadWebPageOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(read_web_page_invalid_response, '__module__', "dex_connectors.dataeyes.dataeyesmock")

def read_web_page_defect(value: ReadWebPageOutput, failure: Failure | None = None) -> Case[ReadWebPageOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(read_web_page_defect, '__module__', "dex_connectors.dataeyes.dataeyesmock")

def read_web_page_retry(failure: Failure, after_seconds: float = 0) -> Case[ReadWebPageOutput]:
    return Case.retry(failure, after_seconds)

setattr(read_web_page_retry, '__module__', "dex_connectors.dataeyes.dataeyesmock")

def read_web_page_page_read() -> Case[ReadWebPageOutput]:
    return Case.branch_result(FOUND, ReadWebPageOutput(title='Example Domain', markdown='# Example Domain\n\nExample page content.'))

setattr(read_web_page_page_read, '__module__', "dex_connectors.dataeyes.dataeyesmock")

def read_web_page_empty_page() -> Case[ReadWebPageOutput]:
    return Case.branch_result(FOUND, ReadWebPageOutput(title='Empty Page', markdown=''))

setattr(read_web_page_empty_page, '__module__', "dex_connectors.dataeyes.dataeyesmock")


class Mock:

    def __init__(self, connection_name: str = "dataeyes") -> None:
        self.read_web_page_script: Query[ReadWebPageInput, ReadWebPageOutput] = Query(READ_WEB_PAGE_DEFINITION)
        self.read_web_page_script.default(read_web_page_page_read())
        self._connection = new_connection_with_operations(
            read_web_page=self.read_web_page_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.dataeyes.dataeyesmock")

__all__ = [
    "Mock",
    "read_web_page_found",
    "read_web_page_provider_rejected",
    "read_web_page_invalid_response",
    "read_web_page_defect",
    "read_web_page_retry",
    "read_web_page_page_read",
    "read_web_page_empty_page",
]
