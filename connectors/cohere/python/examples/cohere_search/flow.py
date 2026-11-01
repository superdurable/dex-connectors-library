# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Cohere search Flow: embed the query, then rerank candidate documents."""

from dataclasses import dataclass

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.cohere import (
    Connection,
    EmbedInput,
    EmbedOutput,
    RankedDocument,
    RerankInput,
    RerankOutput,
    new_embed_step,
    new_rerank_step,
)

FLOW_TYPE = "CohereSearch"
CONNECTION_NAME = "cohere"
_EMBED_STEP_TYPE = "EmbedSearchQuery"
_RERANK_STEP_TYPE = "RerankSearchDocuments"
_ANNOTATION_GROUP = {"groupId": "cohere-search", "groupLabel": "Cohere search"}


@dataclass(frozen=True, slots=True)
class SearchRequest:
    """Search query and at least two candidate documents."""

    query: str
    documents: tuple[str, ...]


@dataclass(frozen=True, slots=True)
class SearchOutcome:
    """Candidate documents in Cohere's ranked order."""

    results: tuple[RankedDocument, ...] = ()


_REQUEST = dex.Attribute("cohere-search-request", SearchRequest)


def map_to_embed_input(request: SearchRequest) -> EmbedInput:
    """Embed the query as a search query."""
    return EmbedInput(model="embed-v4.0", texts=(request.query,), input_type="search_query")


def map_to_rerank_input(request: SearchRequest) -> RerankInput:
    """Rerank every candidate document against the query."""
    return RerankInput(model="rerank-v5.0-pro", query=request.query, documents=request.documents)


# dex:group group-id:cohere-search group-label:"Cohere search"
# dex:explanation text:"Check and store the search request before calling Cohere."
class _StartRequest(dex.Step[SearchRequest]):
    def get_step_type(self) -> str:
        return "StartCohereSearch"

    def execute(self, context: dex.Context, input: SearchRequest) -> dex.StepDecision:
        if not input.query.strip() or len(input.documents) < 2:
            return dex.force_fail("query and at least two documents are required")
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef(_EMBED_STEP_TYPE), input)


# dex:group group-id:cohere-search group-label:"Cohere search"
# dex:explanation text:"Continue from the query embedding to the rerank call."
class _AfterEmbedding(dex.Step[QueryResult[EmbedOutput]]):
    def get_step_type(self) -> str:
        return "AfterCohereEmbedding"

    def execute(self, context: dex.Context, input: QueryResult[EmbedOutput]) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_RERANK_STEP_TYPE), _REQUEST.get(context))


# dex:group group-id:cohere-search group-label:"Cohere search"
# dex:explanation text:"Complete with the ranked documents."
class _CompleteSearch(dex.Step[QueryResult[RerankOutput]]):
    def get_step_type(self) -> str:
        return "CompleteCohereSearch"

    def execute(self, context: dex.Context, input: QueryResult[RerankOutput]) -> dex.StepDecision:
        return dex.graceful_complete(SearchOutcome(input.value.results))


class SearchFlow(dex.Flow[SearchRequest]):
    """Embed a query and rerank documents through the static cohere connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Cohere search requires the static cohere connection")
        self._start = _StartRequest()
        self._after_embedding = _AfterEmbedding()
        self._complete = _CompleteSearch()
        self._embed = new_embed_step(
            step_type=_EMBED_STEP_TYPE,
            step_input_type=SearchRequest,
            annotations={**_ANNOTATION_GROUP, "explanation": "Embed the search query."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_embed_input,
            embedded=self._after_embedding,
        )
        self._rerank = new_rerank_step(
            step_type=_RERANK_STEP_TYPE,
            step_input_type=SearchRequest,
            annotations={**_ANNOTATION_GROUP, "explanation": "Rank the candidate documents."},
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_rerank_input,
            ranked=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[SearchRequest]:
        """Register the start, both Cohere calls, and their result Steps."""
        return dex.StepList.start_step(self._start).other_steps(
            self._embed, self._after_embedding, self._rerank, self._complete
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the search request between the two calls."""
        return dex.PersistenceSchema.of(_REQUEST)
