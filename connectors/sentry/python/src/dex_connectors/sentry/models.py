# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass

from ._generated.models import (
    GetIssueInput as GetIssueInput,
)
from ._generated.models import (
    Issue as Issue,
)
from ._generated.models import (
    ListIssuesInput as ListIssuesInput,
)
from ._generated.models import (
    UpdateIssueStatusInput as UpdateIssueStatusInput,
)


@dataclass(frozen=True, slots=True)
class IssuePage:
    """Issues from Sentry's first result page for the query."""

    issues: tuple[Issue, ...] = ()
