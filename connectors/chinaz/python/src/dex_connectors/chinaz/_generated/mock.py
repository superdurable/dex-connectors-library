# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetSiteTDKInput, SiteTDK
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_SITE_TDK_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_site_tdk_found(value: SiteTDK, failure: Failure | None = None) -> Case[SiteTDK]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_site_tdk_found, '__module__', "dex_connectors.chinaz.chinazmock")

def get_site_tdk_not_found(value: SiteTDK, failure: Failure | None = None) -> Case[SiteTDK]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_site_tdk_not_found, '__module__', "dex_connectors.chinaz.chinazmock")

def get_site_tdk_provider_rejected(value: SiteTDK, failure: Failure | None = None) -> Case[SiteTDK]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_site_tdk_provider_rejected, '__module__', "dex_connectors.chinaz.chinazmock")

def get_site_tdk_invalid_response(value: SiteTDK, failure: Failure | None = None) -> Case[SiteTDK]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_site_tdk_invalid_response, '__module__', "dex_connectors.chinaz.chinazmock")

def get_site_tdk_defect(value: SiteTDK, failure: Failure | None = None) -> Case[SiteTDK]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_site_tdk_defect, '__module__', "dex_connectors.chinaz.chinazmock")

def get_site_tdk_retry(failure: Failure, after_seconds: float = 0) -> Case[SiteTDK]:
    return Case.retry(failure, after_seconds)

setattr(get_site_tdk_retry, '__module__', "dex_connectors.chinaz.chinazmock")

def get_site_tdk_found_site() -> Case[SiteTDK]:
    return Case.branch_result(FOUND, SiteTDK(domain='example.com', title='Example Site', keywords='example', description='Public example site', ip_address='192.0.2.1'))

setattr(get_site_tdk_found_site, '__module__', "dex_connectors.chinaz.chinazmock")


class Mock:

    def __init__(self, connection_name: str = "chinaz") -> None:
        self.get_site_tdk_script: Query[GetSiteTDKInput, SiteTDK] = Query(GET_SITE_TDK_DEFINITION)
        self.get_site_tdk_script.default(get_site_tdk_found_site())
        self._connection = new_connection_with_operations(
            get_site_tdk=self.get_site_tdk_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.chinaz.chinazmock")

__all__ = [
    "Mock",
    "get_site_tdk_found",
    "get_site_tdk_not_found",
    "get_site_tdk_provider_rejected",
    "get_site_tdk_invalid_response",
    "get_site_tdk_defect",
    "get_site_tdk_retry",
    "get_site_tdk_found_site",
]
