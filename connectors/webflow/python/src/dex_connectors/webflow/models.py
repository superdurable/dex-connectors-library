# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    GetCollectionItemInput as GetCollectionItemInput,
)
from ._generated.models import (
    GetCollectionItemOutput as GetCollectionItemOutput,
)
from ._generated.models import (
    ListCollectionItemsInput as ListCollectionItemsInput,
)
from ._generated.models import (
    ListCollectionItemsOutput as ListCollectionItemsOutput,
)

ListCollectionItemsInput.__doc__ = "Collection ID, an optional limit of at most 100, and an optional offset."
ListCollectionItemsOutput.__doc__ = "The provider's collection item page JSON, kept untouched."
GetCollectionItemInput.__doc__ = "Collection and item IDs, each 24 hexadecimal characters."
GetCollectionItemOutput.__doc__ = "The provider's collection item JSON, kept untouched."
