# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Fail when the hand-written Python contract drifts from connector.yaml."""

import tomllib
from datetime import timedelta
from pathlib import Path

import dex
import yaml
from dex_connectors.sdk import default_connector_step_execute_retry_policy

from dex_connectors.tmdb import GET_MOVIE_DEFINITION, SEARCH_MOVIES_DEFINITION
from dex_connectors.tmdb.tmdbmock import (
    Mock,
    get_movie_default,
    search_movies_default,
)
from examples.movie_search.flow import FLOW_TYPE, MovieSearchFlow

CONNECTOR_DIRECTORY = Path(__file__).resolve().parents[2]


def test_manifest_operations_branches_and_mocks() -> None:
    manifest = yaml.safe_load((CONNECTOR_DIRECTORY / "connector.yaml").read_text())
    package = tomllib.loads((CONNECTOR_DIRECTORY / "python" / "pyproject.toml").read_text())
    assert manifest["spec"]["languages"]["python"]["version"] == "v" + package["project"]["version"]
    assert package["project"]["name"] == "dex-connector-tmdb"
    operations = {operation["name"]: operation for operation in manifest["spec"]["operations"]}
    assert set(operations) == {"searchMovies", "getMovie"}
    for definition in (SEARCH_MOVIES_DEFINITION, GET_MOVIE_DEFINITION):
        operation = operations[definition.operation.operation_id]
        assert definition.operation.connector_id == manifest["metadata"]["name"]
        assert operation["kind"] == "query"
        assert [
            (branch["id"], branch["description"], branch.get("optional", False))
            for branch in operation["branches"]
        ] == [(branch.id, branch.description, branch.optional) for branch in definition.branches or ()]
        assert operation["execution"] == {"executeMethodTimeout": "30s", "durability": "async"}
        defaults = definition.step_defaults
        assert defaults.execute_method_timeout == timedelta(seconds=30)
        assert defaults.execute_durability == 2
        assert defaults.execute_retry == default_connector_step_execute_retry_policy()
    page = search_movies_default().value
    assert page is not None
    assert operations["searchMovies"]["mocks"][0]["output"] == {
        "page": page.page,
        "total_pages": page.total_pages,
        "total_results": page.total_results,
        "results": [
            {
                "id": movie.id,
                "title": movie.title,
                "overview": movie.overview,
                "release_date": movie.release_date,
            }
            for movie in page.results
        ],
    }
    movie = get_movie_default().value
    assert movie is not None
    assert operations["getMovie"]["mocks"][0]["output"] == {
        "id": movie.id,
        "title": movie.title,
        "overview": movie.overview,
        "release_date": movie.release_date,
    }


def test_example_registers_with_mock_connection() -> None:
    flow = MovieSearchFlow(Mock().connection())
    assert flow.get_flow_type() == FLOW_TYPE
    dex.Registry([flow], allow_async_handlers=True)
