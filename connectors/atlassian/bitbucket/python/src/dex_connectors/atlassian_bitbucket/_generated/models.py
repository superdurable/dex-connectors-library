# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass

@dataclass(frozen=True, slots=True)
class ListRepositoriesInput:
    """Typed ListRepositoriesInput value from the Go connector contract."""

    workspace: str = ''


setattr(ListRepositoriesInput, '__module__', 'dex_connectors.atlassian_bitbucket.models')


@dataclass(frozen=True, slots=True)
class Repository:
    """Typed Repository value from the Go connector contract."""

    uuid: str = ''
    name: str = ''
    full_name: str = ''
    is_private: bool = False


setattr(Repository, '__module__', 'dex_connectors.atlassian_bitbucket.models')


@dataclass(frozen=True, slots=True)
class RepositoryPage:
    """Typed RepositoryPage value from the Go connector contract."""

    values: tuple[Repository, ...] | None = None
    next: str = ''
    page: int = 0
    pagelen: int = 0


setattr(RepositoryPage, '__module__', 'dex_connectors.atlassian_bitbucket.models')


@dataclass(frozen=True, slots=True)
class ListPullRequestsInput:
    """Typed ListPullRequestsInput value from the Go connector contract."""

    workspace: str = ''
    repository: str = ''


setattr(ListPullRequestsInput, '__module__', 'dex_connectors.atlassian_bitbucket.models')


@dataclass(frozen=True, slots=True)
class PullRequest:
    """Typed PullRequest value from the Go connector contract."""

    id: int = 0
    title: str = ''
    state: str = ''


setattr(PullRequest, '__module__', 'dex_connectors.atlassian_bitbucket.models')


@dataclass(frozen=True, slots=True)
class PullRequestPage:
    """Typed PullRequestPage value from the Go connector contract."""

    values: tuple[PullRequest, ...] | None = None
    next: str = ''
    page: int = 0
    pagelen: int = 0


setattr(PullRequestPage, '__module__', 'dex_connectors.atlassian_bitbucket.models')
