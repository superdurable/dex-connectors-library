# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the ElevenLabs operations."""

import base64
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

from dex_connectors.elevenlabs import (
    CREATE_SPEECH_DEFINITION,
    CREATED,
    DEFECT,
    INVALID_RESPONSE,
    LIST_VOICES_DEFINITION,
    LISTED,
    PROVIDER_REJECTED,
    Client,
    Config,
    CreateSpeechInput,
    Credentials,
    ListVoicesInput,
)

CONNECTION = ConnectionRef("elevenlabs", "elevenlabs")
KEY = "elevenlabs-test-key"
VOICES = {
    "voices": [{"voice_id": "voice_meridian", "name": "Meridian Narrator"}],
    "has_more": True,
    "next_page_token": "page-2",
}
SPEECH = CreateSpeechInput("voice_meridian", "Welcome to Meridian.")


def make_client(
    response: httpx.Response, requests: list[httpx.Request] | None = None, key: str = KEY
) -> Client:
    def respond(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return response

    return Client(
        Config(base_url="http://localhost:8848", max_response_bytes=300),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(key))}),
        httpx.AsyncClient(transport=httpx.MockTransport(respond)),
    )


def voices_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, LIST_VOICES_DEFINITION.operation)


def speech_call() -> Call:
    return Call(CallID(str(uuid4())), CONNECTION, CREATE_SPEECH_DEFINITION.operation)


@pytest.mark.asyncio
async def test_list_voices_sends_key_and_page_query() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=VOICES), requests).list_voices(),
        voices_call(),
        ListVoicesInput(25, "page-1"),
    )
    assert result.branch == LISTED
    assert [(voice.voice_id, voice.name) for voice in result.value.voices] == [
        ("voice_meridian", "Meridian Narrator")
    ]
    assert result.value.has_more is True
    assert result.value.next_page_token == "page-2"
    request = requests[0]
    assert request.method == "GET"
    assert request.url.path == "/v2/voices"
    assert request.url.query == b"next_page_token=page-1&page_size=25"
    assert request.headers["xi-api-key"] == KEY
    assert request.headers["Accept"] == "application/json"
    assert "Content-Type" not in request.headers


@pytest.mark.asyncio
async def test_list_voices_without_page_sends_no_query() -> None:
    requests: list[httpx.Request] = []
    await run_query(
        make_client(httpx.Response(200, json={"voices": []}), requests).list_voices(),
        voices_call(),
        ListVoicesInput(),
    )
    assert str(requests[0].url) == "http://localhost:8848/v2/voices"


@pytest.mark.asyncio
async def test_create_speech_returns_base64_audio() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, content=b"ID3audio"), requests).create_speech(), speech_call(), SPEECH
    )
    assert result.branch == CREATED
    assert result.value.audio_base64 == base64.b64encode(b"ID3audio").decode()
    assert result.value.content_type == "audio/mpeg"
    request = requests[0]
    assert request.method == "POST"
    assert request.url.path == "/v1/text-to-speech/voice_meridian"
    assert request.url.query == b"output_format=mp3_44100_128"
    assert request.headers["Accept"] == "audio/mpeg"
    assert request.headers["Content-Type"] == "application/json"
    assert json.loads(request.content) == {
        "text": "Welcome to Meridian.",
        "model_id": "eleven_multilingual_v2",
    }


@pytest.mark.asyncio
@pytest.mark.parametrize("value", [ListVoicesInput(-1), ListVoicesInput(101), ListVoicesInput(0, "a\nb")])
async def test_invalid_voice_page_is_defect_without_request(value: ListVoicesInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=VOICES), requests).list_voices(), voices_call(), value
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.VALIDATION
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        CreateSpeechInput("", "hi"),
        CreateSpeechInput("voice/../x", "hi"),
        CreateSpeechInput("v" * 129, "hi"),
        CreateSpeechInput("voice", " "),
    ],
)
async def test_invalid_speech_is_defect_without_request(value: CreateSpeechInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, content=b"a"), requests).create_speech(), speech_call(), value
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("response", "branch", "kind"),
    [
        (httpx.Response(401), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(404), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(409), PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION),
        (httpx.Response(200, json={"has_more": False}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, json={"voices": [{"name": "x"}]}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, json={"voices": [], "has_more": 0}), INVALID_RESPONSE, FailureKind.PROTOCOL),
        (httpx.Response(200, content=b"x" * 400), INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE),
    ],
)
async def test_list_voices_branches(response: httpx.Response, branch: str, kind: FailureKind) -> None:
    result = await run_query(make_client(response).list_voices(), voices_call(), ListVoicesInput())
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
async def test_empty_audio_is_invalid_response() -> None:
    result = await run_query(
        make_client(httpx.Response(200, content=b"")).create_speech(), speech_call(), SPEECH
    )
    assert result.branch == INVALID_RESPONSE
    assert result.failure is not None and result.failure.kind == FailureKind.PROTOCOL


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"),
    [(408, FailureKind.AVAILABILITY), (429, FailureKind.RATE_LIMIT), (500, FailureKind.AVAILABILITY)],
)
async def test_safe_failures_retry(status: int, kind: FailureKind) -> None:
    with pytest.raises(RetryError) as raised:
        await run_query(make_client(httpx.Response(status)).create_speech(), speech_call(), SPEECH)
    assert raised.value.failure.kind == kind


@pytest.mark.asyncio
async def test_transport_failure_retries() -> None:
    def fail(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("refused", request=request)

    client = Client(
        Config(base_url="http://localhost:8848"),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(KEY))}),
        httpx.AsyncClient(transport=httpx.MockTransport(fail)),
    )
    with pytest.raises(RetryError) as raised:
        await run_query(client.list_voices(), voices_call(), ListVoicesInput())
    assert raised.value.failure.kind == FailureKind.AVAILABILITY


@pytest.mark.asyncio
async def test_blank_key_is_defect_without_request() -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(httpx.Response(200, json=VOICES), requests, key="").list_voices(),
        voices_call(),
        ListVoicesInput(),
    )
    assert result.branch == DEFECT
    assert result.failure is not None and result.failure.kind == FailureKind.AUTHENTICATION
    assert requests == []
