# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import DescribeDomainInput, DomainDetail
from .definitions import CONNECTOR_ID, DEFECT, DESCRIBE_DOMAIN_DEFINITION, FOUND, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def describe_domain_found(value: DomainDetail, failure: Failure | None = None) -> Case[DomainDetail]:
    return Case.branch_result(FOUND, value, failure)

setattr(describe_domain_found, '__module__', "dex_connectors.baishancloud.baishancloudmock")

def describe_domain_not_found(value: DomainDetail, failure: Failure | None = None) -> Case[DomainDetail]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(describe_domain_not_found, '__module__', "dex_connectors.baishancloud.baishancloudmock")

def describe_domain_provider_rejected(value: DomainDetail, failure: Failure | None = None) -> Case[DomainDetail]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(describe_domain_provider_rejected, '__module__', "dex_connectors.baishancloud.baishancloudmock")

def describe_domain_invalid_response(value: DomainDetail, failure: Failure | None = None) -> Case[DomainDetail]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(describe_domain_invalid_response, '__module__', "dex_connectors.baishancloud.baishancloudmock")

def describe_domain_defect(value: DomainDetail, failure: Failure | None = None) -> Case[DomainDetail]:
    return Case.branch_result(DEFECT, value, failure)

setattr(describe_domain_defect, '__module__', "dex_connectors.baishancloud.baishancloudmock")

def describe_domain_retry(failure: Failure, after_seconds: float = 0) -> Case[DomainDetail]:
    return Case.retry(failure, after_seconds)

setattr(describe_domain_retry, '__module__', "dex_connectors.baishancloud.baishancloudmock")

def describe_domain_found_domain() -> Case[DomainDetail]:
    return Case.branch_result(FOUND, DomainDetail(id='31335', domain='cdn.example.com', type='download', status='serving', cname='cdn.example.com.qingcdn.com.'))

setattr(describe_domain_found_domain, '__module__', "dex_connectors.baishancloud.baishancloudmock")


class Mock:

    def __init__(self, connection_name: str = "baishancloud") -> None:
        self.describe_domain_script: Query[DescribeDomainInput, DomainDetail] = Query(DESCRIBE_DOMAIN_DEFINITION)
        self.describe_domain_script.default(describe_domain_found_domain())
        self._connection = new_connection_with_operations(
            describe_domain=self.describe_domain_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.baishancloud.baishancloudmock")

__all__ = [
    "Mock",
    "describe_domain_found",
    "describe_domain_not_found",
    "describe_domain_provider_rejected",
    "describe_domain_invalid_response",
    "describe_domain_defect",
    "describe_domain_retry",
    "describe_domain_found_domain",
]
