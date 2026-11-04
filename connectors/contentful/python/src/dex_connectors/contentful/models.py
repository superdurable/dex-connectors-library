# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    GetEntryInput as GetEntryInput,
)
from ._generated.models import (
    GetEntryOutput as GetEntryOutput,
)
from ._generated.models import (
    ListEntriesInput as ListEntriesInput,
)
from ._generated.models import (
    ListEntriesOutput as ListEntriesOutput,
)

GetEntryInput.__doc__ = "Space, environment, and entry IDs."
GetEntryOutput.__doc__ = "The provider's entry JSON, kept untouched."
ListEntriesInput.__doc__ = (
    "Space and environment IDs, an optional limit of at most 1000, and an optional skip."
)
ListEntriesOutput.__doc__ = "The provider's entry collection JSON, kept untouched."
