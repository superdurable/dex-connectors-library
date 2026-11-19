# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetSMSBalanceInput, SMSBalance
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_SMS_BALANCE_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_sms_balance_found(value: SMSBalance, failure: Failure | None = None) -> Case[SMSBalance]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_sms_balance_found, '__module__', "dex_connectors.submail.submailmock")

def get_sms_balance_provider_rejected(value: SMSBalance, failure: Failure | None = None) -> Case[SMSBalance]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_sms_balance_provider_rejected, '__module__', "dex_connectors.submail.submailmock")

def get_sms_balance_invalid_response(value: SMSBalance, failure: Failure | None = None) -> Case[SMSBalance]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_sms_balance_invalid_response, '__module__', "dex_connectors.submail.submailmock")

def get_sms_balance_defect(value: SMSBalance, failure: Failure | None = None) -> Case[SMSBalance]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_sms_balance_defect, '__module__', "dex_connectors.submail.submailmock")

def get_sms_balance_retry(failure: Failure, after_seconds: float = 0) -> Case[SMSBalance]:
    return Case.retry(failure, after_seconds)

setattr(get_sms_balance_retry, '__module__', "dex_connectors.submail.submailmock")

def get_sms_balance_found_default() -> Case[SMSBalance]:
    return Case.branch_result(FOUND, SMSBalance(general_credits='9410', transactional_credits='9775'))

setattr(get_sms_balance_found_default, '__module__', "dex_connectors.submail.submailmock")


class Mock:

    def __init__(self, connection_name: str = "submail") -> None:
        self.get_sms_balance_script: Query[GetSMSBalanceInput, SMSBalance] = Query(GET_SMS_BALANCE_DEFINITION)
        self.get_sms_balance_script.default(get_sms_balance_found_default())
        self._connection = new_connection_with_operations(
            get_sms_balance=self.get_sms_balance_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.submail.submailmock")

__all__ = [
    "Mock",
    "get_sms_balance_found",
    "get_sms_balance_provider_rejected",
    "get_sms_balance_invalid_response",
    "get_sms_balance_defect",
    "get_sms_balance_retry",
    "get_sms_balance_found_default",
]
