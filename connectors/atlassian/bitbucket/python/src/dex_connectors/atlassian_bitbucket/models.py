# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Bitbucket repository and pull request pages."""

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class ListRepositoriesInput:
    """Bitbucket workspace slug or UUID."""

    workspace: str = ""


@dataclass(frozen=True, slots=True)
class Repository:
    """Repository UUID, name, workspace-qualified full name, and privacy flag."""

    uuid: str = ""
    name: str = ""
    full_name: str = ""
    is_private: bool = False


@dataclass(frozen=True, slots=True)
class RepositoryPage:
    """First page of repositories; next is Bitbucket's next-page URL or blank."""

    values: tuple[Repository, ...] = ()
    next: str = ""
    page: int = 0
    pagelen: int = 0


@dataclass(frozen=True, slots=True)
class ListPullRequestsInput:
    """Bitbucket workspace and repository slug."""

    workspace: str = ""
    repository: str = ""


@dataclass(frozen=True, slots=True)
class PullRequest:
    """Pull request number, title, and state such as OPEN."""

    id: int = 0
    title: str = ""
    state: str = ""


@dataclass(frozen=True, slots=True)
class PullRequestPage:
    """First page of pull requests; next is Bitbucket's next-page URL or blank."""

    values: tuple[PullRequest, ...] = ()
    next: str = ""
    page: int = 0
    pagelen: int = 0
