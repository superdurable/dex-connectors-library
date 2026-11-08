# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListSurveysInput selects a one-based page of at most 100 surveys.
@dataclass(frozen=True, slots=True)
class ListSurveysInput:
    # Page is one-based; zero defaults to the first page.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # PerPage is the maximum survey count; zero defaults to 50.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage', 'dex_json_omit_default': True})


setattr(ListSurveysInput, '__module__', 'dex_connectors.surveymonkey.models')


# SurveyMetadata contains survey identity without questions or answers.
@dataclass(frozen=True, slots=True)
class SurveyMetadata:
    # ID is the provider survey ID.
    id: str = ''
    # Title is the untrusted survey display title.
    title: str = ''


setattr(SurveyMetadata, '__module__', 'dex_connectors.surveymonkey.models')


# SurveysPage is one page of survey metadata.
@dataclass(frozen=True, slots=True)
class SurveysPage:
    # Surveys contains the selected page.
    surveys: tuple[SurveyMetadata, ...] = ()
    # Page is the one-based page number.
    page: int = 0
    # PerPage is the provider page size.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage'})
    # Total is the provider total survey count.
    total: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'surveys', _replace_nil_slices(self.surveys, 1))


SurveysPage.__annotations__['surveys'] = tuple[SurveyMetadata, ...] | None
SurveysPage.__dataclass_fields__['surveys'].type = tuple[SurveyMetadata, ...] | None
setattr(SurveysPage, '__module__', 'dex_connectors.surveymonkey.models')


# GetResponseInput selects one response within one survey.
@dataclass(frozen=True, slots=True)
class GetResponseInput:
    # SurveyID is the numeric survey ID.
    survey_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'surveyId'})
    # ResponseID is the numeric response ID.
    response_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'responseId'})


setattr(GetResponseInput, '__module__', 'dex_connectors.surveymonkey.models')


# ResponseMetadata omits answers and respondent personal data.
@dataclass(frozen=True, slots=True)
class ResponseMetadata:
    # ID is the stable response ID.
    id: str = ''
    # SurveyID identifies the owning survey.
    survey_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'surveyId'})
    # Status is the provider response status.
    status: str = ''
    # CreatedAt is the provider creation timestamp.
    created_at: str = _dataclass_field(default='', metadata={'dex_json_name': 'createdAt'})
    # ModifiedAt is the provider modification timestamp.
    modified_at: str = _dataclass_field(default='', metadata={'dex_json_name': 'modifiedAt'})


setattr(ResponseMetadata, '__module__', 'dex_connectors.surveymonkey.models')
