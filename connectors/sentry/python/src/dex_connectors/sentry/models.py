# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    GetIssueInput as GetIssueInput,
)
from ._generated.models import (
    Issue as Issue,
)
from ._generated.models import IssuePage as IssuePage
from ._generated.models import (
    ListIssuesInput as ListIssuesInput,
)
from ._generated.models import (
    UpdateIssueStatusInput as UpdateIssueStatusInput,
)

IssuePage.__doc__ = "Issues from Sentry's first result page for the query."

ListIssuesInput.__doc__ = "Organization slug and optional Sentry search query, such as is:unresolved."
GetIssueInput.__doc__ = "Organization slug and numeric issue ID text."
UpdateIssueStatusInput.__doc__ = "Organization slug, issue ID, and status: resolved, unresolved, or ignored."
Issue.__doc__ = "Sentry issue summary; short_id is the project-scoped label such as APP-1."
