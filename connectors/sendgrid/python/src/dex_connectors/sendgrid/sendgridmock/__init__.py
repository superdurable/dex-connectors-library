# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed SendGrid scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import CONNECTOR_ID, FOUND, GET_STATS_DEFINITION, LIST_TEMPLATES_DEFINITION, LISTED
from ..connection import Connection, new_connection_with_operations
from ..models import GetStatsInput, ListTemplatesInput, StatsDay, StatsOutput, Template, TemplatesOutput


def get_stats_found(value: StatsOutput, failure: Failure | None = None) -> Case[StatsOutput]:
    """Script the found route."""
    return Case.branch_result(FOUND, value, failure)


def get_stats_default_answer() -> Case[StatsOutput]:
    """Return the manifest's getStats default answer."""
    return get_stats_found(StatsOutput((StatsDay("2026-10-27", 8, 7),)))


def list_templates_listed(value: TemplatesOutput, failure: Failure | None = None) -> Case[TemplatesOutput]:
    """Script the listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_templates_default_answer() -> Case[TemplatesOutput]:
    """Return the manifest's listTemplates default answer."""
    return list_templates_listed(TemplatesOutput((Template("d-meridian", "Meridian receipt", "dynamic"),), 1))


class Mock:
    """Script SendGrid operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "sendgrid") -> None:
        self.get_stats_script: Query[GetStatsInput, StatsOutput] = Query(GET_STATS_DEFINITION)
        self.get_stats_script.default(get_stats_default_answer())
        self.list_templates_script: Query[ListTemplatesInput, TemplatesOutput] = Query(
            LIST_TEMPLATES_DEFINITION
        )
        self.list_templates_script.default(list_templates_default_answer())
        self._connection = new_connection_with_operations(
            self.get_stats_script, self.list_templates_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "get_stats_found",
    "get_stats_default_answer",
    "list_templates_listed",
    "list_templates_default_answer",
]
