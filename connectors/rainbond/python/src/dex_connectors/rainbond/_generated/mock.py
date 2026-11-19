# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListTeamsInput, ListTeamsOutput, Team
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_TEAMS_DEFINITION, PROVIDER_REJECTED

def list_teams_listed(value: ListTeamsOutput, failure: Failure | None = None) -> Case[ListTeamsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_teams_listed, '__module__', "dex_connectors.rainbond.rainbondmock")

def list_teams_provider_rejected(value: ListTeamsOutput, failure: Failure | None = None) -> Case[ListTeamsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_teams_provider_rejected, '__module__', "dex_connectors.rainbond.rainbondmock")

def list_teams_invalid_response(value: ListTeamsOutput, failure: Failure | None = None) -> Case[ListTeamsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_teams_invalid_response, '__module__', "dex_connectors.rainbond.rainbondmock")

def list_teams_defect(value: ListTeamsOutput, failure: Failure | None = None) -> Case[ListTeamsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_teams_defect, '__module__', "dex_connectors.rainbond.rainbondmock")

def list_teams_retry(failure: Failure, after_seconds: float = 0) -> Case[ListTeamsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_teams_retry, '__module__', "dex_connectors.rainbond.rainbondmock")

def list_teams_sample_teams() -> Case[ListTeamsOutput]:
    return Case.branch_result(LISTED, ListTeamsOutput(teams=(Team(id='team-example-1', name='Example Team', active=True),), total=1))

setattr(list_teams_sample_teams, '__module__', "dex_connectors.rainbond.rainbondmock")


class Mock:

    def __init__(self, connection_name: str = "rainbond") -> None:
        self.list_teams_script: Query[ListTeamsInput, ListTeamsOutput] = Query(LIST_TEAMS_DEFINITION)
        self.list_teams_script.default(list_teams_sample_teams())
        self._connection = new_connection_with_operations(
            list_teams=self.list_teams_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.rainbond.rainbondmock")

__all__ = [
    "Mock",
    "list_teams_listed",
    "list_teams_provider_rejected",
    "list_teams_invalid_response",
    "list_teams_defect",
    "list_teams_retry",
    "list_teams_sample_teams",
]
