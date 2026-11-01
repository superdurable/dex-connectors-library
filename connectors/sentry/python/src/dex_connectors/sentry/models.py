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
