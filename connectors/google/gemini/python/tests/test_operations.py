# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for Gemini generateContent."""

import json
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

from dex_connectors.google_gemini import (
    BLOCKED,
    DEFECT,
    GENERATE_CONTENT_DEFINITION,
    GENERATED,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    TRUNCATED,
    Client,
    Config,
    Content,
    Credentials,
    GenerateContentRequest,
    Part,
)
from dex_connectors.google_gemini.geminimock import (
    validate_generate_content_mock_answer,
    validate_generate_content_mock_request,
)

CONNECTION = ConnectionRef("google", "gemini")
KEY = "AIza-test-key"
REQUEST = GenerateContentRequest(contents=(Content("user", (Part("Summarize this."),)),))
GENERATED_BODY = {
    "candidates": [
        {
            "content": {
                "parts": [{"text": "thinking", "thought": True}, {"text": "Hello"}, {"text": " world"}]
            },
            "finishReason": "STOP",
        }
    ],
    "usageMetadata": {
        "promptTokenCount": 10,
        "candidatesTokenCount": 3,
        "thoughtsTokenCount": 2,
        "cachedContentTokenCount": 1,
        "totalTokenCount": 15,
    },
    "modelVersion": "gemini-3.5-flash-lite",
    "responseId": "resp-1",
}


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, key: str = KEY, **config: object
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(endpoint="http://127.0.0.1:8848/v1beta/", **config),  # type: ignore[arg-type]
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(key))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def generate_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, GENERATE_CONTENT_DEFINITION.operation)


@pytest.mark.asyncio
async def test_generated_joins_non_thought_text_and_records_receipt() -> None:
    requests: list[httpx.Request] = []
    call = generate_call()
    response = httpx.Response(200, json=GENERATED_BODY, headers={"X-Goog-Request-Id": "req-9"})
    result = await run_query(make_client(response, requests).generate_content(), call, REQUEST)
    assert result.branch == GENERATED
    assert result.value.text == "Hello world"
    assert result.value.finish_reason == "STOP"
    assert (
        result.value.usage.prompt_tokens,
        result.value.usage.thoughts_tokens,
        result.value.usage.total_tokens,
    ) == (
        10,
        2,
        15,
    )
    assert result.receipt.provider_object_id == "resp-1"
    assert result.receipt.provider_request_id == "req-9"
    request = requests[0]
    assert str(request.url) == "http://127.0.0.1:8848/v1beta/models/gemini-3.5-flash-lite:generateContent"
    assert request.headers["x-goog-api-key"] == KEY
    assert json.loads(request.content) == {
        "contents": [{"role": "user", "parts": [{"text": "Summarize this."}]}]
    }


@pytest.mark.asyncio
async def test_request_encodes_generation_config_like_go() -> None:
    requests: list[httpx.Request] = []
    request = GenerateContentRequest(
        model="models/gemini-3.8-flash",
        system_instruction="Be brief.",
        contents=(Content(parts=(Part("a"),)), Content("model", (Part("b"),))),
        response_json_schema={"type": "object"},
        temperature=0.5,
        max_output_tokens=64,
        thinking_budget=0,
    )
    await run_query(
        make_client(httpx.Response(200, json=GENERATED_BODY), requests).generate_content(),
        generate_call(),
        request,
    )
    assert requests[0].url.path == "/v1beta/models/gemini-3.8-flash:generateContent"
    assert json.loads(requests[0].content) == {
        "contents": [{"parts": [{"text": "a"}]}, {"role": "model", "parts": [{"text": "b"}]}],
        "systemInstruction": {"parts": [{"text": "Be brief."}]},
        "generationConfig": {
            "responseMimeType": "application/json",
            "responseJsonSchema": {"type": "object"},
            "temperature": 0.5,
            "maxOutputTokens": 64,
            "thinkingConfig": {"thinkingBudget": 0},
        },
    }


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "request_value",
    [
        GenerateContentRequest(),
        GenerateContentRequest(model="bad model", contents=REQUEST.contents),
        GenerateContentRequest(contents=(Content("system", (Part("a"),)),)),
        GenerateContentRequest(contents=(Content("user", ()),)),
        GenerateContentRequest(contents=(Content("user", (Part(""),)),)),
        GenerateContentRequest(
            contents=REQUEST.contents, response_mime_type="text/plain", response_json_schema={}
        ),
        GenerateContentRequest(contents=REQUEST.contents, temperature=2.5),
        GenerateContentRequest(contents=REQUEST.contents, temperature=float("nan")),
        GenerateContentRequest(contents=REQUEST.contents, max_output_tokens=-1),
        GenerateContentRequest(contents=REQUEST.contents, thinking_budget=-2),
    ],
)
async def test_invalid_request_is_defect_without_provider_call(request_value: GenerateContentRequest) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=GENERATED_BODY), requests).generate_content(),
        generate_call(),
        request_value,
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.VALIDATION
    assert requests == []
    assert validate_generate_content_mock_request(request_value) is not None


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("body", "branch", "kind", "text"),
    [
        (
            {**GENERATED_BODY, "promptFeedback": {"blockReason": "SAFETY"}},
            BLOCKED,
            FailureKind.PROVIDER_REJECTION,
            "",
        ),
        (
            {**GENERATED_BODY, "promptFeedback": {"blockReason": "safety"}},
            INVALID_RESPONSE,
            FailureKind.PROTOCOL,
            "",
        ),
        (
            {"candidates": [{"content": {"parts": [{"text": "Par"}]}, "finishReason": "MAX_TOKENS"}]},
            TRUNCATED,
            FailureKind.RESPONSE_TOO_LARGE,
            "Par",
        ),
        ({"candidates": [{"finishReason": "RECITATION"}]}, BLOCKED, FailureKind.PROVIDER_REJECTION, ""),
        (
            {"candidates": [{"finishReason": "MALFORMED_FUNCTION_CALL"}]},
            INVALID_RESPONSE,
            FailureKind.PROTOCOL,
            "",
        ),
        (
            {
                "candidates": [
                    {"content": {"parts": [{"text": "x", "thought": True}]}, "finishReason": "STOP"}
                ]
            },
            INVALID_RESPONSE,
            FailureKind.PROTOCOL,
            "",
        ),
        (
            {"candidates": [{"content": {"parts": [{"text": "x"}]}}]},
            INVALID_RESPONSE,
            FailureKind.PROTOCOL,
            "",
        ),
        ({"candidates": []}, INVALID_RESPONSE, FailureKind.PROTOCOL, ""),
        ({"candidates": "x"}, INVALID_RESPONSE, FailureKind.PROTOCOL, ""),
    ],
)
async def test_response_classification(body: object, branch: str, kind: FailureKind, text: str) -> None:
    result = await run_query(
        make_client(httpx.Response(200, json=body)).generate_content(), generate_call(), REQUEST
    )
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind
    assert result.value.text == text


@pytest.mark.asyncio
async def test_oversized_response_is_invalid() -> None:
    result = await run_query(
        make_client(httpx.Response(200, content=b"x" * 50), max_response_bytes=10).generate_content(),
        generate_call(),
        REQUEST,
    )
    assert result.branch == INVALID_RESPONSE
    assert result.failure is not None and result.failure.kind == FailureKind.RESPONSE_TOO_LARGE


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "body", "kind"),
    [
        (400, {"error": {"status": "INVALID_ARGUMENT"}}, FailureKind.PROVIDER_REJECTION),
        (
            400,
            {
                "error": {
                    "details": [
                        {"@type": "type.googleapis.com/google.rpc.ErrorInfo", "reason": "API_KEY_INVALID"}
                    ]
                }
            },
            FailureKind.AUTHENTICATION,
        ),
        (403, [{"error": {"status": "PERMISSION_DENIED"}}], FailureKind.AUTHORIZATION),
        (404, {}, FailureKind.NOT_FOUND),
        (409, {}, FailureKind.CONFLICT),
        (501, {}, FailureKind.PROVIDER_REJECTION),
    ],
)
async def test_rejections(status: int, body: object, kind: FailureKind) -> None:
    result = await run_query(
        make_client(httpx.Response(status, json=body)).generate_content(), generate_call(), REQUEST
    )
    assert result.branch == PROVIDER_REJECTED
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
async def test_rejection_message_names_google_status() -> None:
    result = await run_query(
        make_client(httpx.Response(400, json={"error": {"status": "INVALID_ARGUMENT"}})).generate_content(),
        generate_call(),
        REQUEST,
    )
    assert (
        result.failure is not None and result.failure.message == "provider returned HTTP 400 INVALID_ARGUMENT"
    )


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "body", "headers", "kind", "delay"),
    [
        (
            429,
            {
                "error": {
                    "details": [{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "12.5s"}]
                }
            },
            {"Retry-After": "3"},
            FailureKind.RATE_LIMIT,
            12.5,
        ),
        (503, {}, {"Retry-After": "7"}, FailureKind.AVAILABILITY, 7),
        (500, {}, {"Retry-After": "999999"}, FailureKind.AVAILABILITY, 3600),
        (408, {}, {}, FailureKind.AVAILABILITY, 0),
    ],
)
async def test_retries_use_google_retry_delay(
    status: int, body: object, headers: dict[str, str], kind: FailureKind, delay: float
) -> None:
    with pytest.raises(RetryError) as raised:
        await run_query(
            make_client(httpx.Response(status, json=body, headers=headers)).generate_content(),
            generate_call(),
            REQUEST,
        )
    assert raised.value.failure.kind == kind
    assert raised.value.retry_after_seconds == delay


@pytest.mark.asyncio
async def test_transport_failure_retries() -> None:
    def fail(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("refused", request=request)

    client = Client(
        Config(endpoint="http://localhost:8848/v1beta"),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(KEY))}),
        httpx.AsyncClient(transport=httpx.MockTransport(fail)),
    )
    with pytest.raises(RetryError) as raised:
        await run_query(client.generate_content(), generate_call(), REQUEST)
    assert raised.value.failure.kind == FailureKind.TRANSPORT


@pytest.mark.asyncio
@pytest.mark.parametrize("key", ["", "has space", "tab\tkey"])
async def test_invalid_key_is_defect_without_request(key: str) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=GENERATED_BODY), requests, key=key).generate_content(),
        generate_call(),
        REQUEST,
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []


@pytest.mark.parametrize(
    "endpoint",
    [
        "ftp://x",
        "http://example.com/v1",
        "https://user@x.test",
        "https://x.test/?q=1",
        "https://x.test/#f",
        "relative",
    ],
)
def test_endpoint_validation(endpoint: str) -> None:
    with pytest.raises(ValueError):
        Client(Config(endpoint=endpoint), StaticCredentialProvider({}))


def test_model_validation_and_defaults() -> None:
    assert Client(Config(model="  "), StaticCredentialProvider({})).model == "gemini-3.5-flash-lite"
    assert (
        Client(Config(model="models/gemini-3.8-flash"), StaticCredentialProvider({})).model
        == "gemini-3.8-flash"
    )
    with pytest.raises(ValueError):
        Client(Config(model="-bad"), StaticCredentialProvider({}))


def test_mock_answer_validation() -> None:
    from dex_connectors.google_gemini import GenerateContentResponse

    assert validate_generate_content_mock_answer(REQUEST, GenerateContentResponse(text="ok")) == ""
    assert validate_generate_content_mock_answer(
        REQUEST, GenerateContentResponse(text="ok", finish_reason="MAX_TOKENS")
    )
    assert validate_generate_content_mock_answer(REQUEST, GenerateContentResponse())
    assert validate_generate_content_mock_request(REQUEST) is None
