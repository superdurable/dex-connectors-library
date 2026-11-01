# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Close scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..client import (
    CONNECTOR_ID,
    FOUND,
    GET_LEAD_DEFINITION,
    LIST_LEADS_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
)
from ..connection import Connection, new_connection_with_operations
from ..models import GetLeadInput, Lead, LeadPage, ListLeadsInput

_MERIDIAN = Lead("lead_meridian1", "Meridian Labs")


def list_leads_listed(value: LeadPage, failure: Failure | None = None) -> Case[LeadPage]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_leads_default_answer() -> Case[LeadPage]:
    """Return the manifest's listLeads defaultAnswer."""
    return list_leads_listed(LeadPage((_MERIDIAN,), False))


def list_leads_invalid_key() -> Case[LeadPage]:
    """Return the manifest's listLeads invalidKey answer."""
    return Case.branch_result(
        PROVIDER_REJECTED,
        LeadPage(),
        Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listLeads", "Close rejected the token."),
    )


def get_lead_found(value: Lead, failure: Failure | None = None) -> Case[Lead]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_lead_not_found(value: Lead, failure: Failure | None = None) -> Case[Lead]:
    """Script a missing lead."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_lead_default_answer() -> Case[Lead]:
    """Return the manifest's getLead defaultAnswer."""
    return get_lead_found(_MERIDIAN)


def get_lead_invalid_key() -> Case[Lead]:
    """Return the manifest's getLead invalidKey answer."""
    return Case.branch_result(
        PROVIDER_REJECTED,
        Lead(),
        Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "getLead", "Close rejected the token."),
    )


class Mock:
    """Script Close Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "close") -> None:
        self.list_leads_script: Query[ListLeadsInput, LeadPage] = Query(LIST_LEADS_DEFINITION)
        self.list_leads_script.default(list_leads_default_answer())
        self.get_lead_script: Query[GetLeadInput, Lead] = Query(GET_LEAD_DEFINITION)
        self.get_lead_script.default(get_lead_default_answer())
        self._connection = new_connection_with_operations(
            self.list_leads_script, self.get_lead_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "list_leads_listed",
    "list_leads_default_answer",
    "list_leads_invalid_key",
    "get_lead_found",
    "get_lead_not_found",
    "get_lead_default_answer",
    "get_lead_invalid_key",
]
