# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Sentry issue inputs, issues, and issue pages."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class ListIssuesInput:
    """Organization slug and optional Sentry search query, such as is:unresolved."""

    organization: str = ""
    query: str = ""


@dataclass(frozen=True, slots=True)
class GetIssueInput:
    """Organization slug and numeric issue ID text."""

    organization: str = ""
    id: str = ""


@dataclass(frozen=True, slots=True)
class UpdateIssueStatusInput:
    """Organization slug, issue ID, and status: resolved, unresolved, or ignored."""

    organization: str = ""
    id: str = ""
    status: str = ""


@dataclass(frozen=True, slots=True)
class Issue:
    """Sentry issue summary; short_id is the project-scoped label such as APP-1."""

    id: str = ""
    short_id: str = field(default="", metadata={"dex_json_name": "shortId"})
    title: str = ""
    status: str = ""


@dataclass(frozen=True, slots=True)
class IssuePage:
    """Issues from Sentry's first result page for the query."""

    issues: tuple[Issue, ...] = ()
