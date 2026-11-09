# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetResponseInput, ListSurveysInput, ResponseMetadata, SurveyMetadata, SurveysPage
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_RESPONSE_DEFINITION, INVALID_RESPONSE, LISTED, LIST_SURVEYS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_surveys_listed(value: SurveysPage, failure: Failure | None = None) -> Case[SurveysPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_surveys_listed, '__module__', "dex_connectors.surveymonkey.surveymonkeymock")

def list_surveys_provider_rejected(value: SurveysPage, failure: Failure | None = None) -> Case[SurveysPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_surveys_provider_rejected, '__module__', "dex_connectors.surveymonkey.surveymonkeymock")

def list_surveys_invalid_response(value: SurveysPage, failure: Failure | None = None) -> Case[SurveysPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_surveys_invalid_response, '__module__', "dex_connectors.surveymonkey.surveymonkeymock")

def list_surveys_defect(value: SurveysPage, failure: Failure | None = None) -> Case[SurveysPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_surveys_defect, '__module__', "dex_connectors.surveymonkey.surveymonkeymock")

def list_surveys_retry(failure: Failure, after_seconds: float = 0) -> Case[SurveysPage]:
    return Case.retry(failure, after_seconds)

setattr(list_surveys_retry, '__module__', "dex_connectors.surveymonkey.surveymonkeymock")

def list_surveys_listed_default() -> Case[SurveysPage]:
    return Case.branch_result(LISTED, SurveysPage(surveys=(SurveyMetadata(id='123456789', title='Customer feedback'),), page=1, per_page=50, total=1))

setattr(list_surveys_listed_default, '__module__', "dex_connectors.surveymonkey.surveymonkeymock")

def get_response_found(value: ResponseMetadata, failure: Failure | None = None) -> Case[ResponseMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_response_found, '__module__', "dex_connectors.surveymonkey.surveymonkeymock")

def get_response_not_found(value: ResponseMetadata, failure: Failure | None = None) -> Case[ResponseMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_response_not_found, '__module__', "dex_connectors.surveymonkey.surveymonkeymock")

def get_response_provider_rejected(value: ResponseMetadata, failure: Failure | None = None) -> Case[ResponseMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_response_provider_rejected, '__module__', "dex_connectors.surveymonkey.surveymonkeymock")

def get_response_invalid_response(value: ResponseMetadata, failure: Failure | None = None) -> Case[ResponseMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_response_invalid_response, '__module__', "dex_connectors.surveymonkey.surveymonkeymock")

def get_response_defect(value: ResponseMetadata, failure: Failure | None = None) -> Case[ResponseMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_response_defect, '__module__', "dex_connectors.surveymonkey.surveymonkeymock")

def get_response_retry(failure: Failure, after_seconds: float = 0) -> Case[ResponseMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_response_retry, '__module__', "dex_connectors.surveymonkey.surveymonkeymock")

def get_response_found_default() -> Case[ResponseMetadata]:
    return Case.branch_result(FOUND, ResponseMetadata(id='987654321', survey_id='123456789', status='completed', created_at='2026-11-07T10:00:00+00:00', modified_at='2026-11-07T10:01:00+00:00'))

setattr(get_response_found_default, '__module__', "dex_connectors.surveymonkey.surveymonkeymock")


class Mock:

    def __init__(self, connection_name: str = "surveymonkey") -> None:
        self.list_surveys_script: Query[ListSurveysInput, SurveysPage] = Query(LIST_SURVEYS_DEFINITION)
        self.list_surveys_script.default(list_surveys_listed_default())
        self.get_response_script: Query[GetResponseInput, ResponseMetadata] = Query(GET_RESPONSE_DEFINITION)
        self.get_response_script.default(get_response_found_default())
        self._connection = new_connection_with_operations(
            list_surveys=self.list_surveys_script,
            get_response=self.get_response_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.surveymonkey.surveymonkeymock")

__all__ = [
    "Mock",
    "list_surveys_listed",
    "list_surveys_provider_rejected",
    "list_surveys_invalid_response",
    "list_surveys_defect",
    "list_surveys_retry",
    "list_surveys_listed_default",
    "get_response_found",
    "get_response_not_found",
    "get_response_provider_rejected",
    "get_response_invalid_response",
    "get_response_defect",
    "get_response_retry",
    "get_response_found_default",
]
