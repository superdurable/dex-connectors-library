# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import QueryVectorsInput as QueryVectorsInput
from ._generated.models import (
    QueryVectorsOutput as QueryVectorsOutput,
)
from ._generated.models import UpsertVectorsInput as UpsertVectorsInput
from ._generated.models import (
    UpsertVectorsOutput as UpsertVectorsOutput,
)
from ._generated.models import Vector as Vector

Vector.__doc__ = "One dense vector replaced by its stable ID, with optional string metadata."
UpsertVectorsInput.__doc__ = (
    "Namespace (blank uses Pinecone's default) and 1 through 1000 vectors with unique stable IDs."
)
QueryVectorsInput.__doc__ = (
    "Namespace, dense query vector, result limit from 1 through 10000, and whether to include metadata."
)

UpsertVectorsOutput.__doc__ = "The number of vectors Pinecone accepted."
QueryVectorsOutput.__doc__ = "Pinecone's query JSON with matches and namespace, kept untouched."
