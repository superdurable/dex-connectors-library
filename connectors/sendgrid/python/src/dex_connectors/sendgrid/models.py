# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass, field

from ._generated.models import (
    GetStatsInput as GetStatsInput,
)
from ._generated.models import (
    ListTemplatesInput as ListTemplatesInput,
)
from ._generated.models import (
    StatsDay as StatsDay,
)
from ._generated.models import (
    Template as Template,
)


@dataclass(frozen=True, slots=True)
class StatsOutput:
    """The returned daily global email statistics."""

    days: tuple[StatsDay, ...] = ()


@dataclass(frozen=True, slots=True)
class TemplatesOutput:
    """One template page, the provider-reported count, and the next page token when supplied."""

    templates: tuple[Template, ...] = ()
    count: int = 0
    next_page_token: str = field(
        default="", metadata={"dex_json_name": "nextPageToken", "dex_json_omit_default": True}
    )
