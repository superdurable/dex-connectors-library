# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""GitLab connection and project credential binding."""

from typing import Any as Any

import httpx as httpx
from dex_connectors.sdk import Call as Call
from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Mutation as Mutation
from dex_connectors.sdk import Query as Query
from dex_connectors.sdk import SecretString as SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey as ConnectionKey
from dex_connectors.sdk.projectconfig import LoadedProject as LoadedProject

from ._generated.connection import (
    Connection as Connection,
)
from ._generated.connection import (
    new_connection as new_connection,
)
from ._generated.connection import (
    new_connection_with_operations as new_connection_with_operations,
)
from ._generated.connection import (
    new_project_connection as new_project_connection,
)
from .client import CONNECTOR_ID as CONNECTOR_ID
from .client import GET_PROJECT_DEFINITION as GET_PROJECT_DEFINITION
from .client import LIST_ISSUES_DEFINITION as LIST_ISSUES_DEFINITION
from .client import UPDATE_ISSUE_LABELS_DEFINITION as UPDATE_ISSUE_LABELS_DEFINITION
from .client import Client as Client
from .client import Config as Config
from .client import Credentials as Credentials
from .models import GetProjectInput as GetProjectInput
from .models import Issue as Issue
from .models import IssuesPage as IssuesPage
from .models import ListIssuesInput as ListIssuesInput
from .models import Project as Project
from .models import UpdateIssueLabelsInput as UpdateIssueLabelsInput

Connection.__doc__ = "Nonserializable GitLab operation bindings for a named project connection."

new_connection.__doc__ = "Bind a client to an explicit logical connection reference."
new_connection_with_operations.__doc__ = (
    "Bind mock operations to the same connection identity as the real provider."
)
new_project_connection.__doc__ = (
    "Open declared ordinary settings and resolve the current access token per call."
)
