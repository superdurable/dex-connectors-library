# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# SendRequestInput describes one HTTP request to the connection's API.
@dataclass(frozen=True, slots=True)
class SendRequestInput:
    # Method is GET, HEAD, OPTIONS, POST, PUT, PATCH, or DELETE, compared without case. Blank means GET.
    method: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Path is the request path under the connection's apiBaseUrl. It starts with a single slash, may be
    # percent-encoded, and carries no query, fragment, backslash, or .. segment. Set exactly one of Path and URL.
    path: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # URL is an absolute http or https URL with the scheme, host, and port of the connection's apiBaseUrl,
    # such as a next-page link the API returned. Set exactly one of Path and URL.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Query holds query parameters that are added to those already in URL. A name may repeat through several values.
    query: dict[str, tuple[str, ...]] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    # Headers holds extra request headers, such as Accept. Authorization, Cookie, Host, and the headers the
    # transport owns select defect; the connector always sends Authorization: Bearer with the access token.
    headers: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    # Body is the request body, sent as text. It is not allowed with GET or HEAD and is limited to the
    # connection's maxBodyBytes. Content-Type defaults to application/json when Headers sets none.
    body: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IdempotencyKeyHeader names a header, such as Idempotency-Key, that carries the Step's idempotency key,
    # which is the same for every retry of one Step execution. Blank sends none.
    idempotency_key_header: str = _dataclass_field(default='', metadata={'dex_json_name': 'idempotencyKeyHeader', 'dex_json_omit_default': True})


setattr(SendRequestInput, '__module__', 'dex_connectors.superdurable_oauth2_http.models')


# SendRequestOutput is the API's answer. It never holds a credential: the access token is replaced by
# [REDACTED] if the API echoes it.
@dataclass(frozen=True, slots=True)
class SendRequestOutput:
    # StatusCode is the final HTTP status, or zero when no request was sent or none was answered.
    status_code: int = _dataclass_field(default=0, metadata={'dex_json_name': 'statusCode'})
    # ContentType is the response Content-Type header, when it is short printable text.
    content_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentType', 'dex_json_omit_default': True})
    # Body is the response body, cut at the connection's maxBodyBytes. It is base64 text when IsBodyBase64 is set.
    body: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsBodyBase64 reports that the body was not valid UTF-8, so Body holds its base64 encoding.
    body_is_base64: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'bodyIsBase64', 'dex_json_omit_default': True})
    # IsBodyTruncated reports that the API sent more than maxBodyBytes bytes and the rest was dropped.
    body_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'bodyTruncated', 'dex_json_omit_default': True})


setattr(SendRequestOutput, '__module__', 'dex_connectors.superdurable_oauth2_http.models')
