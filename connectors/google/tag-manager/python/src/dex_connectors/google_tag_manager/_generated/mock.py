# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AccountsPage, ContainersPage, ListAccountsInput, ListContainersInput, ListTagsInput, TagManagerAccount, TagManagerContainer, TagManagerTag, TagsPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_ACCOUNTS_DEFINITION, LIST_CONTAINERS_DEFINITION, LIST_TAGS_DEFINITION, PROVIDER_REJECTED

def list_accounts_listed(value: AccountsPage, failure: Failure | None = None) -> Case[AccountsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_accounts_listed, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_accounts_provider_rejected(value: AccountsPage, failure: Failure | None = None) -> Case[AccountsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_accounts_provider_rejected, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_accounts_invalid_response(value: AccountsPage, failure: Failure | None = None) -> Case[AccountsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_accounts_invalid_response, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_accounts_defect(value: AccountsPage, failure: Failure | None = None) -> Case[AccountsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_accounts_defect, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_accounts_retry(failure: Failure, after_seconds: float = 0) -> Case[AccountsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_accounts_retry, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_accounts_listed_default() -> Case[AccountsPage]:
    return Case.branch_result(LISTED, AccountsPage(accounts=(TagManagerAccount(path='accounts/123456', name='Example Account'),), next_page_token=''))

setattr(list_accounts_listed_default, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_containers_listed(value: ContainersPage, failure: Failure | None = None) -> Case[ContainersPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_containers_listed, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_containers_provider_rejected(value: ContainersPage, failure: Failure | None = None) -> Case[ContainersPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_containers_provider_rejected, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_containers_invalid_response(value: ContainersPage, failure: Failure | None = None) -> Case[ContainersPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_containers_invalid_response, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_containers_defect(value: ContainersPage, failure: Failure | None = None) -> Case[ContainersPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_containers_defect, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_containers_retry(failure: Failure, after_seconds: float = 0) -> Case[ContainersPage]:
    return Case.retry(failure, after_seconds)

setattr(list_containers_retry, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_containers_listed_default() -> Case[ContainersPage]:
    return Case.branch_result(LISTED, ContainersPage(containers=(TagManagerContainer(path='accounts/123456/containers/987654', name='Example Container', public_id='GTM-EXAMPLE'),), next_page_token=''))

setattr(list_containers_listed_default, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_tags_listed(value: TagsPage, failure: Failure | None = None) -> Case[TagsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_tags_listed, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_tags_provider_rejected(value: TagsPage, failure: Failure | None = None) -> Case[TagsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_tags_provider_rejected, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_tags_invalid_response(value: TagsPage, failure: Failure | None = None) -> Case[TagsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_tags_invalid_response, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_tags_defect(value: TagsPage, failure: Failure | None = None) -> Case[TagsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_tags_defect, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_tags_retry(failure: Failure, after_seconds: float = 0) -> Case[TagsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_tags_retry, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

def list_tags_listed_default() -> Case[TagsPage]:
    return Case.branch_result(LISTED, TagsPage(tags=(TagManagerTag(path='accounts/123456/containers/987654/workspaces/1/tags/42', name='Analytics Tag', type='gaawe'),), next_page_token=''))

setattr(list_tags_listed_default, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")


class Mock:

    def __init__(self, connection_name: str = "google-tag-manager") -> None:
        self.list_accounts_script: Query[ListAccountsInput, AccountsPage] = Query(LIST_ACCOUNTS_DEFINITION)
        self.list_accounts_script.default(list_accounts_listed_default())
        self.list_containers_script: Query[ListContainersInput, ContainersPage] = Query(LIST_CONTAINERS_DEFINITION)
        self.list_containers_script.default(list_containers_listed_default())
        self.list_tags_script: Query[ListTagsInput, TagsPage] = Query(LIST_TAGS_DEFINITION)
        self.list_tags_script.default(list_tags_listed_default())
        self._connection = new_connection_with_operations(
            list_accounts=self.list_accounts_script,
            list_containers=self.list_containers_script,
            list_tags=self.list_tags_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_tag_manager.tagmanagermock")

__all__ = [
    "Mock",
    "list_accounts_listed",
    "list_accounts_provider_rejected",
    "list_accounts_invalid_response",
    "list_accounts_defect",
    "list_accounts_retry",
    "list_accounts_listed_default",
    "list_containers_listed",
    "list_containers_provider_rejected",
    "list_containers_invalid_response",
    "list_containers_defect",
    "list_containers_retry",
    "list_containers_listed_default",
    "list_tags_listed",
    "list_tags_provider_rejected",
    "list_tags_invalid_response",
    "list_tags_defect",
    "list_tags_retry",
    "list_tags_listed_default",
]
