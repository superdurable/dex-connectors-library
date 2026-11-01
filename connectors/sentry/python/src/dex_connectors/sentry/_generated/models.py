# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class ListIssuesInput:
    """Typed ListIssuesInput value from the Go connector contract."""

    organization: str = ''
    query: str = ''


setattr(ListIssuesInput, '__module__', 'dex_connectors.sentry.models')


@dataclass(frozen=True, slots=True)
class Issue:
    """Typed Issue value from the Go connector contract."""

    id: str = ''
    short_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'shortId'})
    title: str = ''
    status: str = ''


setattr(Issue, '__module__', 'dex_connectors.sentry.models')


@dataclass(frozen=True, slots=True)
class IssuePage:
    """Typed IssuePage value from the Go connector contract."""

    issues: tuple[Issue, ...] | None = None


setattr(IssuePage, '__module__', 'dex_connectors.sentry.models')


@dataclass(frozen=True, slots=True)
class GetIssueInput:
    """Typed GetIssueInput value from the Go connector contract."""

    organization: str = ''
    id: str = ''


setattr(GetIssueInput, '__module__', 'dex_connectors.sentry.models')


@dataclass(frozen=True, slots=True)
class UpdateIssueStatusInput:
    """Typed UpdateIssueStatusInput value from the Go connector contract."""

    organization: str = ''
    id: str = ''
    status: str = ''


setattr(UpdateIssueStatusInput, '__module__', 'dex_connectors.sentry.models')
