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

setattr(get_company_profile_found, '__module__', "dex_connectors.tianyancha.tianyanchamock")

def get_company_profile_no_data(value: GetCompanyProfileOutput, failure: Failure | None = None) -> Case[GetCompanyProfileOutput]:
    return Case.branch_result(NO_DATA, value, failure)

setattr(get_company_profile_no_data, '__module__', "dex_connectors.tianyancha.tianyanchamock")

def get_company_profile_provider_rejected(value: GetCompanyProfileOutput, failure: Failure | None = None) -> Case[GetCompanyProfileOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_company_profile_provider_rejected, '__module__', "dex_connectors.tianyancha.tianyanchamock")

def get_company_profile_invalid_response(value: GetCompanyProfileOutput, failure: Failure | None = None) -> Case[GetCompanyProfileOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_company_profile_invalid_response, '__module__', "dex_connectors.tianyancha.tianyanchamock")

def get_company_profile_defect(value: GetCompanyProfileOutput, failure: Failure | None = None) -> Case[GetCompanyProfileOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_company_profile_defect, '__module__', "dex_connectors.tianyancha.tianyanchamock")

def get_company_profile_retry(failure: Failure, after_seconds: float = 0) -> Case[GetCompanyProfileOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_company_profile_retry, '__module__', "dex_connectors.tianyancha.tianyanchamock")

def get_company_profile_sample_company() -> Case[GetCompanyProfileOutput]:
    return Case.branch_result(FOUND, GetCompanyProfileOutput(company_id=199557844, name='平安银行股份有限公司', registration_status='存续', credit_code='91440300192185379H', industry='货币金融服务', registered_capital='1142489.4787万人民币', company_type='股份有限公司(上市)'))

setattr(get_company_profile_sample_company, '__module__', "dex_connectors.tianyancha.tianyanchamock")

def get_company_profile_no_company() -> Case[GetCompanyProfileOutput]:
    return Case.branch_result(NO_DATA, GetCompanyProfileOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getCompanyProfile", "Tianyancha found no matching company."))

setattr(get_company_profile_no_company, '__module__', "dex_connectors.tianyancha.tianyanchamock")


class Mock:

    def __init__(self, connection_name: str = "tianyancha") -> None:
        self.get_company_profile_script: Query[GetCompanyProfileInput, GetCompanyProfileOutput] = Query(GET_COMPANY_PROFILE_DEFINITION)
        self.get_company_profile_script.default(get_company_profile_sample_company())
        self._connection = new_connection_with_operations(
            get_company_profile=self.get_company_profile_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.tianyancha.tianyanchamock")

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
