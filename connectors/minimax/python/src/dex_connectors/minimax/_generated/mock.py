# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetTokenPlanRemainsInput, GetTokenPlanRemainsOutput, TokenPlanModelGroup
from .definitions import CONNECTOR_ID, DEFECT, FETCHED, GET_TOKEN_PLAN_REMAINS_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_token_plan_remains_fetched(value: GetTokenPlanRemainsOutput, failure: Failure | None = None) -> Case[GetTokenPlanRemainsOutput]:
    return Case.branch_result(FETCHED, value, failure)

setattr(get_token_plan_remains_fetched, '__module__', "dex_connectors.minimax.minimaxmock")

def get_token_plan_remains_provider_rejected(value: GetTokenPlanRemainsOutput, failure: Failure | None = None) -> Case[GetTokenPlanRemainsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_token_plan_remains_provider_rejected, '__module__', "dex_connectors.minimax.minimaxmock")

def get_token_plan_remains_invalid_response(value: GetTokenPlanRemainsOutput, failure: Failure | None = None) -> Case[GetTokenPlanRemainsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_token_plan_remains_invalid_response, '__module__', "dex_connectors.minimax.minimaxmock")

def get_token_plan_remains_defect(value: GetTokenPlanRemainsOutput, failure: Failure | None = None) -> Case[GetTokenPlanRemainsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_token_plan_remains_defect, '__module__', "dex_connectors.minimax.minimaxmock")

def get_token_plan_remains_retry(failure: Failure, after_seconds: float = 0) -> Case[GetTokenPlanRemainsOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_token_plan_remains_retry, '__module__', "dex_connectors.minimax.minimaxmock")

def get_token_plan_remains_sample_usage() -> Case[GetTokenPlanRemainsOutput]:
    return Case.branch_result(FETCHED, GetTokenPlanRemainsOutput(model_groups=(TokenPlanModelGroup(model_name='MiniMax-M*', interval_remaining_percent=85, weekly_remaining_percent=97, interval_reset_at_millis=1776373200000, weekly_reset_at_millis=1776614400000),)))

setattr(get_token_plan_remains_sample_usage, '__module__', "dex_connectors.minimax.minimaxmock")


class Mock:

    def __init__(self, connection_name: str = "minimax") -> None:
        self.get_token_plan_remains_script: Query[GetTokenPlanRemainsInput, GetTokenPlanRemainsOutput] = Query(GET_TOKEN_PLAN_REMAINS_DEFINITION)
        self.get_token_plan_remains_script.default(get_token_plan_remains_sample_usage())
        self._connection = new_connection_with_operations(
            get_token_plan_remains=self.get_token_plan_remains_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.minimax.minimaxmock")

__all__ = [
    "Mock",
    "get_token_plan_remains_fetched",
    "get_token_plan_remains_provider_rejected",
    "get_token_plan_remains_invalid_response",
    "get_token_plan_remains_defect",
    "get_token_plan_remains_retry",
    "get_token_plan_remains_sample_usage",
]
