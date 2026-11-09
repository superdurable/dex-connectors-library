# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetPackageInput, PackageStatus
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_PACKAGE_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_package_found(value: PackageStatus, failure: Failure | None = None) -> Case[PackageStatus]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_package_found, '__module__', "dex_connectors.onespan_sign.onespan_signmock")

def get_package_not_found(value: PackageStatus, failure: Failure | None = None) -> Case[PackageStatus]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_package_not_found, '__module__', "dex_connectors.onespan_sign.onespan_signmock")

def get_package_provider_rejected(value: PackageStatus, failure: Failure | None = None) -> Case[PackageStatus]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_package_provider_rejected, '__module__', "dex_connectors.onespan_sign.onespan_signmock")

def get_package_invalid_response(value: PackageStatus, failure: Failure | None = None) -> Case[PackageStatus]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_package_invalid_response, '__module__', "dex_connectors.onespan_sign.onespan_signmock")

def get_package_defect(value: PackageStatus, failure: Failure | None = None) -> Case[PackageStatus]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_package_defect, '__module__', "dex_connectors.onespan_sign.onespan_signmock")

def get_package_retry(failure: Failure, after_seconds: float = 0) -> Case[PackageStatus]:
    return Case.retry(failure, after_seconds)

setattr(get_package_retry, '__module__', "dex_connectors.onespan_sign.onespan_signmock")

def get_package_found_default() -> Case[PackageStatus]:
    return Case.branch_result(FOUND, PackageStatus(id='9sKhW-h-qS9m6Ho3zRv3n2a-rkI=', name='Example agreement', status='COMPLETED'))

setattr(get_package_found_default, '__module__', "dex_connectors.onespan_sign.onespan_signmock")


class Mock:

    def __init__(self, connection_name: str = "onespan-sign") -> None:
        self.get_package_script: Query[GetPackageInput, PackageStatus] = Query(GET_PACKAGE_DEFINITION)
        self.get_package_script.default(get_package_found_default())
        self._connection = new_connection_with_operations(
            get_package=self.get_package_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.onespan_sign.onespan_signmock")

__all__ = [
    "Mock",
    "get_package_found",
    "get_package_not_found",
    "get_package_provider_rejected",
    "get_package_invalid_response",
    "get_package_defect",
    "get_package_retry",
    "get_package_found_default",
]
