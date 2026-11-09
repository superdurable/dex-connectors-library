# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetSubscriptionInput, SubscriptionStatus
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_SUBSCRIPTION_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_subscription_found(value: SubscriptionStatus, failure: Failure | None = None) -> Case[SubscriptionStatus]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_subscription_found, '__module__', "dex_connectors.chargebee.chargebeemock")

def get_subscription_not_found(value: SubscriptionStatus, failure: Failure | None = None) -> Case[SubscriptionStatus]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_subscription_not_found, '__module__', "dex_connectors.chargebee.chargebeemock")

def get_subscription_provider_rejected(value: SubscriptionStatus, failure: Failure | None = None) -> Case[SubscriptionStatus]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_subscription_provider_rejected, '__module__', "dex_connectors.chargebee.chargebeemock")

def get_subscription_invalid_response(value: SubscriptionStatus, failure: Failure | None = None) -> Case[SubscriptionStatus]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_subscription_invalid_response, '__module__', "dex_connectors.chargebee.chargebeemock")

def get_subscription_defect(value: SubscriptionStatus, failure: Failure | None = None) -> Case[SubscriptionStatus]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_subscription_defect, '__module__', "dex_connectors.chargebee.chargebeemock")

def get_subscription_retry(failure: Failure, after_seconds: float = 0) -> Case[SubscriptionStatus]:
    return Case.retry(failure, after_seconds)

setattr(get_subscription_retry, '__module__', "dex_connectors.chargebee.chargebeemock")

def get_subscription_found_default() -> Case[SubscriptionStatus]:
    return Case.branch_result(FOUND, SubscriptionStatus(id='sub_123456', currency_code='USD', status='active'))

setattr(get_subscription_found_default, '__module__', "dex_connectors.chargebee.chargebeemock")


class Mock:

    def __init__(self, connection_name: str = "chargebee") -> None:
        self.get_subscription_script: Query[GetSubscriptionInput, SubscriptionStatus] = Query(GET_SUBSCRIPTION_DEFINITION)
        self.get_subscription_script.default(get_subscription_found_default())
        self._connection = new_connection_with_operations(
            get_subscription=self.get_subscription_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.chargebee.chargebeemock")

__all__ = [
    "Mock",
    "get_subscription_found",
    "get_subscription_not_found",
    "get_subscription_provider_rejected",
    "get_subscription_invalid_response",
    "get_subscription_defect",
    "get_subscription_retry",
    "get_subscription_found_default",
]
