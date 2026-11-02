# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Case as _Case
from dex_connectors.sdk.connectormock import Mutation as Mutation
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import get_project_found as get_project_found
from .._generated.mock import get_project_found_default as _get_project_found_default
from .._generated.mock import get_project_not_found as get_project_not_found
from .._generated.mock import list_issues_listed as list_issues_listed
from .._generated.mock import list_issues_listed_default as _list_issues_listed_default
from .._generated.mock import list_issues_not_found as list_issues_not_found
from .._generated.mock import update_issue_labels_not_found as update_issue_labels_not_found
from .._generated.mock import update_issue_labels_updated as update_issue_labels_updated
from .._generated.mock import update_issue_labels_updated_default as _update_issue_labels_updated_default
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import FOUND as FOUND
from ..client import GET_PROJECT_DEFINITION as GET_PROJECT_DEFINITION
from ..client import LIST_ISSUES_DEFINITION as LIST_ISSUES_DEFINITION
from ..client import LISTED as LISTED
from ..client import NOT_FOUND as NOT_FOUND
from ..client import UPDATE_ISSUE_LABELS_DEFINITION as UPDATE_ISSUE_LABELS_DEFINITION
from ..client import UPDATED as UPDATED
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import GetProjectInput as GetProjectInput
from ..models import Issue as Issue
from ..models import Issue as _Issue
from ..models import IssuesPage as IssuesPage
from ..models import IssuesPage as _IssuesPage
from ..models import ListIssuesInput as ListIssuesInput
from ..models import Project as Project
from ..models import Project as _Project
from ..models import UpdateIssueLabelsInput as UpdateIssueLabelsInput


def get_project_default_answer() -> _Case[_Project]:
    """Return the manifest's getProject default answer."""
    return _get_project_found_default()


def list_issues_default_answer() -> _Case[_IssuesPage]:
    """Return the manifest's listIssues default answer."""
    return _list_issues_listed_default()


def update_issue_labels_default_answer() -> _Case[_Issue]:
    """Return the manifest's updateIssueLabels default answer."""
    return _update_issue_labels_updated_default()


__all__ = [
    "Mock",
    "get_project_found",
    "get_project_not_found",
    "get_project_default_answer",
    "list_issues_listed",
    "list_issues_not_found",
    "list_issues_default_answer",
    "update_issue_labels_updated",
    "update_issue_labels_not_found",
    "update_issue_labels_default_answer",
]

Mock.__doc__ = "Script GitLab operations, defaulting to their manifest answers."

get_project_found.__doc__ = "Script the found route."
get_project_not_found.__doc__ = "Script a missing or inaccessible project."
list_issues_listed.__doc__ = "Script the listed route."
list_issues_not_found.__doc__ = "Script a missing or inaccessible project."
update_issue_labels_updated.__doc__ = "Script the updated route."
update_issue_labels_not_found.__doc__ = "Script a missing or inaccessible issue."
