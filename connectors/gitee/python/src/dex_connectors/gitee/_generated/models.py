# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetRepositoryInput selects one Gitee repository by its URL owner and repository path segments.
@dataclass(frozen=True, slots=True)
class GetRepositoryInput:
    # Owner is the organization, enterprise, or user path from the Gitee repository URL.
    owner: str = ''
    # Repo is the repository path from the Gitee repository URL.
    repo: str = ''


setattr(GetRepositoryInput, '__module__', 'dex_connectors.gitee.models')


# RepositoryMetadata contains stable public metadata returned by Gitee.
@dataclass(frozen=True, slots=True)
class RepositoryMetadata:
    # ID is Gitee's repository identifier.
    id: int = 0
    # FullName is the owner and repository path.
    full_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'fullName'})
    # Name is the repository name.
    name: str = ''
    # Description is the repository description, if any.
    description: str = ''
    # HTMLURL is the repository page URL.
    html_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'htmlUrl'})
    # Private reports whether the repository is private.
    private: bool = False
    # DefaultBranch is the default branch name.
    default_branch: str = _dataclass_field(default='', metadata={'dex_json_name': 'defaultBranch'})
    # StarsCount is the current star count.
    stars_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'starsCount'})
    # ForksCount is the current fork count.
    forks_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'forksCount'})


setattr(RepositoryMetadata, '__module__', 'dex_connectors.gitee.models')


# GetRepositoryOutput contains the requested repository metadata.
@dataclass(frozen=True, slots=True)
class GetRepositoryOutput:
    repository: RepositoryMetadata = _dataclass_field(default_factory=RepositoryMetadata)


setattr(GetRepositoryOutput, '__module__', 'dex_connectors.gitee.models')
