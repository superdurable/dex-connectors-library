# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AdAccount, AdAccountsPage, ListAdAccountsInput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_AD_ACCOUNTS_DEFINITION, PROVIDER_REJECTED

def list_ad_accounts_listed(value: AdAccountsPage, failure: Failure | None = None) -> Case[AdAccountsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_ad_accounts_listed, '__module__', "dex_connectors.linkedin_ads.adsmock")

def list_ad_accounts_provider_rejected(value: AdAccountsPage, failure: Failure | None = None) -> Case[AdAccountsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_ad_accounts_provider_rejected, '__module__', "dex_connectors.linkedin_ads.adsmock")

def list_ad_accounts_invalid_response(value: AdAccountsPage, failure: Failure | None = None) -> Case[AdAccountsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_ad_accounts_invalid_response, '__module__', "dex_connectors.linkedin_ads.adsmock")

def list_ad_accounts_defect(value: AdAccountsPage, failure: Failure | None = None) -> Case[AdAccountsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_ad_accounts_defect, '__module__', "dex_connectors.linkedin_ads.adsmock")

def list_ad_accounts_retry(failure: Failure, after_seconds: float = 0) -> Case[AdAccountsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_ad_accounts_retry, '__module__', "dex_connectors.linkedin_ads.adsmock")

def list_ad_accounts_listed_default() -> Case[AdAccountsPage]:
    return Case.branch_result(LISTED, AdAccountsPage(accounts=(AdAccount(id='507404993', name='Example Account'),), next_page_token=''))

setattr(list_ad_accounts_listed_default, '__module__', "dex_connectors.linkedin_ads.adsmock")


class Mock:

    def __init__(self, connection_name: str = "linkedin-ads") -> None:
        self.list_ad_accounts_script: Query[ListAdAccountsInput, AdAccountsPage] = Query(LIST_AD_ACCOUNTS_DEFINITION)
        self.list_ad_accounts_script.default(list_ad_accounts_listed_default())
        self._connection = new_connection_with_operations(
            list_ad_accounts=self.list_ad_accounts_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.linkedin_ads.adsmock")

__all__ = [
    "Mock",
    "list_ad_accounts_listed",
    "list_ad_accounts_provider_rejected",
    "list_ad_accounts_invalid_response",
    "list_ad_accounts_defect",
    "list_ad_accounts_retry",
    "list_ad_accounts_listed_default",
]
