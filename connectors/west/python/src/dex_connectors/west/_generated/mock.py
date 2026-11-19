# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Domain, ListDomainsInput, ListDomainsOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_DOMAINS_DEFINITION, PROVIDER_REJECTED

def list_domains_listed(value: ListDomainsOutput, failure: Failure | None = None) -> Case[ListDomainsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_domains_listed, '__module__', "dex_connectors.west.westmock")

def list_domains_provider_rejected(value: ListDomainsOutput, failure: Failure | None = None) -> Case[ListDomainsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_domains_provider_rejected, '__module__', "dex_connectors.west.westmock")

def list_domains_invalid_response(value: ListDomainsOutput, failure: Failure | None = None) -> Case[ListDomainsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_domains_invalid_response, '__module__', "dex_connectors.west.westmock")

def list_domains_defect(value: ListDomainsOutput, failure: Failure | None = None) -> Case[ListDomainsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_domains_defect, '__module__', "dex_connectors.west.westmock")

def list_domains_retry(failure: Failure, after_seconds: float = 0) -> Case[ListDomainsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_domains_retry, '__module__', "dex_connectors.west.westmock")

def list_domains_sample_domains() -> Case[ListDomainsOutput]:
    return Case.branch_result(LISTED, ListDomainsOutput(domains=(Domain(name='example.cn', expires_at='2030-01-01'),), page=1, total_pages=1, total=1))

setattr(list_domains_sample_domains, '__module__', "dex_connectors.west.westmock")


class Mock:

    def __init__(self, connection_name: str = "west") -> None:
        self.list_domains_script: Query[ListDomainsInput, ListDomainsOutput] = Query(LIST_DOMAINS_DEFINITION)
        self.list_domains_script.default(list_domains_sample_domains())
        self._connection = new_connection_with_operations(
            list_domains=self.list_domains_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.west.westmock")

__all__ = [
    "Mock",
    "list_domains_listed",
    "list_domains_provider_rejected",
    "list_domains_invalid_response",
    "list_domains_defect",
    "list_domains_retry",
    "list_domains_sample_domains",
]
