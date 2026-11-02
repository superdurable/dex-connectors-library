# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Mutation as Mutation
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import get_issue_default as get_issue_default
from .._generated.mock import get_issue_found as get_issue_found
from .._generated.mock import get_issue_not_found as get_issue_not_found
from .._generated.mock import list_issues_default as list_issues_default
from .._generated.mock import list_issues_listed as list_issues_listed
from .._generated.mock import update_issue_status_default as update_issue_status_default
from .._generated.mock import update_issue_status_provider_rejected as update_issue_status_provider_rejected
from .._generated.mock import update_issue_status_updated as update_issue_status_updated
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import FOUND as FOUND
from ..client import GET_ISSUE_DEFINITION as GET_ISSUE_DEFINITION
from ..client import LIST_ISSUES_DEFINITION as LIST_ISSUES_DEFINITION
from ..client import LISTED as LISTED
from ..client import NOT_FOUND as NOT_FOUND
from ..client import PROVIDER_REJECTED as PROVIDER_REJECTED
from ..client import UPDATE_ISSUE_STATUS_DEFINITION as UPDATE_ISSUE_STATUS_DEFINITION
from ..client import UPDATED as UPDATED
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import GetIssueInput as GetIssueInput
from ..models import Issue as Issue
from ..models import IssuePage as IssuePage
from ..models import ListIssuesInput as ListIssuesInput
from ..models import UpdateIssueStatusInput as UpdateIssueStatusInput

__all__ = [
    "Mock",
    "list_issues_listed",
    "list_issues_default",
    "get_issue_found",
    "get_issue_not_found",
    "get_issue_default",
    "update_issue_status_updated",
    "update_issue_status_provider_rejected",
    "update_issue_status_default",
]

Mock.__doc__ = "Script Sentry operations, defaulting to their manifest answers."

list_issues_listed.__doc__ = "Script the successful listed route."
list_issues_default.__doc__ = "Return the manifest's listIssuesDefault answer."
get_issue_found.__doc__ = "Script the successful found route."
get_issue_not_found.__doc__ = "Script a missing issue."
get_issue_default.__doc__ = "Return the manifest's getIssueDefault answer."
update_issue_status_updated.__doc__ = "Script the successful updated route."
update_issue_status_provider_rejected.__doc__ = "Script a definite rejection of the status update."
update_issue_status_default.__doc__ = "Return the manifest's updateIssueStatusDefault answer."
