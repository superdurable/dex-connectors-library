# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CandidateProfile, GetCandidateInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_CANDIDATE_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_candidate_found(value: CandidateProfile, failure: Failure | None = None) -> Case[CandidateProfile]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_candidate_found, '__module__', "dex_connectors.greenhouse.greenhousemock")

def get_candidate_not_found(value: CandidateProfile, failure: Failure | None = None) -> Case[CandidateProfile]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_candidate_not_found, '__module__', "dex_connectors.greenhouse.greenhousemock")

def get_candidate_provider_rejected(value: CandidateProfile, failure: Failure | None = None) -> Case[CandidateProfile]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_candidate_provider_rejected, '__module__', "dex_connectors.greenhouse.greenhousemock")

def get_candidate_invalid_response(value: CandidateProfile, failure: Failure | None = None) -> Case[CandidateProfile]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_candidate_invalid_response, '__module__', "dex_connectors.greenhouse.greenhousemock")

def get_candidate_defect(value: CandidateProfile, failure: Failure | None = None) -> Case[CandidateProfile]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_candidate_defect, '__module__', "dex_connectors.greenhouse.greenhousemock")

def get_candidate_retry(failure: Failure, after_seconds: float = 0) -> Case[CandidateProfile]:
    return Case.retry(failure, after_seconds)

setattr(get_candidate_retry, '__module__', "dex_connectors.greenhouse.greenhousemock")

def get_candidate_found_default() -> Case[CandidateProfile]:
    return Case.branch_result(FOUND, CandidateProfile(id=53883394, first_name='Sample', last_name='Candidate'))

setattr(get_candidate_found_default, '__module__', "dex_connectors.greenhouse.greenhousemock")


class Mock:

    def __init__(self, connection_name: str = "greenhouse") -> None:
        self.get_candidate_script: Query[GetCandidateInput, CandidateProfile] = Query(GET_CANDIDATE_DEFINITION)
        self.get_candidate_script.default(get_candidate_found_default())
        self._connection = new_connection_with_operations(
            get_candidate=self.get_candidate_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.greenhouse.greenhousemock")

__all__ = [
    "Mock",
    "get_candidate_found",
    "get_candidate_not_found",
    "get_candidate_provider_rejected",
    "get_candidate_invalid_response",
    "get_candidate_defect",
    "get_candidate_retry",
    "get_candidate_found_default",
]
