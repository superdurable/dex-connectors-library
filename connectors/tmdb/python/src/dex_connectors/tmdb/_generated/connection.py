# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_MOVIE_DEFINITION, SEARCH_MOVIES_DEFINITION
from ..models import GetMovieInput, Movie, MoviePage, SearchMoviesInput

class Connection:

    __slots__ = ('_search_movies', '_get_movie', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        search_movies: Query[SearchMoviesInput, MoviePage],
        get_movie: Query[GetMovieInput, Movie],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or search_movies is None or get_movie is None:
            raise ValueError("TMDB connection provider and operations are required")
        if search_movies.definition().operation != SEARCH_MOVIES_DEFINITION.operation:
            raise ValueError("TMDB connection operation identity differs")
        if get_movie.definition().operation != GET_MOVIE_DEFINITION.operation:
            raise ValueError("TMDB connection operation identity differs")
        self.reference = reference
        self._search_movies = search_movies
        self._get_movie = get_movie
        self._client = client

    def search_movies(self) -> Query[SearchMoviesInput, MoviePage]:
        return self._search_movies

    def get_movie(self) -> Query[GetMovieInput, Movie]:
        return self._get_movie

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "tmdb.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("TMDB connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.tmdb.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("TMDB client is required")
    return Connection(reference, client.search_movies(), client.get_movie(), client)

setattr(new_connection, '__module__', 'dex_connectors.tmdb.connection')


def new_connection_with_operations(
    search_movies: Query[SearchMoviesInput, MoviePage],
    get_movie: Query[GetMovieInput, Movie],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, search_movies, get_movie)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.tmdb.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("TMDB credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
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

setattr(new_project_connection, '__module__', 'dex_connectors.tmdb.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("TMDB configuration has unknown fields")
    base_url = value.get("baseUrl", '')
    if not isinstance(base_url, str):
        raise ValueError("TMDB configuration baseUrl has an invalid type")
    timeout = value.get("timeout", 0)
    if not type(timeout) is int:
        raise ValueError("TMDB configuration timeout has an invalid type")
    timeout_seconds = timeout / 1_000_000_000
    max_response_bytes = value.get("maxResponseBytes", 0)
    if not type(max_response_bytes) is int:
        raise ValueError("TMDB configuration maxResponseBytes has an invalid type")
    return Config(base_url=base_url, timeout_seconds=timeout_seconds, max_response_bytes=max_response_bytes)
