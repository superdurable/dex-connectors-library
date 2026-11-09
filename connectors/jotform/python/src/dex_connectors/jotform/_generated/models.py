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


# ListFormsInput selects a zero-based page of at most 100 forms.
@dataclass(frozen=True, slots=True)
class ListFormsInput:
    # Offset is the zero-based form offset; zero starts the first page.
    offset: int = 0
    # Limit is the maximum form count; zero defaults to 20.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListFormsInput, '__module__', 'dex_connectors.jotform.models')


# FormMetadata contains public form identity and status without questions or answers.
@dataclass(frozen=True, slots=True)
class FormMetadata:
    # ID is the provider form ID.
    id: str = ''
    # Title is the form's untrusted display title.
    title: str = ''
    # Status is the provider form status.
    status: str = ''


setattr(FormMetadata, '__module__', 'dex_connectors.jotform.models')


# FormsPage is one offset-based Jotform form page.
@dataclass(frozen=True, slots=True)
class FormsPage:
    # Forms contains the selected page.
    forms: tuple[FormMetadata, ...] = ()
    # Offset is the requested zero-based offset.
    offset: int = 0
    # Limit is the requested maximum count.
    limit: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'forms', _replace_nil_slices(self.forms, 1))


FormsPage.__annotations__['forms'] = tuple[FormMetadata, ...] | None
FormsPage.__dataclass_fields__['forms'].type = tuple[FormMetadata, ...] | None
setattr(FormsPage, '__module__', 'dex_connectors.jotform.models')


# GetSubmissionInput selects one numeric Jotform submission ID.
@dataclass(frozen=True, slots=True)
class GetSubmissionInput:
    # SubmissionID is the numeric ID from Jotform's submission URL or API.
    submission_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'submissionId'})


setattr(GetSubmissionInput, '__module__', 'dex_connectors.jotform.models')


# SubmissionMetadata omits answers, which may contain secrets or personal data.
@dataclass(frozen=True, slots=True)
class SubmissionMetadata:
    # ID is the stable submission ID.
    id: str = ''
    # FormID identifies the owning form.
    form_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'formId'})
    # Status is the provider submission status.
    status: str = ''
    # CreatedAt is Jotform's raw creation timestamp.
    created_at: str = _dataclass_field(default='', metadata={'dex_json_name': 'createdAt'})


setattr(SubmissionMetadata, '__module__', 'dex_connectors.jotform.models')
