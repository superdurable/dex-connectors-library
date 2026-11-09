# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetOpportunityInput, OpportunityStage
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_OPPORTUNITY_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_opportunity_found(value: OpportunityStage, failure: Failure | None = None) -> Case[OpportunityStage]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_opportunity_found, '__module__', "dex_connectors.lever.levermock")

def get_opportunity_not_found(value: OpportunityStage, failure: Failure | None = None) -> Case[OpportunityStage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_opportunity_not_found, '__module__', "dex_connectors.lever.levermock")

def get_opportunity_provider_rejected(value: OpportunityStage, failure: Failure | None = None) -> Case[OpportunityStage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_opportunity_provider_rejected, '__module__', "dex_connectors.lever.levermock")

def get_opportunity_invalid_response(value: OpportunityStage, failure: Failure | None = None) -> Case[OpportunityStage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_opportunity_invalid_response, '__module__', "dex_connectors.lever.levermock")

def get_opportunity_defect(value: OpportunityStage, failure: Failure | None = None) -> Case[OpportunityStage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_opportunity_defect, '__module__', "dex_connectors.lever.levermock")

def get_opportunity_retry(failure: Failure, after_seconds: float = 0) -> Case[OpportunityStage]:
    return Case.retry(failure, after_seconds)

setattr(get_opportunity_retry, '__module__', "dex_connectors.lever.levermock")

def get_opportunity_found_default() -> Case[OpportunityStage]:
    return Case.branch_result(FOUND, OpportunityStage(id='250d8f03-738a-4bba-a671-8a3d73477145', name='Sample Candidate', stage='00922a60-7c15-422b-b086-f62000824fd7'))

setattr(get_opportunity_found_default, '__module__', "dex_connectors.lever.levermock")


class Mock:

    def __init__(self, connection_name: str = "lever") -> None:
        self.get_opportunity_script: Query[GetOpportunityInput, OpportunityStage] = Query(GET_OPPORTUNITY_DEFINITION)
        self.get_opportunity_script.default(get_opportunity_found_default())
        self._connection = new_connection_with_operations(
            get_opportunity=self.get_opportunity_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.lever.levermock")

__all__ = [
    "Mock",
    "get_opportunity_found",
    "get_opportunity_not_found",
    "get_opportunity_provider_rejected",
    "get_opportunity_invalid_response",
    "get_opportunity_defect",
    "get_opportunity_retry",
    "get_opportunity_found_default",
]
