# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListOrganizationsInput, ListOrganizationsOutput, OrganizationSummary
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_ORGANIZATIONS_DEFINITION, PROVIDER_REJECTED

def list_organizations_listed(value: ListOrganizationsOutput, failure: Failure | None = None) -> Case[ListOrganizationsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_organizations_listed, '__module__', "dex_connectors.suxin.suxinmock")

def list_organizations_provider_rejected(value: ListOrganizationsOutput, failure: Failure | None = None) -> Case[ListOrganizationsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_organizations_provider_rejected, '__module__', "dex_connectors.suxin.suxinmock")

def list_organizations_invalid_response(value: ListOrganizationsOutput, failure: Failure | None = None) -> Case[ListOrganizationsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_organizations_invalid_response, '__module__', "dex_connectors.suxin.suxinmock")

def list_organizations_defect(value: ListOrganizationsOutput, failure: Failure | None = None) -> Case[ListOrganizationsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_organizations_defect, '__module__', "dex_connectors.suxin.suxinmock")

def list_organizations_retry(failure: Failure, after_seconds: float = 0) -> Case[ListOrganizationsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_organizations_retry, '__module__', "dex_connectors.suxin.suxinmock")

def list_organizations_sample_organizations() -> Case[ListOrganizationsOutput]:
    return Case.branch_result(LISTED, ListOrganizationsOutput(organizations=(OrganizationSummary(id='1001', name='Example Organization', entity_type='COMPANY', certify_status='VERIFIED'),)))

setattr(list_organizations_sample_organizations, '__module__', "dex_connectors.suxin.suxinmock")


class Mock:

    def __init__(self, connection_name: str = "suxin") -> None:
        self.list_organizations_script: Query[ListOrganizationsInput, ListOrganizationsOutput] = Query(LIST_ORGANIZATIONS_DEFINITION)
        self.list_organizations_script.default(list_organizations_sample_organizations())
        self._connection = new_connection_with_operations(
            list_organizations=self.list_organizations_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.suxin.suxinmock")

__all__ = [
    "Mock",
    "list_organizations_listed",
    "list_organizations_provider_rejected",
    "list_organizations_invalid_response",
    "list_organizations_defect",
    "list_organizations_retry",
    "list_organizations_sample_organizations",
]
