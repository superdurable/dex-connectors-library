# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetRepositoryInput identifies a repository by its GitCode URL path components.
@dataclass(frozen=True, slots=True)
class GetRepositoryInput:
    # Owner is the organization or user path before the slash.
    owner: str = ''
    # Repo is the repository path after the slash.
    repo: str = ''


setattr(GetRepositoryInput, '__module__', 'dex_connectors.gitcode.models')


# GetRepositoryOutput includes only repository identity and visibility, omitting members and creator details.
@dataclass(frozen=True, slots=True)
class GetRepositoryOutput:
    # ID is the GitCode repository ID.
    id: int = 0
    # FullName is the owner/repository path.
    full_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'fullName'})
    # Private indicates whether this repository is private.
    private: bool = False
    # DefaultBranch is the repository's default branch, if configured.
    default_branch: str = _dataclass_field(default='', metadata={'dex_json_name': 'defaultBranch'})


setattr(GetRepositoryOutput, '__module__', 'dex_connectors.gitcode.models')
