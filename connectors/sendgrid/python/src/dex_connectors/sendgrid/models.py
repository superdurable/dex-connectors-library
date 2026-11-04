# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    GetStatsInput as GetStatsInput,
)
from ._generated.models import (
    ListTemplatesInput as ListTemplatesInput,
)
from ._generated.models import (
    StatsDay as StatsDay,
)
from ._generated.models import StatsOutput as StatsOutput
from ._generated.models import (
    Template as Template,
)
from ._generated.models import TemplatesOutput as TemplatesOutput

StatsOutput.__doc__ = "The returned daily global email statistics."
TemplatesOutput.__doc__ = (
    "One template page, the provider-reported count, and the next page token when supplied."
)

GetStatsInput.__doc__ = "Inclusive YYYY-MM-DD date range; a blank end date means today."
StatsDay.__doc__ = "One YYYY-MM-DD reporting date with its email request and delivered counts."
ListTemplatesInput.__doc__ = (
    "Generation (dynamic or legacy; blank is dynamic), page size 1-200 (zero is 100), and page token."
)
Template.__doc__ = "A transactional template's provider ID, name, and generation."
