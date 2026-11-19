# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import PolicyCard, SearchPoliciesInput, SearchPoliciesOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, PROVIDER_REJECTED, SEARCH_POLICIES_DEFINITION

def search_policies_found(value: SearchPoliciesOutput, failure: Failure | None = None) -> Case[SearchPoliciesOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(search_policies_found, '__module__', "dex_connectors.enlvon.enlvonmock")

def search_policies_provider_rejected(value: SearchPoliciesOutput, failure: Failure | None = None) -> Case[SearchPoliciesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_policies_provider_rejected, '__module__', "dex_connectors.enlvon.enlvonmock")

def search_policies_invalid_response(value: SearchPoliciesOutput, failure: Failure | None = None) -> Case[SearchPoliciesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_policies_invalid_response, '__module__', "dex_connectors.enlvon.enlvonmock")

def search_policies_defect(value: SearchPoliciesOutput, failure: Failure | None = None) -> Case[SearchPoliciesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_policies_defect, '__module__', "dex_connectors.enlvon.enlvonmock")

def search_policies_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchPoliciesOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_policies_retry, '__module__', "dex_connectors.enlvon.enlvonmock")

def search_policies_sample_policies() -> Case[SearchPoliciesOutput]:
    return Case.branch_result(FOUND, SearchPoliciesOutput(policies=(PolicyCard(title='Example electricity policy', date='2026-01-01', source='ndrc', original_url='https://www.ndrc.gov.cn/example'),)))

setattr(search_policies_sample_policies, '__module__', "dex_connectors.enlvon.enlvonmock")


class Mock:

    def __init__(self, connection_name: str = "enlvon") -> None:
        self.search_policies_script: Query[SearchPoliciesInput, SearchPoliciesOutput] = Query(SEARCH_POLICIES_DEFINITION)
        self.search_policies_script.default(search_policies_sample_policies())
        self._connection = new_connection_with_operations(
            search_policies=self.search_policies_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.enlvon.enlvonmock")

__all__ = [
    "Mock",
    "search_policies_found",
    "search_policies_provider_rejected",
    "search_policies_invalid_response",
    "search_policies_defect",
    "search_policies_retry",
    "search_policies_sample_policies",
]
