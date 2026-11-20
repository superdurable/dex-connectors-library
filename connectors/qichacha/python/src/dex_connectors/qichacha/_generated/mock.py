# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetEnterpriseBasicDetailsInput, GetEnterpriseBasicDetailsOutput
from .definitions import CONNECTOR_ID, DEFECT, FETCHED, GET_ENTERPRISE_BASIC_DETAILS_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_enterprise_basic_details_fetched(value: GetEnterpriseBasicDetailsOutput, failure: Failure | None = None) -> Case[GetEnterpriseBasicDetailsOutput]:
    return Case.branch_result(FETCHED, value, failure)

setattr(get_enterprise_basic_details_fetched, '__module__', "dex_connectors.qichacha.qichachamock")

def get_enterprise_basic_details_provider_rejected(value: GetEnterpriseBasicDetailsOutput, failure: Failure | None = None) -> Case[GetEnterpriseBasicDetailsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_enterprise_basic_details_provider_rejected, '__module__', "dex_connectors.qichacha.qichachamock")

def get_enterprise_basic_details_invalid_response(value: GetEnterpriseBasicDetailsOutput, failure: Failure | None = None) -> Case[GetEnterpriseBasicDetailsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_enterprise_basic_details_invalid_response, '__module__', "dex_connectors.qichacha.qichachamock")

def get_enterprise_basic_details_defect(value: GetEnterpriseBasicDetailsOutput, failure: Failure | None = None) -> Case[GetEnterpriseBasicDetailsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_enterprise_basic_details_defect, '__module__', "dex_connectors.qichacha.qichachamock")

def get_enterprise_basic_details_retry(failure: Failure, after_seconds: float = 0) -> Case[GetEnterpriseBasicDetailsOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_enterprise_basic_details_retry, '__module__', "dex_connectors.qichacha.qichachamock")

def get_enterprise_basic_details_sample_mainland_enterprise() -> Case[GetEnterpriseBasicDetailsOutput]:
    return Case.branch_result(FETCHED, GetEnterpriseBasicDetailsOutput(name='示例科技有限公司', credit_code='91110101000000000X', registration_status='存续'))

setattr(get_enterprise_basic_details_sample_mainland_enterprise, '__module__', "dex_connectors.qichacha.qichachamock")


class Mock:

    def __init__(self, connection_name: str = "qichacha") -> None:
        self.get_enterprise_basic_details_script: Query[GetEnterpriseBasicDetailsInput, GetEnterpriseBasicDetailsOutput] = Query(GET_ENTERPRISE_BASIC_DETAILS_DEFINITION)
        self.get_enterprise_basic_details_script.default(get_enterprise_basic_details_sample_mainland_enterprise())
        self._connection = new_connection_with_operations(
            get_enterprise_basic_details=self.get_enterprise_basic_details_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.qichacha.qichachamock")

__all__ = [
    "Mock",
    "get_enterprise_basic_details_fetched",
    "get_enterprise_basic_details_provider_rejected",
    "get_enterprise_basic_details_invalid_response",
    "get_enterprise_basic_details_defect",
    "get_enterprise_basic_details_retry",
    "get_enterprise_basic_details_sample_mainland_enterprise",
]
