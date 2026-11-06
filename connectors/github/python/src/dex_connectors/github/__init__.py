# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa: F403, F405
"""Dex-native GitHub connector."""

from .client import Client as Client
from .client import Config as Config
from .client import Credentials as Credentials
from .connection import Connection as Connection
from .connection import new_connection as new_connection
from .connection import new_connection_with_operations as new_connection_with_operations
from .connection import new_project_connection as new_project_connection
from .models import *  # noqa: F403
from .steps import *  # noqa: F403

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "GetAuthenticatedProfileInput",
    "AuthenticatedProfile",
    "ListPublicRepositoriesInput",
    "Repository",
    "PublicRepositories",
    "ListMergedPullRequestsInput",
    "MergedPullRequest",
    "MergedPullRequestPage",
    "ListPullRequestFilesInput",
    "PullRequestFile",
    "PullRequestFilePage",
    "ListReleasesInput",
    "Release",
    "ReleasePage",
    "ListCommitsInput",
    "CommitSummary",
    "CommitPage",
    "new_get_authenticated_profile_step",
    "new_list_public_repositories_step",
    "new_list_merged_pull_requests_step",
    "new_list_pull_request_files_step",
    "new_list_releases_step",
    "new_list_commits_step",
]
