# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass, field

from ._generated.models import (
    Domain as Domain,
)
from ._generated.models import (
    Event as Event,
)
from ._generated.models import (
    GetEventsInput as GetEventsInput,
)
from ._generated.models import (
    ListDomainsInput as ListDomainsInput,
)


@dataclass(frozen=True, slots=True)
class EventsOutput:
    """Events on the first page."""

    events: tuple[Event, ...] = ()


@dataclass(frozen=True, slots=True)
class DomainsOutput:
    """Domains on the page and Mailgun's total domain count."""

    domains: tuple[Domain, ...] = ()
    total_count: int = field(default=0, metadata={"dex_json_name": "totalCount"})
