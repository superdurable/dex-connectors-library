# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass, field

from ._generated.models import (
    GetLeadInput as GetLeadInput,
)
from ._generated.models import (
    Lead as Lead,
)
from ._generated.models import (
    ListLeadsInput as ListLeadsInput,
)


@dataclass(frozen=True, slots=True)
class LeadPage:
    """One page of leads; has_more reports whether Close has another page."""

    leads: tuple[Lead, ...] = ()
    has_more: bool = field(default=False, metadata={"dex_json_name": "hasMore"})
