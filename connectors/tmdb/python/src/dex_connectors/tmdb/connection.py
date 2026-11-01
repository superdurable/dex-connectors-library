# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""TMDB connection and project credential binding."""

from typing import Any

import httpx
from dex_connectors.sdk import Call, ConnectionRef, Query, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from .client import (
    CONNECTOR_ID,
    GET_MOVIE_DEFINITION,
    SEARCH_MOVIES_DEFINITION,
    Client,
    Config,
    Credentials,
)
from .models import GetMovieInput, Movie, MoviePage, SearchMoviesInput


class Connection:
    """Nonserializable TMDB operation bindings for a named project connection."""

    __slots__ = ("_search_movies", "_get_movie", "_client", "reference")

    def __init__(
        self,
        reference: ConnectionRef,
        search_movies: Query[SearchMoviesInput, MoviePage],
        get_movie: Query[GetMovieInput, Movie],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or search_movies is None or get_movie is None:
            raise ValueError("TMDB connection provider and operations are required")
        if (
            search_movies.definition().operation != SEARCH_MOVIES_DEFINITION.operation
            or get_movie.definition().operation != GET_MOVIE_DEFINITION.operation
        ):
            raise ValueError("TMDB connection operation identity differs")
        self.reference = reference
        self._search_movies = search_movies
        self._get_movie = get_movie
        self._client = client

    def search_movies(self) -> Query[SearchMoviesInput, MoviePage]:
        """Return the movie search Query used by an application Step."""
        return self._search_movies

    def get_movie(self) -> Query[GetMovieInput, Movie]:
        """Return the movie lookup Query used by an application Step."""
        return self._get_movie

    async def aclose(self) -> None:
        """Close the owned provider transport after Worker shutdown."""
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        """Keep provider transport and credentials out of logs."""
        return "tmdb.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        """Prevent a live connection from entering durable Flow state."""
        raise TypeError("TMDB connections cannot be serialized")


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind a client to an explicit logical connection reference."""
    if client is None:
        raise ValueError("TMDB client is required")
    return Connection(reference, client.search_movies(), client.get_movie(), client)


def new_connection_with_operations(
    search_movies: Query[SearchMoviesInput, MoviePage],
    get_movie: Query[GetMovieInput, Movie],
    reference: ConnectionRef,
) -> Connection:
    """Bind mock operations to the same connection identity as the real provider."""
    return Connection(reference, search_movies, get_movie)


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        value = material.get("api_key")
        if not isinstance(value, str) or not value:
            raise ValueError("TMDB credential api_key is required")
        return Credentials(SecretString(value))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Open declared ordinary settings and resolve the current private key per call."""
    if project is None:
        raise ValueError("TMDB connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("TMDB project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("TMDB project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("TMDB configuration has unknown fields")
    base_url = value.get("baseUrl", "")
    timeout = value.get("timeout", 0)
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not isinstance(base_url, str):
        raise ValueError("TMDB configuration baseUrl must be a string")
    if type(timeout) is not int or type(max_response_bytes) is not int:
        raise ValueError("TMDB configuration timeout and maxResponseBytes must be integers")
    return Config(base_url, timeout / 1_000_000_000, max_response_bytes)
