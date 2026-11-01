# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

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
    values: tuple[Repository, ...] | None = None
    # Next is the provider's next page URL, if any; applications must treat it as untrusted.
    next: str = ''
    # Page is the one-based page number.
    page: int = 0
    # PageLength is the provider page length.
    pagelen: int = 0


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
    values: tuple[PullRequest, ...] | None = None
    # Next is the provider's next page URL, if any; applications must treat it as untrusted.
    next: str = ''
    # Page is the one-based page number.
    page: int = 0
    # PageLength is the provider page length.
    pagelen: int = 0


setattr(PullRequestPage, '__module__', 'dex_connectors.atlassian_bitbucket.models')
