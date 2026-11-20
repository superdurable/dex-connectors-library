# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CheckUserOnlineInput, CheckUserOnlineOutput
from .definitions import CONNECTOR_ID, CHECKED, CHECK_USER_ONLINE_DEFINITION, DEFECT, INVALID_RESPONSE, PROVIDER_REJECTED

def check_user_online_checked(value: CheckUserOnlineOutput, failure: Failure | None = None) -> Case[CheckUserOnlineOutput]:
    return Case.branch_result(CHECKED, value, failure)

setattr(check_user_online_checked, '__module__', "dex_connectors.rongcloud.rongcloudmock")

def check_user_online_provider_rejected(value: CheckUserOnlineOutput, failure: Failure | None = None) -> Case[CheckUserOnlineOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(check_user_online_provider_rejected, '__module__', "dex_connectors.rongcloud.rongcloudmock")

def check_user_online_invalid_response(value: CheckUserOnlineOutput, failure: Failure | None = None) -> Case[CheckUserOnlineOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(check_user_online_invalid_response, '__module__', "dex_connectors.rongcloud.rongcloudmock")

def check_user_online_defect(value: CheckUserOnlineOutput, failure: Failure | None = None) -> Case[CheckUserOnlineOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(check_user_online_defect, '__module__', "dex_connectors.rongcloud.rongcloudmock")

def check_user_online_retry(failure: Failure, after_seconds: float = 0) -> Case[CheckUserOnlineOutput]:
    return Case.retry(failure, after_seconds)

setattr(check_user_online_retry, '__module__', "dex_connectors.rongcloud.rongcloudmock")

def check_user_online_user_online() -> Case[CheckUserOnlineOutput]:
    return Case.branch_result(CHECKED, CheckUserOnlineOutput(is_online=True))

setattr(check_user_online_user_online, '__module__', "dex_connectors.rongcloud.rongcloudmock")


class Mock:

    def __init__(self, connection_name: str = "rongcloud") -> None:
        self.check_user_online_script: Query[CheckUserOnlineInput, CheckUserOnlineOutput] = Query(CHECK_USER_ONLINE_DEFINITION)
        self.check_user_online_script.default(check_user_online_user_online())
        self._connection = new_connection_with_operations(
            check_user_online=self.check_user_online_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.rongcloud.rongcloudmock")

__all__ = [
    "Mock",
    "check_user_online_checked",
    "check_user_online_provider_rejected",
    "check_user_online_invalid_response",
    "check_user_online_defect",
    "check_user_online_retry",
    "check_user_online_user_online",
]
