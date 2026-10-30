# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the NewsAPI search Query."""

from datetime import UTC, datetime
from uuid import uuid4

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_query,
)
from dex_connectors.sdk.projectconfig import (
    Configuration,
    ConfigurationStore,
    ConnectionConfiguration,
    ConnectionKey,
    ConnectionStore,
    CredentialMaterial,
    InMemoryObjectStore,
    LoadedProject,
    Scope,
    SnapshotRef,
)

from dex_connectors.newsapi import (
    DEFECT,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    SEARCH_ARTICLES_DEFINITION,
    SEARCHED,
    Client,
    Config,
    Credentials,
    SearchArticlesInput,
    new_project_connection,
)
from dex_connectors.newsapi.newsapimock import (
    Mock,
    search_articles_invalid_key,
    search_articles_searched_default_answer,
)

CONNECTION = ConnectionRef("newsapi", "newsapi")
KEY = "0123456789abcdef0123456789abcdef"
VALID_BODY = b'{"status":"ok","totalResults":0,"articles":[]}'


def make_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, SEARCH_ARTICLES_DEFINITION.operation)


def test_manifest_definition_uses_async_durability_and_one_hour_retry() -> None:
    defaults = SEARCH_ARTICLES_DEFINITION.step_defaults
    assert defaults.execute_method_timeout.total_seconds() == 30
    assert defaults.execute_durability == 2
    assert defaults.execute_retry is not None
    assert defaults.execute_retry.total_duration.total_seconds() == 3600


def make_client(
    response: httpx.Response | None = None,
    credentials: StaticCredentialProvider[Credentials] | None = None,
    requests: list[httpx.Request] | None = None,
    config: Config | None = None,
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response or httpx.Response(200, content=VALID_BODY)

    return Client(
        config or Config(base_url="http://localhost:8848/v2"),
        credentials or StaticCredentialProvider({CONNECTION: Credentials(SecretString(KEY))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


@pytest.mark.asyncio
async def test_search_encodes_parameters_and_keeps_key_out_of_url() -> None:
    requests: list[httpx.Request] = []
    response = httpx.Response(
        200,
        json={
            "status": "ok",
            "totalResults": 5,
            "articles": [
                {
                    "source": {"id": None, "name": "Example Wire"},
                    "author": None,
                    "title": "Example & partners",
                    "description": None,
                    "url": "https://news.example/a",
                    "urlToImage": None,
                    "publishedAt": "2026-10-04T12:30:00Z",
                    "content": None,
                },
                {
                    "source": {"id": "bbc-news", "name": "BBC News"},
                    "author": "Reporter",
                    "title": "Second",
                    "description": "",
                    "url": "https://news.example/b",
                    "urlToImage": "https://news.example/b.jpg",
                    "publishedAt": "2026-10-04T05:00:00-07:00",
                    "content": "Body",
                },
            ],
        },
    )
    client = make_client(response, requests=requests)
    result = await client.search_articles().invoke(
        make_call(),
        SearchArticlesInput(
            query='"AT&T" #1 + co',
            search_in=("title", "description"),
            sources=("bbc-news", "the-verge"),
            domains=("techcrunch.com",),
            exclude_domains=("example.com",),
            from_time="2026-09-25",
            to_time="2026-10-05T07:00:00Z",
            language="en",
            sort_by="publishedAt",
            page_size=2,
            page=2,
        ),
    )
    assert result.kind == "branch" and result.branch == SEARCHED
    assert result.receipt is not None and result.receipt.call_id == ""
    assert result.value is not None
    assert result.value.page == 2 and result.value.has_more
    assert result.value.articles[0].description is None
    assert result.value.articles[1].description == ""
    assert result.value.articles[1].published_at == datetime(2026, 10, 4, 12, tzinfo=UTC)
    request = requests[0]
    assert request.url.path == "/v2/everything"
    assert request.headers["X-Api-Key"] == KEY
    assert KEY not in str(request.url)
    assert dict(request.url.params) == {
        "q": '"AT&T" #1 + co',
        "searchIn": "title,description",
        "sources": "bbc-news,the-verge",
        "domains": "techcrunch.com",
        "excludeDomains": "example.com",
        "from": "2026-09-25",
        "to": "2026-10-05T07:00:00Z",
        "language": "en",
        "sortBy": "publishedAt",
        "pageSize": "2",
        "page": "2",
    }
    await client.aclose()


@pytest.mark.asyncio
async def test_empty_optional_fields_and_removed_article() -> None:
    requests: list[httpx.Request] = []
    response = httpx.Response(
        200,
        json={
            "status": "ok",
            "totalResults": 101,
            "articles": [
                {
                    "source": {"id": None, "name": "[Removed]"},
                    "author": None,
                    "title": "[Removed]",
                    "description": "[Removed]",
                    "url": "https://removed.com",
                    "urlToImage": None,
                    "publishedAt": "1970-01-01T00:00:00Z",
                    "content": "[Removed]",
                },
            ],
        },
    )
    client = make_client(response, requests=requests)
    result = await client.search_articles().invoke(make_call(), SearchArticlesInput(query="acme"))
    assert result.value is not None and result.value.has_more
    assert result.value.articles[0].title == "[Removed]"
    assert dict(requests[0].url.params) == {"q": "acme"}
    await client.aclose()


@pytest.mark.asyncio
async def test_missing_article_fields_keep_go_zero_values() -> None:
    client = make_client(
        httpx.Response(
            200,
            json={
                "status": "ok",
                "totalResults": 1,
                "articles": [{}],
            },
        )
    )
    result = await client.search_articles().invoke(make_call(), SearchArticlesInput(query="a"))
    assert result.branch == SEARCHED and result.value is not None
    article = result.value.articles[0]
    assert article.source.name == "" and article.source.id is None
    assert article.title == "" and article.url == ""
    assert article.published_at == datetime.min.replace(tzinfo=UTC)
    await client.aclose()


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "body", "branch", "kind", "is_retry"),
    [
        (
            401,
            b'{"code":"apiKeyInvalid","message":"SENTINEL"}',
            PROVIDER_REJECTED,
            FailureKind.AUTHENTICATION,
            False,
        ),
        (
            400,
            b'{"code":"apiKeyMissing","message":"SENTINEL"}',
            PROVIDER_REJECTED,
            FailureKind.AUTHENTICATION,
            False,
        ),
        (
            429,
            b'{"code":"apiKeyExhausted","message":"SENTINEL"}',
            PROVIDER_REJECTED,
            FailureKind.QUOTA_EXHAUSTED,
            False,
        ),
        (
            400,
            b'{"code":"parameterInvalid","message":"SENTINEL"}',
            PROVIDER_REJECTED,
            FailureKind.VALIDATION,
            False,
        ),
        (
            400,
            b'{"code":"sourceDoesNotExist","message":"SENTINEL"}',
            PROVIDER_REJECTED,
            FailureKind.VALIDATION,
            False,
        ),
        (
            426,
            b'{"code":"maximumResultsReached","message":"SENTINEL"}',
            PROVIDER_REJECTED,
            FailureKind.AUTHORIZATION,
            False,
        ),
        (302, b"", PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, False),
        (
            418,
            b'{"code":"SENTINEL","message":"SENTINEL"}',
            PROVIDER_REJECTED,
            FailureKind.PROVIDER_REJECTION,
            False,
        ),
        (429, b'{"code":"rateLimited","message":"SENTINEL"}', "", FailureKind.RATE_LIMIT, True),
        (500, b'{"code":"unexpectedError","message":"SENTINEL"}', "", FailureKind.AVAILABILITY, True),
        (503, b"<html>SENTINEL</html>", "", FailureKind.AVAILABILITY, True),
        (408, b"", "", FailureKind.AVAILABILITY, True),
    ],
)
async def test_http_classification(
    status: int, body: bytes, branch: str, kind: FailureKind, is_retry: bool
) -> None:
    client = make_client(
        httpx.Response(status, content=body, headers={"Retry-After": "2", "Location": "/elsewhere"})
    )
    result = await client.search_articles().invoke(make_call(), SearchArticlesInput(query="acme"))
    assert result.branch == branch and result.failure is not None and result.failure.kind == kind
    assert result.kind == ("retry" if is_retry else "branch")
    assert result.retry_after_seconds == (2 if is_retry else 0)
    assert "SENTINEL" not in result.failure.message and KEY not in result.failure.message
    await client.aclose()


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "input",
    [
        SearchArticlesInput(query="  "),
        SearchArticlesInput(query="a" * 501),
        SearchArticlesInput(query="a", search_in=("body",)),
        SearchArticlesInput(sources=("s",) * 21),
        SearchArticlesInput(sources=("BBC News",)),
        SearchArticlesInput(domains=("a.com,b.com",)),
        SearchArticlesInput(query="a", exclude_domains=("a b",)),
        SearchArticlesInput(query="a", from_time="yesterday"),
        SearchArticlesInput(query="a", from_time="2026-10-05", to_time="2026-10-01"),
        SearchArticlesInput(query="a", language="xx"),
        SearchArticlesInput(query="a", sort_by="date"),
        SearchArticlesInput(query="a", page_size=101),
        SearchArticlesInput(query="a", page_size=-1),
        SearchArticlesInput(query="a", page=-1),
    ],
)
async def test_invalid_input_selects_defect_without_request(input: SearchArticlesInput) -> None:
    requests: list[httpx.Request] = []
    client = make_client(requests=requests)
    result = await client.search_articles().invoke(make_call(), input)
    assert result.branch == DEFECT and result.failure is not None
    assert result.failure.kind == FailureKind.VALIDATION and not requests
    await client.aclose()


@pytest.mark.asyncio
async def test_wrong_runtime_input_type_selects_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    client = make_client(requests=requests)
    result = await client.search_articles().invoke(make_call(), SearchArticlesInput(query=42))  # type: ignore[arg-type]
    assert result.branch == DEFECT and result.failure is not None
    assert result.failure.kind == FailureKind.VALIDATION and not requests
    await client.aclose()


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [
        b"{",
        b'{"status":"error","totalResults":0,"articles":[]}',
        b'{"status":"ok","totalResults":1}',
        b'{"status":"ok","articles":[]}',
        b'{"status":"ok","totalResults":-1,"articles":[]}',
        b'{"status":"ok","totalResults":3,"articles":[{"title":"a"},{"title":"b"},{"title":"c"}]}',
        b'{"status":"ok","totalResults":1,"articles":[{"title":"a","publishedAt":"yesterday"}]}',
    ],
)
async def test_invalid_response(body: bytes) -> None:
    client = make_client(httpx.Response(200, content=body))
    result = await client.search_articles().invoke(make_call(), SearchArticlesInput(query="a", page_size=2))
    assert result.branch == INVALID_RESPONSE and result.failure is not None
    assert result.failure.kind == FailureKind.PROTOCOL
    await client.aclose()


@pytest.mark.asyncio
async def test_oversized_response() -> None:
    client = make_client(
        httpx.Response(200, content=VALID_BODY + b" " * 64),
        config=Config(base_url="http://localhost:8848", max_response_bytes=96),
    )
    result = await client.search_articles().invoke(make_call(), SearchArticlesInput(query="a"))
    assert result.branch == INVALID_RESPONSE and result.failure is not None
    assert result.failure.kind == FailureKind.RESPONSE_TOO_LARGE
    await client.aclose()


@pytest.mark.asyncio
@pytest.mark.parametrize("key", ["", "abc\r\nX-Injected: 1"])
async def test_invalid_credentials_select_defect_without_request(key: str) -> None:
    requests: list[httpx.Request] = []
    credentials = StaticCredentialProvider({CONNECTION: Credentials(SecretString(key))})
    client = make_client(credentials=credentials, requests=requests)
    result = await client.search_articles().invoke(make_call(), SearchArticlesInput(query="a"))
    assert result.branch == DEFECT and result.failure is not None
    assert result.failure.kind == FailureKind.AUTHENTICATION and not requests
    await client.aclose()


@pytest.mark.asyncio
async def test_missing_credentials_select_defect() -> None:
    client = make_client(credentials=StaticCredentialProvider({}))
    result = await client.search_articles().invoke(make_call(), SearchArticlesInput(query="a"))
    assert result.branch == DEFECT and result.failure is not None
    assert result.failure.kind == FailureKind.AUTHENTICATION
    await client.aclose()


@pytest.mark.asyncio
async def test_transport_failure_retries_without_exposing_transport_text() -> None:
    def fail(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("SENTINEL", request=request)

    client = Client(
        Config(base_url="http://localhost:8848"),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(KEY))}),
        httpx.AsyncClient(transport=httpx.MockTransport(fail)),
    )
    result = await client.search_articles().invoke(make_call(), SearchArticlesInput(query="a"))
    assert result.kind == "retry" and result.failure is not None
    assert result.failure.kind == FailureKind.AVAILABILITY
    assert "SENTINEL" not in result.failure.message
    await client.aclose()


@pytest.mark.asyncio
async def test_sdk_query_runner_returns_receipt_and_raises_retry() -> None:
    client = make_client()
    call = make_call()
    result = await run_query(client.search_articles(), call, SearchArticlesInput(query="a"))
    assert result.branch == SEARCHED
    assert result.receipt.call_id == call.id
    await client.aclose()

    unavailable = make_client(httpx.Response(503, content=b"unavailable"))
    with pytest.raises(RetryError) as raised:
        await run_query(unavailable.search_articles(), make_call(), SearchArticlesInput(query="a"))
    assert raised.value.failure.kind == FailureKind.AVAILABILITY
    await unavailable.aclose()


@pytest.mark.asyncio
async def test_credentials_are_resolved_each_call() -> None:
    class ReplacingCredentials:
        def __init__(self) -> None:
            self.current = KEY

        async def resolve(self, call: Call) -> Credentials:
            return Credentials(SecretString(self.current))

    source = ReplacingCredentials()
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, content=VALID_BODY)

    client = Client(
        Config(base_url="http://localhost:8848"),
        source,
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )
    await client.search_articles().invoke(make_call(), SearchArticlesInput(query="a"))
    source.current = "fedcba9876543210fedcba9876543210"
    await client.search_articles().invoke(make_call(), SearchArticlesInput(query="a"))
    assert [request.headers["X-Api-Key"] for request in requests] == [KEY, source.current]
    await client.aclose()


@pytest.mark.asyncio
async def test_project_connection_reads_settings_and_current_key() -> None:
    scope = Scope("project-news", "preview")
    objects = InMemoryObjectStore()
    key = ConnectionKey("newsapi", "newsapi")
    configuration = Configuration(
        scope,
        1,
        (
            ConnectionConfiguration(
                "newsapi",
                "newsapi",
                "dex-connector-newsapi",
                "newsapi",
                {"baseUrl": "http://localhost:8848/v2", "timeout": 5_000_000_000, "maxResponseBytes": 1024},
            ),
        ),
    )
    material = {key: CredentialMaterial({"api_key": KEY}, None, "apiKey")}
    project = LoadedProject(
        configuration,
        SnapshotRef("", "", ""),
        ConnectionStore(objects, scope),
        ConfigurationStore(objects, scope),
        local_credentials=material,
    )
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(200, content=VALID_BODY)

    connection = new_project_connection(
        project, "newsapi", httpx.AsyncClient(transport=httpx.MockTransport(respond))
    )
    assert connection.reference == CONNECTION
    assert (
        await connection.search_articles().invoke(make_call(), SearchArticlesInput(query="a"))
    ).branch == SEARCHED
    material[key] = CredentialMaterial({"api_key": "fedcba9876543210fedcba9876543210"}, None, "apiKey")
    assert (
        await connection.search_articles().invoke(make_call(), SearchArticlesInput(query="a"))
    ).branch == SEARCHED
    assert [request.headers["X-Api-Key"] for request in requests] == [
        KEY,
        material[key].credentials["api_key"],
    ]
    await connection.aclose()


def test_project_connection_rejects_unknown_configuration_fields() -> None:
    scope = Scope("project-news", "preview")
    objects = InMemoryObjectStore()
    configuration = Configuration(
        scope,
        1,
        (
            ConnectionConfiguration(
                "newsapi",
                "newsapi",
                "dex-connector-newsapi",
                "newsapi",
                {"baseURL": "https://gateway.example.test/v2"},
            ),
        ),
    )
    project = LoadedProject(
        configuration,
        SnapshotRef("", "", ""),
        ConnectionStore(objects, scope),
        ConfigurationStore(objects, scope),
    )
    with pytest.raises(ValueError) as error:
        new_project_connection(project, "newsapi")
    assert str(error.value) == "NewsAPI configuration has unknown fields"


def test_client_validates_config() -> None:
    credentials = StaticCredentialProvider({CONNECTION: Credentials(SecretString(KEY))})
    with pytest.raises(ValueError, match="HTTPS"):
        Client(Config(base_url="http://newsapi.example/v2"), credentials)
    with pytest.raises(ValueError):
        Client(Config(base_url="https://newsapi.org/v2?apiKey=x"), credentials)
    with pytest.raises(ValueError, match="positive"):
        Client(Config(timeout_seconds=-1), credentials)


def test_manifest_mock_default_and_invalid_key() -> None:
    mock = Mock()
    assert mock.search_articles().definition() == SEARCH_ARTICLES_DEFINITION
    default = search_articles_searched_default_answer()
    assert default.branch == SEARCHED and default.value is not None
    assert len(default.value.articles) == 2
    mock.search_articles_script.respond(search_articles_invalid_key())
    invalid = search_articles_invalid_key()
    assert invalid.branch == PROVIDER_REJECTED and invalid.failure is not None
    assert invalid.failure.kind == FailureKind.AUTHENTICATION
    mock.search_articles_script.assert_no_failures()
