# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk import FailureKind as FailureKind
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import embed_default_embedding as embed_default_embedding
from .._generated.mock import embed_invalid_key as embed_invalid_key
from .._generated.mock import rerank_default_ranking as rerank_default_ranking
from .._generated.mock import rerank_invalid_key as rerank_invalid_key
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import EMBED_DEFINITION as EMBED_DEFINITION
from ..client import EMBEDDED as EMBEDDED
from ..client import PROVIDER_REJECTED as PROVIDER_REJECTED
from ..client import RANKED as RANKED
from ..client import RERANK_DEFINITION as RERANK_DEFINITION
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import EmbedInput as EmbedInput
from ..models import EmbedOutput as EmbedOutput
from ..models import RankedDocument as RankedDocument
from ..models import RerankInput as RerankInput
from ..models import RerankOutput as RerankOutput

__all__ = [
    "Mock",
    "embed_default_embedding",
    "embed_invalid_key",
    "rerank_default_ranking",
    "rerank_invalid_key",
]

Mock.__doc__ = "Script Cohere Queries, defaulting to their manifest answers."

embed_default_embedding.__doc__ = "Return the manifest's embed defaultEmbedding answer."
embed_invalid_key.__doc__ = "Return the manifest's embed invalidKey answer."
rerank_default_ranking.__doc__ = "Return the manifest's rerank defaultRanking answer."
rerank_invalid_key.__doc__ = "Return the manifest's rerank invalidKey answer."
