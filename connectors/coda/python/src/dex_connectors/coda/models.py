# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    GetRowInput as GetRowInput,
)
from ._generated.models import (
    GetRowOutput as GetRowOutput,
)
from ._generated.models import (
    ListRowsInput as ListRowsInput,
)
from ._generated.models import (
    ListRowsOutput as ListRowsOutput,
)

ListRowsInput.__doc__ = "Doc and table IDs, an optional row limit of at most 500, and an optional page token."
ListRowsOutput.__doc__ = "The provider's row page JSON, kept untouched."
GetRowInput.__doc__ = "Doc, table, and row IDs."
GetRowOutput.__doc__ = "The provider's row JSON, kept untouched."
