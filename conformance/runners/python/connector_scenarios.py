# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Test-only validation and provider replay for connector conformance fixtures."""

from __future__ import annotations

import base64
import json
import os
import re
from collections.abc import Awaitable, Callable
from dataclasses import fields
from datetime import UTC, datetime, timedelta
from email.message import Message
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs

import httpx
from dex import CodecRegistry, Value, WireKind
from dex_connectors import sdk
from dex_connectors.sdk.singledispatch import Marker

_NO_HEARTBEAT_VALUE = object()


class HeartbeatContext:
    """Persist the connector's encoded heartbeat across fixture attempts."""

    def __init__(self, prior_heartbeat: dict[str, Any] | None = None,
                 fixed_clock: str | None = None, scenario_name: str = "") -> None:
        self.scenario_name = scenario_name
        self.attempt = prior_heartbeat["attempt"] + 1 if prior_heartbeat is not None else 1
        self.run_id = "run"
        self.fixed_clock = (datetime.fromisoformat(fixed_clock)
                            if fixed_clock is not None else datetime.fromtimestamp(1, UTC))
        self.flow_started_at = datetime.fromtimestamp(1, UTC)
        self.deadline = datetime.now(UTC) + timedelta(seconds=30)
        self.heartbeat_payload = (prior_heartbeat.get("value", prior_heartbeat)
                                  if prior_heartbeat is not None else None)
        self.has_heartbeat_value = prior_heartbeat is not None
        self.has_written_heartbeat = False

    def has_last_heartbeat_value(self) -> bool:
        """Report whether a prior Dex attempt persisted a heartbeat."""
        return self.has_heartbeat_value

    def get_last_heartbeat_value(self, value_type: type[object]) -> object:
        """Decode the exact Go-recorded payload into the requested heartbeat type."""
        if not self.has_heartbeat_value:
            return None
        if value_type is Marker:
            return Marker.from_wire(self.heartbeat_payload)
        return CodecRegistry().resolve(value_type).decode(
            Value(WireKind.JSON, json.dumps(self.heartbeat_payload))
        )

    async def heartbeat(self, value: object = _NO_HEARTBEAT_VALUE) -> None:
        """Record the last heartbeat for the next fixture invocation."""
        is_clear = value is _NO_HEARTBEAT_VALUE
        encoded = (None if is_clear else value.to_wire() if isinstance(value, Marker)
                   else json.loads(CodecRegistry().resolve(type(value)).encode(value).data))
        if os.environ.get("CONNECTOR_CONFORMANCE_MUTATE_HEARTBEAT") == self.scenario_name:
            encoded = {"__wrong_heartbeat_shape__": True}
            is_clear = False
        if is_clear and os.environ.get("CONNECTOR_CONFORMANCE_MUTATE_CLEAR_AS_NULL") == self.scenario_name:
            is_clear = False
        self.heartbeat_payload = encoded
        self.has_heartbeat_value = not is_clear
        self.has_written_heartbeat = True


def _keys(value: object, required: set[str], optional: set[str], where: str) -> dict[str, Any]:
    if not isinstance(value, dict) or required - value.keys() or value.keys() - required - optional:
        raise ValueError(f"{where}: missing or unknown fields")
    return value


def _same_json_value(actual: object, expected: object) -> bool:
    if type(actual) is not type(expected):
        return False
    if isinstance(actual, dict):
        return actual.keys() == expected.keys() and all(
            _same_json_value(value, expected[key]) for key, value in actual.items()
        )
    if isinstance(actual, list):
        return len(actual) == len(expected) and all(
            _same_json_value(value, item) for value, item in zip(actual, expected)
        )
    return actual == expected


def load_scenarios(path: Path) -> list[dict[str, Any]]:
    """Reject incomplete and unknown fixture fields before a provider is invoked."""
    path = Path(os.environ.get("CONNECTOR_CONFORMANCE_FIXTURE", path))
    document = _keys(json.loads(path.read_text()), {"schemaVersion", "connector", "scenarios"}, set(), str(path))
    if document["schemaVersion"] != 1 or not isinstance(document["connector"], str) or not document["connector"]:
        raise ValueError(f"{path}: invalid schema version")
    scenarios = document["scenarios"]
    if not isinstance(scenarios, list) or not scenarios:
        raise ValueError(f"{path}: scenarios must be nonempty")
    for scenario in scenarios:
        _keys(scenario, {"name", "operation", "callId", "input", "exchanges", "expected"},
              {"idempotencyKey", "attempts", "intermediate", "priorHeartbeat", "fixedClock"}, str(path))
        if any(not isinstance(scenario[field], str) or not scenario[field]
               for field in ("name", "operation", "callId")):
            raise ValueError(f"{scenario['name']}: invalid Call ID")
        if not isinstance(scenario["input"], dict) or not isinstance(scenario["exchanges"], list):
            raise TypeError(f"{scenario['name']}: invalid input or exchanges")
        for exchange in scenario["exchanges"]:
            _keys(exchange, {"method", "path", "keyMode"},
                  {"status", "headers", "body", "query", "form", "requestBody", "requestHeaders", "transportFailure", "derivedKey"},
                  scenario["name"])
            if ("transportFailure" in exchange) == ("status" in exchange):
                raise ValueError(f"{scenario['name']}: exchange requires exactly one response or transport failure")
            if any(not isinstance(exchange[field], str) or not exchange[field] for field in ("method", "path")):
                raise ValueError(f"{scenario['name']}: invalid request method or path")
            if "status" in exchange and not {"headers", "body"} <= exchange.keys():
                raise ValueError(f"{scenario['name']}: response headers and body are required")
            if "status" in exchange and (
                type(exchange["status"]) is not int or not isinstance(exchange["headers"], dict)
                or not isinstance(exchange["body"], str)
            ):
                raise ValueError(f"{scenario['name']}: invalid response")
            if "transportFailure" in exchange and {"headers", "body"} & exchange.keys():
                raise ValueError(f"{scenario['name']}: transport failure cannot return a response")
            if exchange.get("transportFailure", "before-send") not in {"before-send", "after-send"}:
                raise ValueError(f"{scenario['name']}: invalid transport failure")
            if exchange["keyMode"] not in {"absent", "equals-call-id", "stable-nonempty", "derived"}:
                raise ValueError(f"{scenario['name']}: invalid key mode")
            if exchange["keyMode"] == "derived" and not exchange.get("derivedKey"):
                raise ValueError(f"{scenario['name']}: derived key is missing")
            for field in ("query", "form", "requestHeaders"):
                if field in exchange and (not isinstance(exchange[field], dict)
                                          or any(not isinstance(key, str) or not (isinstance(value, str) or
                                              isinstance(value, list) and bool(value) and all(isinstance(item, str) for item in value))
                                                 for key, value in exchange[field].items())):
                    raise ValueError(f"{scenario['name']}: invalid {field}")
            body = exchange.get("requestBody")
            if isinstance(body, dict) and "$multipart" in body and (
                set(body) != {"$multipart"} or not isinstance(body["$multipart"], list)
                or any(not isinstance(part, dict) or set(part) != {"headers", "bodyBase64"}
                       or not isinstance(part["headers"], dict)
                       or any(not isinstance(name, str) or not isinstance(values, list)
                              or not values or any(not isinstance(value, str) for value in values)
                              for name, values in part["headers"].items())
                       or not isinstance(part["bodyBase64"], str) for part in body["$multipart"])
            ):
                raise ValueError(f"{scenario['name']}: invalid multipart expectation")
            if isinstance(body, dict) and "$rawMime" in body and (
                set(body) - {"$rawMime", "threadId"} or not isinstance(body["$rawMime"], str)
            ):
                raise ValueError(f"{scenario['name']}: invalid raw MIME expectation")
            if isinstance(body, dict) and "$rawMimeContains" in body and (
                not set(body) <= {"$rawMimeContains", "threadId"} or not isinstance(body["$rawMimeContains"], list)
                or not body["$rawMimeContains"]
                or any(not isinstance(fragment, str) or not fragment for fragment in body["$rawMimeContains"])
                or ("threadId" in body and (not isinstance(body["threadId"], str) or not body["threadId"]))
            ):
                raise ValueError(f"{scenario['name']}: invalid raw MIME assertions")
        _keys(scenario["expected"], {"branch", "failureKind", "retry", "retryAfterMs", "uncertain", "requestCount"},
              {"output", "heartbeat"}, scenario["name"])
        expected = scenario["expected"]
        if (any(not isinstance(expected[field], str) for field in ("branch", "failureKind"))
                or any(type(expected[field]) is not bool for field in ("retry", "uncertain"))
                or any(type(expected[field]) is not int for field in ("retryAfterMs", "requestCount"))):
            raise ValueError(f"{scenario['name']}: invalid expected decision")
        for intermediate in scenario.get("intermediate", []):
            _keys(intermediate, {"failureKind", "retryAfterMs", "exchangeCount"}, {"heartbeat"}, scenario["name"])
        if "priorHeartbeat" in scenario:
            prior = _keys(scenario["priorHeartbeat"], {"attempt"},
                          {"value", "callId", "state", "providerId"}, scenario["name"])
            if type(prior["attempt"]) is not int or prior["attempt"] < 1:
                raise ValueError(f"{scenario['name']}: invalid prior heartbeat attempt")
            if "value" in prior:
                if set(prior) != {"attempt", "value"} or prior["value"] is None:
                    raise ValueError(f"{scenario['name']}: invalid prior heartbeat value")
            else:
                _keys(prior, {"callId", "attempt", "state"}, {"providerId"}, scenario["name"])
                if prior["callId"] != scenario["callId"]:
                    raise ValueError(f"{scenario['name']}: prior heartbeat Call ID differs")
                Marker.from_wire(prior)
        if "fixedClock" in scenario:
            if (not isinstance(scenario["fixedClock"], str)
                    or re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,3})?Z",
                                    scenario["fixedClock"]) is None):
                raise ValueError(f"{scenario['name']}: invalid fixed clock")
            datetime.fromisoformat(scenario["fixedClock"])
        if scenario.get("attempts", 1) != len(scenario.get("intermediate", [])) + 1:
            raise ValueError(f"{scenario['name']}: inconsistent attempts")
        if "idempotencyKey" in scenario and not isinstance(scenario["idempotencyKey"], str):
            raise ValueError(f"{scenario['name']}: idempotency key must be a string")
    return scenarios


def _multipart_media_type(content_type: str) -> tuple[str, tuple[tuple[str, str], ...]]:
    message = Message()
    message["Content-Type"] = content_type
    parameter_names = [name.lower() for name, _ in (message.get_params() or [])[1:]]
    if len(parameter_names) != len(set(parameter_names)):
        raise ValueError("duplicate multipart media type parameter")
    media_type = message.get_content_type()
    parameters = tuple(sorted((name, value) for name, value in (message.get_params() or [])[1:]
                              if name != "boundary"))
    return media_type, parameters


def _is_multipart(content_type: str) -> bool:
    return _multipart_media_type(content_type)[0] in {"multipart/form-data", "multipart/mixed"}


def _multipart_body(content_type: str, body: bytes) -> dict[str, object]:
    message = Message()
    message["Content-Type"] = content_type
    boundary = message.get_param("boundary")
    if not _is_multipart(content_type) or not isinstance(boundary, str) or not boundary:
        raise ValueError("missing multipart boundary")
    delimiter = b"--" + boundary.encode("ascii")
    if not body.startswith(delimiter):
        raise ValueError("invalid multipart opening delimiter")
    position = len(delimiter)
    parts: list[dict[str, object]] = []
    while True:
        if body[position:position + 2] == b"--":
            break
        if body[position:position + 2] != b"\r\n":
            raise ValueError("invalid multipart delimiter")
        header_start = position + 2
        header_end = body.find(b"\r\n\r\n", header_start)
        if header_end < 0:
            raise ValueError("missing multipart part headers")
        part_headers: dict[str, list[str]] = {}
        last_name = ""
        for raw_line in body[header_start:header_end].split(b"\r\n"):
            if b"\r" in raw_line or b"\n" in raw_line:
                raise ValueError("invalid multipart part header")
            if raw_line.startswith((b" ", b"\t")):
                if not last_name:
                    raise ValueError("invalid multipart part header")
                part_headers[last_name][-1] += " " + raw_line.strip(b" \t").decode("utf-8")
                continue
            name_bytes, separator, value_bytes = raw_line.partition(b":")
            if not separator or not re.fullmatch(rb"[!#$%&'*+.^_`|~0-9A-Za-z-]+", name_bytes):
                raise ValueError("invalid multipart part header")
            name = "-".join(component[:1].upper() + component[1:].lower()
                            for component in name_bytes.decode("ascii").split("-"))
            part_headers.setdefault(name, []).append(value_bytes.strip(b" \t").decode("utf-8"))
            last_name = name
        content_start = header_end + 4
        part_delimiter = b"\r\n" + delimiter
        content_end = body.find(part_delimiter, content_start)
        while content_end >= 0 and body[content_end + len(part_delimiter):content_end + len(part_delimiter) + 2] not in (b"\r\n", b"--"):
            content_end = body.find(part_delimiter, content_end + 1)
        if content_end < 0:
            raise ValueError("missing multipart closing delimiter")
        parts.append({"headers": part_headers, "bodyBase64": base64.b64encode(body[content_start:content_end]).decode("ascii")})
        position = content_end + 2 + len(delimiter)
    return {"$multipart": parts}


class ExchangeRecorder:
    """Return scripted responses while retaining every transport assertion failure."""

    def __init__(self, scenario: dict[str, Any], call_id: str, path_prefix: str = "") -> None:
        self.scenario = scenario
        self.call_id = call_id
        self.path_prefix = path_prefix
        self.request_count = 0
        self.provider_request_count = 0
        self.mismatches: list[str] = []
        self.stable_key = ""

    async def answer(self, request: httpx.Request) -> httpx.Response:
        """Check method, path, optional fields, and key without raising assertions."""
        exchanges = self.scenario["exchanges"]
        index = self.request_count
        self.request_count += 1
        if index >= len(exchanges):
            self.mismatches.append(f"request {index}: unexpected provider request")
            return httpx.Response(599, content=b"")
        exchange = exchanges[index]
        if exchange.get("transportFailure") != "before-send":
            self.provider_request_count += 1
        self._equal(index, "method", request.method, exchange["method"])
        self._equal(index, "path", request.url.path, self.path_prefix + exchange["path"])
        for field, actual in (("query", parse_qs(request.url.query.decode(), keep_blank_values=True)),
                              ("form", parse_qs(request.content.decode(), keep_blank_values=True)
                               if request.headers.get("content-type", "").startswith("application/x-www-form-urlencoded") else {})):
            for key, value in exchange.get(field, {}).items():
                self._equal(index, f"{field} {key}", actual.get(key), value if isinstance(value, list) else [value])
            for key, values in actual.items():
                if key not in exchange.get(field, {}):
                    self.mismatches.append(f"request {index}: unasserted {field} {key}={values!r}")
        if "requestBody" in exchange:
            expected_body = exchange["requestBody"]
            actual_body = "" if isinstance(expected_body, dict) and "$multipart" in expected_body else request.content.decode()
            if isinstance(expected_body, dict) and "$multipart" in expected_body:
                try:
                    self._equal(index, "multipart body", _multipart_body(request.headers.get("content-type", ""), request.content), expected_body)
                except ValueError as error:
                    self.mismatches.append(f"request {index}: invalid multipart body: {error}")
            elif isinstance(expected_body, dict) and "$rawMimeContains" in expected_body:
                try:
                    payload = json.loads(actual_body)
                    expected_keys = {"raw"} | ({"threadId"} if "threadId" in expected_body else set())
                    if set(payload) != expected_keys:
                        self.mismatches.append(f"request {index}: invalid MIME request body")
                    if "threadId" in expected_body:
                        self._equal(index, "threadId", payload.get("threadId"), expected_body["threadId"])
                    raw = payload["raw"]
                    mime = base64.urlsafe_b64decode(raw + "=" * (-len(raw) % 4)).decode()
                except (KeyError, TypeError, ValueError):
                    mime = ""
                for fragment in expected_body["$rawMimeContains"]:
                    if fragment not in mime:
                        self.mismatches.append(f"request {index}: MIME body lacks {fragment!r}")
            elif isinstance(expected_body, dict) and "$rawMime" in expected_body:
                try:
                    payload = json.loads(actual_body)
                    expected_keys = {"raw"} | ({"threadId"} if "threadId" in expected_body else set())
                    if set(payload) != expected_keys:
                        self.mismatches.append(f"request {index}: invalid MIME request body")
                    if "threadId" in expected_body:
                        self._equal(index, "threadId", payload.get("threadId"), expected_body["threadId"])
                    raw = payload["raw"]
                    mime = base64.urlsafe_b64decode(raw + "=" * (-len(raw) % 4)).decode()
                    self._equal(index, "MIME body", mime, expected_body["$rawMime"])
                except (KeyError, TypeError, ValueError):
                    self.mismatches.append(f"request {index}: invalid raw MIME encoding")
            elif isinstance(expected_body, (dict, list)):
                try:
                    actual_body = json.loads(actual_body)
                except ValueError:
                    pass
                self._equal(index, "body", actual_body, expected_body)
            else:
                self._equal(index, "body", actual_body, expected_body)
        elif not exchange.get("form") and request.content:
            self.mismatches.append(f"request {index}: unasserted body")
        for name, value in exchange.get("requestHeaders", {}).items():
            expected_values = value if isinstance(value, list) else [value]
            actual_values = request.headers.get_list(name)
            if name.lower() == "content-type" and len(expected_values) == len(actual_values) == 1 and _is_multipart(expected_values[0]):
                try:
                    self._equal(index, f"header {name}", _multipart_media_type(actual_values[0]), _multipart_media_type(expected_values[0]))
                except ValueError as error:
                    self.mismatches.append(f"request {index}: invalid header {name}: {error}")
            else:
                self._equal(index, f"header {name}", actual_values, expected_values)
        ignored_headers = {"host", "accept-encoding", "connection", "content-length"}
        for name, value in request.headers.items():
            if name in ignored_headers or name == "idempotency-key":
                continue
            if name == "accept" and value == "*/*" or name == "user-agent" and value.startswith("python-httpx/"):
                continue
            if name not in {key.lower() for key in exchange.get("requestHeaders", {})}:
                self.mismatches.append(f"request {index}: unasserted header {name}={value!r}")
        key_values = request.headers.get_list("Idempotency-Key")
        key = key_values[0] if len(key_values) == 1 else ""
        mode = exchange["keyMode"]
        if mode == "absent":
            self._equal(index, "Idempotency-Key", key_values, [])
        elif mode == "equals-call-id":
            self._equal(index, "Idempotency-Key", key_values, [self.call_id])
        elif mode == "stable-nonempty":
            if len(key_values) != 1 or not key:
                self.mismatches.append(f"request {index}: Idempotency-Key needs one nonempty value")
            elif self.stable_key and key != self.stable_key:
                self.mismatches.append(f"request {index}: Idempotency-Key changed")
            self.stable_key = key
        else:
            self._equal(index, "Idempotency-Key", key_values, [exchange.get("derivedKey", "")])
        if "transportFailure" in exchange:
            failure = exchange["transportFailure"]
            if failure == "before-send":
                trace = request.extensions.get("trace")
                if trace is not None:
                    await trace("connection.connect_tcp.failed", {})
                raise httpx.ConnectError("fixture connection failed", request=request)
            if failure == "after-send":
                raise httpx.ReadError("fixture response lost", request=request)
            self.mismatches.append(f"request {index}: unknown transport failure {failure}")
            raise httpx.ReadError("fixture transport failure", request=request)
        return httpx.Response(exchange["status"], headers=exchange["headers"], content=exchange["body"].encode())

    def _equal(self, index: int, field: str, actual: object, expected: object) -> None:
        if actual != expected:
            self.mismatches.append(f"request {index}: {field}: {actual!r} != {expected!r}")

    def assert_complete(self) -> None:
        """Surface mismatches after the connector has finished its error handling."""
        expected = self.scenario["expected"]["requestCount"]
        assert self.request_count == len(self.scenario["exchanges"]), "unexpected transport attempt count"
        assert self.provider_request_count == expected, (
            f"provider request count {self.provider_request_count}, expected {expected}"
        )
        assert not self.mismatches, "\n".join(self.mismatches)


class NeverRefreshCredentials:
    """Record an unexpected credential exchange after the connector catches it."""

    def __init__(self, recorder: ExchangeRecorder) -> None:
        self.recorder = recorder

    def refresh_required(self, _state: object) -> bool:
        """Keep fixture credentials unchanged."""
        return False

    async def refresh(self, _state: object) -> object:
        """Reject a refresh without losing the mismatch to connector handling."""
        self.recorder.mismatches.append("unexpected credential refresh")
        raise RuntimeError("unexpected credential refresh")


def assert_attempt(attempt: object, expected: dict[str, Any]) -> None:
    """Match a Python SDK attempt to Go's decision and optional output."""
    kind = "retry" if expected["retry"] else "uncertain" if expected["uncertain"] else "branch"
    assert attempt.kind == kind
    assert attempt.branch == expected["branch"]
    assert (attempt.failure.kind.value if attempt.failure else "") == expected["failureKind"]
    assert abs(attempt.retry_after_seconds * 1000 - expected["retryAfterMs"]) < 0.001
    if "output" in expected:
        encoded = CodecRegistry().resolve(type(attempt.value)).encode(attempt.value)
        observed = json.loads(encoded.data)
        assert _same_json_value(observed, expected["output"]), (
            f"output {observed!r} differs from {expected['output']!r}"
        )


class PythonOperationAdapter:
    """Construct one typed connector operation for each replayed Dex attempt."""

    def __init__(self, connector_id: str, connection: sdk.ConnectionRef, module: object,
                 create_client: Callable[[httpx.AsyncClient, ExchangeRecorder], object],
                 operation_types: dict[str, type] | None = None, with_context: bool = False) -> None:
        self.connector_id = connector_id
        self.connection = connection
        self.module = module
        self.create_client = create_client
        self.operation_types = operation_types or {}
        self.with_context = with_context

    async def invoke(self, scenario: dict[str, Any], recorder: ExchangeRecorder,
                     context: HeartbeatContext) -> object:
        """Map fixture input and identity into the package's public operation API."""
        operation_name = scenario["operation"]
        async with httpx.AsyncClient(transport=httpx.MockTransport(recorder.answer)) as http_client:
            client = self.create_client(http_client, recorder)
            operation = (self.operation_types[operation_name](client) if operation_name in self.operation_types
                         else getattr(client, self._snake_case(operation_name))())
            input_type = getattr(self.module, operation_name[0].upper() + operation_name[1:] + "Input")
            fields_by_wire_name = {field.metadata.get("dex_json_name", field.name): field
                                   for field in fields(input_type)}
            input_arguments = {}
            for key, value in scenario["input"].items():
                field = fields_by_wire_name[key]
                if field.metadata.get("dex_json_format") == "rfc3339" and isinstance(value, str):
                    value = datetime.fromisoformat(value.replace("Z", "+00:00"))
                input_arguments[field.name] = value
            input_value = input_type(**input_arguments)
            call_arguments = {"context": context} if self.with_context else {}
            call = sdk.Call(sdk.CallID(scenario["callId"]), self.connection,
                            sdk.OperationRef(self.connector_id, operation_name),
                            sdk.IdempotencyKey(""), **call_arguments)
            if hasattr(operation, "idempotency_key"):
                return await invoke_production_mutation(operation, call, input_value, scenario["idempotencyKey"])
            return await operation.invoke(call, input_value)

    @staticmethod
    def _snake_case(value: str) -> str:
        groups = re.sub(r"([A-Z]+)([A-Z][a-z])", r"\1_\2", value)
        return re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", groups).lower()


async def invoke_production_mutation(operation: object, call: sdk.Call, input_value: object,
                                     expected_key: str) -> object:
    """Use the SDK mutation path and capture the operation's classified attempt."""
    class RecordingMutation:
        def __init__(self) -> None:
            self.attempt: object | None = None
            self.key = ""

        def definition(self) -> object:
            return operation.definition()

        def idempotency_key(self, call_id: sdk.CallID, input: object) -> sdk.IdempotencyKey:
            return operation.idempotency_key(call_id, input)

        async def invoke(self, invocation_call: sdk.Call, input: object) -> object:
            self.key = str(invocation_call.idempotency_key)
            self.attempt = await operation.invoke(invocation_call, input)
            return self.attempt

    recording = RecordingMutation()
    try:
        await sdk.run_mutation(recording, call, input_value)
    except sdk.RetryError:
        pass
    assert recording.key == expected_key, f"production idempotency key {recording.key!r} differs from {expected_key!r}"
    assert recording.attempt is not None
    return recording.attempt

def static_credentials(connection: sdk.ConnectionRef, credential: object) -> sdk.StaticCredentialProvider:
    """Bind one inert test credential to the connector's static connection."""
    return sdk.StaticCredentialProvider({connection: credential})


async def replay(
    scenario: dict[str, Any],
    invoke: Callable[[dict[str, Any], ExchangeRecorder, HeartbeatContext], Awaitable[object]],
    path_prefix: str = "",
) -> None:
    """Replay all attempts, then check transport mismatches and the final decision."""
    recorder = ExchangeRecorder(scenario, scenario["callId"], path_prefix)
    context = HeartbeatContext(scenario.get("priorHeartbeat"), scenario.get("fixedClock"), scenario["name"])
    for intermediate in scenario.get("intermediate", []):
        context.has_written_heartbeat = False
        attempt = await invoke(scenario, recorder, context)
        assert_attempt(attempt, {"retry": True, "uncertain": False, "branch": "", **intermediate})
        assert recorder.request_count == intermediate["exchangeCount"]
        if os.environ.get("CONNECTOR_CONFORMANCE_MUTATE_EXTRA_HEARTBEAT") == scenario["name"] and "heartbeat" not in intermediate:
            await context.heartbeat({"__extra_heartbeat__": True})
        assert context.has_written_heartbeat == ("heartbeat" in intermediate), (
            f"{scenario['name']}: heartbeat write differs from Go recording"
        )
        if "heartbeat" in intermediate:
            _assert_recorded_heartbeat(scenario["name"], context, intermediate["heartbeat"])
        context.attempt += 1
    context.has_written_heartbeat = False
    attempt = await invoke(scenario, recorder, context)
    recorder.assert_complete()
    assert_attempt(attempt, scenario["expected"])
    expected = scenario["expected"]
    if os.environ.get("CONNECTOR_CONFORMANCE_MUTATE_EXTRA_HEARTBEAT") == scenario["name"] and "heartbeat" not in expected:
        await context.heartbeat({"__extra_heartbeat__": True})
    assert context.has_written_heartbeat == ("heartbeat" in expected), (
        f"{scenario['name']}: heartbeat write differs from Go recording"
    )
    if "heartbeat" in expected:
        _assert_recorded_heartbeat(scenario["name"], context, expected["heartbeat"])


def _assert_recorded_heartbeat(name: str, context: HeartbeatContext, recorded: object) -> None:
    is_clear = (isinstance(recorded, dict) and set(recorded) == {"cleared"}
                and recorded["cleared"] is True)
    assert context.has_heartbeat_value != is_clear, f"{name}: heartbeat clear differs from Go recording"
    if not is_clear:
        assert _same_json_value(context.heartbeat_payload, recorded), (
            f"{name}: heartbeat {context.heartbeat_payload!r} differs from Go recording {recorded!r}"
        )
    context.heartbeat_payload = None if is_clear else recorded
    context.has_heartbeat_value = not is_clear
