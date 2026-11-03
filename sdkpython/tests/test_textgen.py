# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Go textgen pipeline and Chat Completions cases replayed at the HTTP boundary."""

import asyncio
import json
from dataclasses import replace
from datetime import timedelta
from typing import Any, cast

import httpx
import pytest

from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    OperationRef,
    QueryDefinition,
    SecretString,
    StepDefaults,
)
from dex_connectors.sdk.textgen import (
    ErrorRule,
    Message,
    MessageRole,
    ModelIDRule,
    RequestFeatures,
    StructuredOutput,
    StructuredOutputMode,
    StructuredOutputRules,
    TextGenerationQuery,
    TextGenerationQueryConfig,
    TextGenerationRequest,
    blocked_outcome,
    extract_structured_answer,
    invalid_response_outcome,
    quota_exhausted_outcome,
    resolve_model,
    temperature_not_accepted,
    temperature_range,
    text_generation_branch_definitions,
    validate_portable_request,
)
from dex_connectors.sdk.textgen.openaichat import (
    MaxTokensField,
    ModelRule,
    Profile,
    StreamingPolicy,
    new_wire_format,
)

_CALL_ID = CallID("5af8fafa-06ff-5b2d-bb13-04693809fabf")
_OPERATION = OperationRef("chat-lab", "generateText")
_REQUEST = TextGenerationRequest((Message(MessageRole.USER, "hello"),))
_SCHEMA: dict[str, object] = {
    "type": "object",
    "properties": {"answer": {"type": "string", "minLength": 2}},
    "required": ["answer"],
    "additionalProperties": False,
}


class _Context:
    def __init__(self) -> None:
        self.heartbeats: list[object] = []

    async def heartbeat(self, value: object) -> None:
        self.heartbeats.append(value)


def _call(context: _Context | None = None) -> Call:
    return Call(
        _CALL_ID, ConnectionRef("chat-lab", "test"), _OPERATION, context=cast(Any, context or _Context())
    )


def _query(
    handler: Any,
    profile: Profile | None = None,
    model: str = "chat-model",
    maximum: int = 65536,
    context_client: httpx.AsyncClient | None = None,
) -> TextGenerationQuery:
    profile = profile or Profile("chat-lab")
    definition = QueryDefinition(
        _OPERATION, text_generation_branch_definitions(), StepDefaults(timedelta(minutes=1), 1)
    )

    async def credential(call: Call) -> SecretString:
        return SecretString("sk-test")

    client = context_client or httpx.AsyncClient(transport=httpx.MockTransport(handler))
    return TextGenerationQuery(
        TextGenerationQueryConfig(
            definition,
            new_wire_format(profile),
            "https://provider.example/v1",
            model,
            10,
            credential,
            maximum,
            1024,
            client,
        )
    )


def _completion(text: str = "answer", finish: str = "stop", **other: object) -> dict[str, object]:
    return {
        "id": "resp-1",
        "model": "served-model",
        "choices": [{"index": 0, "message": {"content": text}, "finish_reason": finish}],
        "usage": {"prompt_tokens": 5, "completion_tokens": 3},
        **other,
    }


@pytest.mark.parametrize(
    "rule,value,expected",
    [
        (ModelIDRule.BODY, " ft:base:org::id ", "ft:base:org::id"),
        (ModelIDRule.PATH_SEGMENT, " models/gemini-2.5 ", "gemini-2.5"),
    ],
)
def test_model_id_cases(rule: ModelIDRule, value: str, expected: str) -> None:
    assert rule.validate_model_id(value) == expected
    assert resolve_model(" ", expected) == expected


def test_model_id_fixture_matches_go() -> None:
    cases = json.loads(
        (
            __import__("pathlib").Path(__file__).parents[2]
            / "sdkgo/textgen/textgentest/testdata/model_id_cases.json"
        ).read_text()
    )["cases"]
    for case in cases:
        rule = ModelIDRule(case["rule"])
        if case["valid"]:
            assert rule.validate_model_id(case["input"]) == case["modelId"]
        else:
            with pytest.raises(ValueError):
                rule.validate_model_id(case["input"])


@pytest.mark.asyncio
async def test_pipeline_resolves_model_and_usage_without_leaking_secrets() -> None:
    seen: list[httpx.Request] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        return httpx.Response(
            200,
            headers={
                "x-request-id": "req-2",
                "x-ratelimit-remaining": "41",
                "x-ratelimit-echo": "has sk-test",
            },
            json=_completion(),
        )

    query = _query(
        handler,
        Profile("chat-lab", rate_limit_headers=("x-ratelimit-remaining", "x-ratelimit-echo")),
        model=" chat-model ",
    )
    result = await query.invoke(_call(), _REQUEST)
    assert result.branch == "generated"
    assert result.value is not None
    assert result.value.text == "answer"
    assert result.value.requested_model == "chat-model"
    assert result.value.served_model == "served-model"
    assert result.value.usage.total_tokens == 8
    assert result.receipt is not None
    assert result.receipt.provider_object_id == "resp-1"
    assert result.receipt.provider_request_id == "req-2"
    assert result.receipt.metadata == {"x-ratelimit-remaining": "41"}
    assert json.loads(seen[0].content)["model"] == "chat-model"
    assert seen[0].headers["authorization"] == "Bearer sk-test"
    assert "sk-test" not in repr(result.value) + json.dumps(result.receipt.to_wire())


@pytest.mark.parametrize(
    "status,branch,kind",
    [
        (408, "", FailureKind.AVAILABILITY),
        (429, "", FailureKind.RATE_LIMIT),
        (500, "", FailureKind.AVAILABILITY),
        (501, "providerRejected", FailureKind.PROVIDER_REJECTION),
        (400, "providerRejected", FailureKind.PROVIDER_REJECTION),
        (401, "providerRejected", FailureKind.AUTHENTICATION),
        (402, "providerRejected", FailureKind.QUOTA_EXHAUSTED),
        (403, "providerRejected", FailureKind.AUTHORIZATION),
        (404, "providerRejected", FailureKind.NOT_FOUND),
        (409, "providerRejected", FailureKind.CONFLICT),
        (307, "invalidResponse", FailureKind.PROTOCOL),
    ],
)
@pytest.mark.asyncio
async def test_go_error_status_matrix(status: int, branch: str, kind: FailureKind) -> None:
    requests: list[httpx.Request] = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(
            status,
            json={"error": {"code": "provider_code", "message": "secret prose"}},
            headers={"location": "https://elsewhere.example"},
        )

    result = await _query(handler).invoke(_call(), _REQUEST)
    assert result.kind == ("branch" if branch else "retry")
    assert result.branch == branch
    assert result.failure is not None and result.failure.kind == kind
    assert "secret prose" not in result.failure.message
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_error_rules_override_defaults_in_order() -> None:
    rules = (
        ErrorRule(429, "billing_exhausted", quota_exhausted_outcome()),
        ErrorRule(400, "content_filter", blocked_outcome()),
    )

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(429, json={"error": {"code": "billing_exhausted"}})

    result = await _query(handler, Profile("chat-lab", error_rules=rules)).invoke(_call(), _REQUEST)
    assert result.branch == "providerRejected"
    assert result.failure is not None and result.failure.kind == FailureKind.QUOTA_EXHAUSTED


@pytest.mark.parametrize(
    "finish,text,expected",
    [
        ("stop", "answer", "generated"),
        ("length", "partial", "truncated"),
        ("content_filter", "partial", "blocked"),
        ("tool_calls", "answer", "invalidResponse"),
        ("", "answer", "invalidResponse"),
        ("stop", "", "invalidResponse"),
    ],
)
@pytest.mark.asyncio
async def test_go_finish_reason_matrix(finish: str, text: str, expected: str) -> None:
    query = _query(lambda request: httpx.Response(200, json=_completion(text, finish)))
    result = await query.invoke(_call(), _REQUEST)
    assert result.branch == expected
    assert result.value is not None
    if expected in ("generated", "truncated"):
        assert result.value.text == text
    else:
        assert result.value.text == ""


@pytest.mark.parametrize(
    "output,valid",
    [
        (StructuredOutput("answer", _SCHEMA), True),
        (StructuredOutput("bad name", _SCHEMA), False),
        (StructuredOutput("answer", {**_SCHEMA, "anyOf": []}), False),
        (StructuredOutput("answer", {**_SCHEMA, "additionalProperties": True}), False),
    ],
)
def test_portable_schema_validation(output: StructuredOutput, valid: bool) -> None:
    request = replace(_REQUEST, structured_output=output)
    assert (validate_portable_request(request) is None) == valid


def test_structured_answer_checks_original_schema() -> None:
    request = replace(_REQUEST, structured_output=StructuredOutput("answer", _SCHEMA))
    assert extract_structured_answer(request, '```json\n{"answer":"yes"}\n```') == '{"answer":"yes"}'
    with pytest.raises(ValueError, match="minLength"):
        extract_structured_answer(request, '{"answer":"x"}')
    with pytest.raises(ValueError, match="does not declare"):
        extract_structured_answer(request, '{"answer":"yes","secret":"value"}')


@pytest.mark.parametrize(
    "schema,reason",
    [
        ({"type": "array", "items": {"type": "string"}}, 'root must have type "object"'),
        ({"type": "object", "properties": {}, "required": []}, '"additionalProperties" to false'),
        (
            {
                "type": "object",
                "properties": {"a": {"type": "string"}},
                "required": [],
                "additionalProperties": False,
            },
            'list every property in "required"',
        ),
        (
            {"type": "object", "properties": {}, "required": ["a"], "additionalProperties": False},
            "does not declare",
        ),
        (
            {
                "type": "object",
                "properties": {"a": {"type": "string"}},
                "required": ["a", "a"],
                "additionalProperties": False,
            },
            "once each",
        ),
        ({**_SCHEMA, "$defs": {}}, 'keyword "$defs"'),
        ({**_SCHEMA, "properties": {"answer": {"type": "array"}}}, 'requires an "items"'),
        ({**_SCHEMA, "properties": {"answer": {"type": "string", "format": "email"}}}, '"format" must be'),
        ({**_SCHEMA, "properties": {"answer": {"type": "string", "minLength": -1}}}, "non-negative integer"),
        (
            {**_SCHEMA, "properties": {"answer": {"type": "integer", "minimum": 5, "maximum": 1}}},
            '"minimum" exceeds "maximum"',
        ),
    ],
)
def test_go_portable_schema_rejection_matrix(schema: dict[str, object], reason: str) -> None:
    failure = validate_portable_request(
        replace(_REQUEST, structured_output=StructuredOutput("answer", schema))
    )
    assert failure is not None and reason in failure.message


@pytest.mark.parametrize(
    "value,pointer",
    [
        ("not JSON", "the root"),
        ('{"name":"x","count":1}', "/name"),
        ('{"name":"good"}', "/count"),
        ('{"name":"good","count":1.5}', "/count"),
        ('{"name":"good","count":5}', "/count"),
        ('{"name":"good","count":1,"secret-value-canary":true}', "the root"),
        ('{"name":"good","count":1e999999}', "/count"),
    ],
)
def test_go_structured_instance_error_locations(value: str, pointer: str) -> None:
    schema = {
        "type": "object",
        "properties": {
            "name": {"type": "string", "minLength": 2},
            "count": {"type": "integer", "minimum": 0, "maximum": 3},
        },
        "required": ["name", "count"],
        "additionalProperties": False,
    }
    request = replace(_REQUEST, structured_output=StructuredOutput("answer", schema))
    with pytest.raises(ValueError) as caught:
        extract_structured_answer(request, value)
    assert str(caught.value).startswith(f"at {pointer}:")
    assert "secret-value-canary" not in str(caught.value)


@pytest.mark.parametrize(
    "generated_request,message",
    [
        (replace(_REQUEST, messages=()), "at least one message"),
        (replace(_REQUEST, messages=(Message(cast(MessageRole, "system"), "hello"),)), "role"),
        (replace(_REQUEST, messages=(Message(MessageRole.USER, ""),)), "requires text"),
        (replace(_REQUEST, max_output_tokens=-1), "maxOutputTokens"),
        (replace(_REQUEST, temperature=1.5), "from 0 through 1"),
        (replace(_REQUEST, temperature=-0.1), "non-negative"),
        (replace(_REQUEST, model="fixed-sampler", temperature=0.5), "does not accept a temperature"),
        (replace(_REQUEST, reasoning_effort="high"), '"high"'),
        (replace(_REQUEST, reasoning_effort="ultra"), "must be none"),
        (replace(_REQUEST, structured_output=StructuredOutput("bad name", _SCHEMA)), "name must match"),
        (
            replace(_REQUEST, structured_output=StructuredOutput("answer", {**_SCHEMA, "anyOf": []})),
            "schema is not portable",
        ),
    ],
)
@pytest.mark.asyncio
async def test_go_local_defect_matrix_makes_no_provider_request(
    generated_request: TextGenerationRequest, message: str
) -> None:
    seen: list[httpx.Request] = []

    def handler(http_request: httpx.Request) -> httpx.Response:
        seen.append(http_request)
        return httpx.Response(200, json=_completion())

    profile = Profile(
        "chat-lab",
        temperature=temperature_range(0, 1),
        model_rules=(ModelRule(model_id_prefix="fixed-sampler", temperature=temperature_not_accepted()),),
    )
    result = await _query(handler, profile).invoke(_call(), generated_request)
    assert result.branch == "defect"
    assert result.failure is not None and result.failure.kind == FailureKind.VALIDATION
    assert message in result.failure.message
    assert result.value is not None
    assert result.value.requested_model == (generated_request.model or "chat-model")
    assert not seen


@pytest.mark.asyncio
async def test_undeclared_feature_is_rejected_before_credential_or_provider() -> None:
    seen: list[httpx.Request] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        return httpx.Response(200, json=_completion())

    query = _query(handler)
    query._wire_format = replace(query._wire_format, features=RequestFeatures())
    result = await query.invoke(_call(), replace(_REQUEST, temperature=0.0))
    assert result.branch == "defect"
    assert result.failure is not None and "Temperature" in result.failure.message
    assert not seen


@pytest.mark.asyncio
async def test_schema_transform_and_json_object_instruction() -> None:
    requests: list[dict[str, Any]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(json.loads(request.content))
        return httpx.Response(200, json=_completion('{"answer":"yes"}'))

    profile = Profile(
        "chat-lab",
        structured_output=StructuredOutputRules(
            StructuredOutputMode.JSON_OBJECT_WITH_INSTRUCTION, ("minLength",)
        ),
    )
    request = replace(_REQUEST, structured_output=StructuredOutput("answer", _SCHEMA))
    result = await _query(handler, profile).invoke(_call(), request)
    assert result.branch == "generated"
    assert requests[0]["response_format"] == {"type": "json_object"}
    assert "JSON Schema named" in requests[0]["messages"][0]["content"]


@pytest.mark.asyncio
async def test_query_canonicalizes_schema_once(monkeypatch: pytest.MonkeyPatch) -> None:
    from dex_connectors.sdk.textgen import structured_output as validation

    canonicalize = validation.canonical_schema
    calls = 0

    def count_canonicalizations(output: StructuredOutput) -> dict[str, Any]:
        nonlocal calls
        calls += 1
        return canonicalize(output)

    monkeypatch.setattr(validation, "canonical_schema", count_canonicalizations)
    request = replace(_REQUEST, structured_output=StructuredOutput("answer", _SCHEMA))
    profile = Profile("chat-lab", structured_output=StructuredOutputRules(StructuredOutputMode.JSON_SCHEMA))
    query = _query(lambda http_request: httpx.Response(200, json=_completion('{"answer":"yes"}')), profile)
    assert (await query.invoke(_call(), request)).branch == "generated"
    assert calls == 1


@pytest.mark.asyncio
async def test_chat_model_rules_and_allowlist() -> None:
    requests: list[dict[str, Any]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(json.loads(request.content))
        return httpx.Response(200, json=_completion())

    profile = Profile(
        "chat-lab",
        allowed_request_fields=("model", "messages", "max_completion_tokens"),
        model_rules=(ModelRule(model_id_prefix="o1", max_tokens_field=MaxTokensField.MAX_COMPLETION_TOKENS),),
    )
    query = _query(handler, profile)
    result = await query.invoke(_call(), replace(_REQUEST, model="o1-mini", max_output_tokens=32))
    assert result.branch == "generated"
    assert requests[0]["max_completion_tokens"] == 32
    defect = await query.invoke(_call(), replace(_REQUEST, temperature=0.5))
    assert defect.branch == "defect"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_chat_stream_writes_deltas_before_terminal_finish() -> None:
    stream = (
        b'data: {"id":"resp-1","choices":[{"index":0,"delta":{"content":"Hel"}}]}\n\n'
        b'data: {"choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":"stop"}]}\n\n'
        b"data: [DONE]\n\n"
    )
    profile = Profile("chat-lab", streaming=StreamingPolicy.ALWAYS)
    query = _query(
        lambda request: httpx.Response(200, headers={"content-type": "text/event-stream"}, content=stream),
        profile,
    )
    result = await query.invoke(_call(), _REQUEST)
    assert result.branch == "generated"
    assert result.value is not None
    assert result.value.text == "Hello"


@pytest.mark.asyncio
async def test_chat_stream_retries_interruption_and_rejects_terminal_without_finish() -> None:
    profile = Profile("chat-lab", streaming=StreamingPolicy.ALWAYS)
    interrupted = b'data: {"choices":[{"index":0,"delta":{"content":"Hel"}}]}\n\n'
    terminal_without_finish = interrupted + b"data: [DONE]\n\n"
    replies = iter((interrupted, terminal_without_finish))

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, headers={"content-type": "text/event-stream"}, content=next(replies))

    query = _query(handler, profile)
    retry = await query.invoke(_call(), _REQUEST)
    assert retry.kind == "retry"
    assert retry.failure is not None and retry.failure.kind == FailureKind.TRANSPORT
    invalid = await query.invoke(_call(), _REQUEST)
    assert invalid.branch == "invalidResponse"
    assert invalid.failure is not None and invalid.failure.kind == FailureKind.PROTOCOL


@pytest.mark.asyncio
async def test_chat_stream_event_limit_selects_invalid_response() -> None:
    profile = Profile("chat-lab", streaming=StreamingPolicy.ALWAYS)
    stream = b'data: {"choices":[{"index":0,"delta":{"content":"' + b"x" * 2000 + b'"}}]}\n\n'
    query = _query(
        lambda request: httpx.Response(200, headers={"content-type": "text/event-stream"}, content=stream),
        profile,
    )
    result = await query.invoke(_call(), _REQUEST)
    assert result.branch == "invalidResponse"
    assert result.failure is not None and result.failure.kind == FailureKind.RESPONSE_TOO_LARGE


@pytest.mark.asyncio
async def test_reported_error_inside_success_uses_status_independent_rule() -> None:
    rule = ErrorRule(0, "overloaded", invalid_response_outcome())
    profile = Profile("chat-lab", error_rules=(rule,))
    query = _query(
        lambda request: httpx.Response(
            200, json={"error": {"code": "overloaded", "message": "secret prose"}}
        ),
        profile,
    )
    result = await query.invoke(_call(), _REQUEST)
    assert result.branch == "invalidResponse"
    assert result.failure is not None and "overloaded" in result.failure.message
    assert "secret prose" not in result.failure.message


@pytest.mark.parametrize(
    "body,branch,text,finish",
    [
        (
            {
                "choices": [
                    {
                        "index": 0,
                        "message": {
                            "content": [
                                {"type": "thinking", "thinking": "hidden"},
                                {"type": "text", "text": "A"},
                                {"type": "text", "text": "B"},
                            ],
                        },
                        "finish_reason": "stop",
                    }
                ],
                "usage": {"prompt_tokens": 12.0, "completion_tokens": 3},
            },
            "generated",
            "AB",
            "stop",
        ),
        (
            {
                "choices": [
                    {
                        "index": 0,
                        "message": {"content": None, "refusal": "secret prose"},
                        "finish_reason": "stop",
                    }
                ]
            },
            "blocked",
            "",
            "refusal",
        ),
        (
            {"choices": [{"index": 0, "message": {"content": None}, "finish_reason": "tool_calls"}]},
            "invalidResponse",
            "",
            "",
        ),
        (
            {
                "choices": [{"index": 0, "message": {"content": "x"}, "finish_reason": "stop"}],
                "usage": {"prompt_tokens": 1.5},
            },
            "invalidResponse",
            "",
            "",
        ),
        (
            {"choices": [{"index": 1, "message": {"content": "x"}, "finish_reason": "stop"}]},
            "invalidResponse",
            "",
            "",
        ),
    ],
)
@pytest.mark.asyncio
async def test_go_chat_provider_variant_matrix(
    body: dict[str, Any], branch: str, text: str, finish: str
) -> None:
    query = _query(lambda request: httpx.Response(200, json=body))
    result = await query.invoke(_call(), _REQUEST)
    assert result.branch == branch
    assert result.value is not None
    assert result.value.text == text
    if finish:
        assert result.value.finish_reason == finish


@pytest.mark.asyncio
async def test_attempt_records_inert_heartbeats_until_completion() -> None:
    async def handler(request: httpx.Request) -> httpx.Response:
        await asyncio.sleep(0.03)
        return httpx.Response(200, json=_completion())

    context = _Context()
    query = _query(handler)
    query._heartbeat_interval_seconds = 0.005
    result = await query.invoke(_call(context), _REQUEST)
    assert result.branch == "generated"
    assert context.heartbeats and all(item is None for item in context.heartbeats)


@pytest.mark.asyncio
async def test_stall_before_headers_retries_availability() -> None:
    async def handler(request: httpx.Request) -> httpx.Response:
        await asyncio.sleep(0.04)
        return httpx.Response(200, json=_completion())

    profile = Profile("chat-lab", stall_timeout_seconds=0.01)
    result = await _query(handler, profile).invoke(_call(), _REQUEST)
    assert result.kind == "retry"
    assert result.failure is not None and result.failure.kind == FailureKind.AVAILABILITY


def test_query_rejects_async_durability() -> None:
    with pytest.raises(ValueError, match="sync Execute"):
        definition = QueryDefinition(
            _OPERATION, text_generation_branch_definitions(), StepDefaults(timedelta(minutes=1), 2)
        )

        async def credential(call: Call) -> SecretString:
            return SecretString("sk-test")

        TextGenerationQuery(
            TextGenerationQueryConfig(
                definition,
                new_wire_format(Profile("chat-lab")),
                "https://provider.example",
                "chat-model",
                10,
                credential,
                65536,
                1024,
            )
        )
