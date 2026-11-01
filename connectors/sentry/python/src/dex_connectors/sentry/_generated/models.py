# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return value
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListIssuesInput selects one organization's current issues.
@dataclass(frozen=True, slots=True)
class ListIssuesInput:
    # Organization is the Sentry organization slug.
    organization: str = ''
    # Query is the provider search expression.
    query: str = ''


setattr(ListIssuesInput, '__module__', 'dex_connectors.sentry.models')


# Issue contains selected issue metadata.
@dataclass(frozen=True, slots=True)
class Issue:
    # ID is the provider's stable resource identifier.
    id: str = ''
    # ShortID is the human-readable Sentry issue identifier.
    short_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'shortId'})
    # Title is the provider's display title.
    title: str = ''
    # Status is the provider's current status.
    status: str = ''


setattr(Issue, '__module__', 'dex_connectors.sentry.models')


# IssuePage is one organization issue response page.
@dataclass(frozen=True, slots=True)
class IssuePage:
    # Issues contains one Sentry issue response page.
    issues: tuple[Issue, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'issues', _replace_nil_slices(self.issues, 1))


IssuePage.__annotations__['issues'] = tuple[Issue, ...] | None
setattr(IssuePage, '__module__', 'dex_connectors.sentry.models')


# GetIssueInput identifies one issue within an organization.
@dataclass(frozen=True, slots=True)
class GetIssueInput:
    # Organization is the Sentry organization slug.
    organization: str = ''
    # ID is the provider's stable resource identifier.
    id: str = ''


setattr(GetIssueInput, '__module__', 'dex_connectors.sentry.models')


# UpdateIssueStatusInput sets one issue's absolute status.
@dataclass(frozen=True, slots=True)
class UpdateIssueStatusInput:
    # Organization is the Sentry organization slug.
    organization: str = ''
    # ID is the provider's stable resource identifier.
    id: str = ''
    # Status is the provider's current status.
    status: str = ''


setattr(UpdateIssueStatusInput, '__module__', 'dex_connectors.sentry.models')
