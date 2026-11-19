# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetCompanyProfileInput, GetCompanyProfileOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_COMPANY_PROFILE_DEFINITION, INVALID_RESPONSE, NO_DATA, PROVIDER_REJECTED

def get_company_profile_found(value: GetCompanyProfileOutput, failure: Failure | None = None) -> Case[GetCompanyProfileOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_company_profile_found, '__module__', "dex_connectors.yuandian.yuandianmock")

def get_company_profile_no_data(value: GetCompanyProfileOutput, failure: Failure | None = None) -> Case[GetCompanyProfileOutput]:
    return Case.branch_result(NO_DATA, value, failure)

setattr(get_company_profile_no_data, '__module__', "dex_connectors.yuandian.yuandianmock")

def get_company_profile_provider_rejected(value: GetCompanyProfileOutput, failure: Failure | None = None) -> Case[GetCompanyProfileOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_company_profile_provider_rejected, '__module__', "dex_connectors.yuandian.yuandianmock")

def get_company_profile_invalid_response(value: GetCompanyProfileOutput, failure: Failure | None = None) -> Case[GetCompanyProfileOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_company_profile_invalid_response, '__module__', "dex_connectors.yuandian.yuandianmock")

def get_company_profile_defect(value: GetCompanyProfileOutput, failure: Failure | None = None) -> Case[GetCompanyProfileOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_company_profile_defect, '__module__', "dex_connectors.yuandian.yuandianmock")

def get_company_profile_retry(failure: Failure, after_seconds: float = 0) -> Case[GetCompanyProfileOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_company_profile_retry, '__module__', "dex_connectors.yuandian.yuandianmock")

def get_company_profile_sample_company() -> Case[GetCompanyProfileOutput]:
    return Case.branch_result(FOUND, GetCompanyProfileOutput(company_id='sample-id', name='示例企业有限公司', credit_code='91110108MA0074PN30', registration_status='存续', industry='商务服务业', company_type='有限责任公司'))

setattr(get_company_profile_sample_company, '__module__', "dex_connectors.yuandian.yuandianmock")

def get_company_profile_no_company() -> Case[GetCompanyProfileOutput]:
    return Case.branch_result(NO_DATA, GetCompanyProfileOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getCompanyProfile", "Yuandian found no matching company."))

setattr(get_company_profile_no_company, '__module__', "dex_connectors.yuandian.yuandianmock")


class Mock:

    def __init__(self, connection_name: str = "yuandian") -> None:
        self.get_company_profile_script: Query[GetCompanyProfileInput, GetCompanyProfileOutput] = Query(GET_COMPANY_PROFILE_DEFINITION)
        self.get_company_profile_script.default(get_company_profile_sample_company())
        self._connection = new_connection_with_operations(
            get_company_profile=self.get_company_profile_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.yuandian.yuandianmock")

__all__ = [
    "Mock",
    "get_company_profile_found",
    "get_company_profile_no_data",
    "get_company_profile_provider_rejected",
    "get_company_profile_invalid_response",
    "get_company_profile_defect",
    "get_company_profile_retry",
    "get_company_profile_sample_company",
    "get_company_profile_no_company",
]
