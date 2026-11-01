# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import EmbedInput as EmbedInput
from ._generated.models import EmbedOutput as EmbedOutput
from ._generated.models import (
    RankedDocument as RankedDocument,
)
from ._generated.models import RerankInput as RerankInput
from ._generated.models import RerankOutput as RerankOutput

EmbedInput.__doc__ = (
    "Model, 1 to 96 nonblank texts, and an input type from INPUT_TYPES, such as search_query."
)
EmbedOutput.__doc__ = "Cohere response ID and one float vector per input text, in input order."
RerankInput.__doc__ = (
    "Model, query, 1 to 1000 nonblank documents, and an optional top_n no larger than the document count."
)
RerankOutput.__doc__ = "Documents in Cohere's ranked order."

RankedDocument.__doc__ = "Input document index and its relevance score."
