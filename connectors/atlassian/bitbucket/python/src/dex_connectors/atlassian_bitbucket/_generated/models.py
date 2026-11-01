# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListRepositoriesInput identifies the workspace whose repositories are read.
@dataclass(frozen=True, slots=True)
class ListRepositoriesInput:
    # Workspace is the Bitbucket workspace slug.
    workspace: str = ''


setattr(ListRepositoriesInput, '__module__', 'dex_connectors.atlassian_bitbucket.models')


# Repository is a minimal Bitbucket repository summary.
@dataclass(frozen=True, slots=True)
class Repository:
    # UUID is the provider repository ID.
    uuid: str = ''
    # Name is its display name.
    name: str = ''
    # FullName combines workspace and repository slug.
    full_name: str = ''
    # IsPrivate reports repository visibility.
    is_private: bool = False


setattr(Repository, '__module__', 'dex_connectors.atlassian_bitbucket.models')


# RepositoryPage is one Bitbucket repository page.
@dataclass(frozen=True, slots=True)
class RepositoryPage:
    # Values contains repositories, possibly none.
    values: tuple[Repository, ...] = ()
    # Next is the provider's next page URL, if any; applications must treat it as untrusted.
    next: str = ''
    # Page is the one-based page number.
    page: int = 0
    # PageLength is the provider page length.
    pagelen: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'values', _replace_nil_slices(self.values, 1))


RepositoryPage.__annotations__['values'] = tuple[Repository, ...] | None
RepositoryPage.__dataclass_fields__['values'].type = tuple[Repository, ...] | None
setattr(RepositoryPage, '__module__', 'dex_connectors.atlassian_bitbucket.models')


# ListPullRequestsInput identifies a repository within a workspace.
@dataclass(frozen=True, slots=True)
class ListPullRequestsInput:
    # Workspace is the Bitbucket workspace slug.
    workspace: str = ''
    # Repository is the repository slug within the workspace.
    repository: str = ''


setattr(ListPullRequestsInput, '__module__', 'dex_connectors.atlassian_bitbucket.models')


# PullRequest is a minimal Bitbucket pull request summary.
@dataclass(frozen=True, slots=True)
class PullRequest:
    # ID is the repository-local numeric pull request ID.
    id: int = 0
    # Title is the pull request title.
    title: str = ''
    # State is the provider state such as OPEN.
    state: str = ''


setattr(PullRequest, '__module__', 'dex_connectors.atlassian_bitbucket.models')


# PullRequestPage is one Bitbucket pull request page.
@dataclass(frozen=True, slots=True)
class PullRequestPage:
    # Values contains pull requests, possibly none.
    values: tuple[PullRequest, ...] = ()
    # Next is the provider's next page URL, if any; applications must treat it as untrusted.
    next: str = ''
    # Page is the one-based page number.
    page: int = 0
    # PageLength is the provider page length.
    pagelen: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'values', _replace_nil_slices(self.values, 1))


PullRequestPage.__annotations__['values'] = tuple[PullRequest, ...] | None
PullRequestPage.__dataclass_fields__['values'].type = tuple[PullRequest, ...] | None
setattr(PullRequestPage, '__module__', 'dex_connectors.atlassian_bitbucket.models')
