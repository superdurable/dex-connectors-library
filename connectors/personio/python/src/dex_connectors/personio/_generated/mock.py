# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetPersonInput, PersonProfile
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_PERSON_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_person_found(value: PersonProfile, failure: Failure | None = None) -> Case[PersonProfile]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_person_found, '__module__', "dex_connectors.personio.personiomock")

def get_person_not_found(value: PersonProfile, failure: Failure | None = None) -> Case[PersonProfile]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_person_not_found, '__module__', "dex_connectors.personio.personiomock")

def get_person_provider_rejected(value: PersonProfile, failure: Failure | None = None) -> Case[PersonProfile]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_person_provider_rejected, '__module__', "dex_connectors.personio.personiomock")

def get_person_invalid_response(value: PersonProfile, failure: Failure | None = None) -> Case[PersonProfile]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_person_invalid_response, '__module__', "dex_connectors.personio.personiomock")

def get_person_defect(value: PersonProfile, failure: Failure | None = None) -> Case[PersonProfile]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_person_defect, '__module__', "dex_connectors.personio.personiomock")

def get_person_retry(failure: Failure, after_seconds: float = 0) -> Case[PersonProfile]:
    return Case.retry(failure, after_seconds)

setattr(get_person_retry, '__module__', "dex_connectors.personio.personiomock")

def get_person_found_default() -> Case[PersonProfile]:
    return Case.branch_result(FOUND, PersonProfile(id='3003', first_name='Jane', last_name='Doe'))

setattr(get_person_found_default, '__module__', "dex_connectors.personio.personiomock")


class Mock:

    def __init__(self, connection_name: str = "personio") -> None:
        self.get_person_script: Query[GetPersonInput, PersonProfile] = Query(GET_PERSON_DEFINITION)
        self.get_person_script.default(get_person_found_default())
        self._connection = new_connection_with_operations(
            get_person=self.get_person_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.personio.personiomock")

__all__ = [
    "Mock",
    "get_person_found",
    "get_person_not_found",
    "get_person_provider_rejected",
    "get_person_invalid_response",
    "get_person_defect",
    "get_person_retry",
    "get_person_found_default",
]
