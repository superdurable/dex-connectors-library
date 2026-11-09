# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""AWS Signature Version 4 header signing for DynamoDB JSON requests."""

import hashlib
import hmac
from datetime import UTC, datetime
from urllib.parse import quote, unquote, urlsplit

import httpx

_ALGORITHM = "AWS4-HMAC-SHA256"


def sign_headers(
    *,
    method: str,
    url: str,
    headers: dict[str, str],
    body: bytes,
    access_key_id: str,
    secret_access_key: str,
    session_token: str,
    region: str,
    service: str,
    signing_time: datetime,
) -> dict[str, str]:
    """Return headers plus X-Amz-Date, any session token, and the Authorization header.

    Every supplied header, the host, and a nonzero content length are signed, matching the
    canonical request the AWS Go SDK signer builds for the same request.
    """
    parsed = urlsplit(url)
    amz_date = signing_time.astimezone(UTC).strftime("%Y%m%dT%H%M%SZ")
    scope_date = amz_date[:8]
    signed = dict(headers)
    signed["X-Amz-Date"] = amz_date
    if session_token:
        signed["X-Amz-Security-Token"] = session_token
    canonical = {name.lower(): " ".join(value.strip().split()) for name, value in signed.items()}
    canonical["host"] = httpx.URL(url).netloc.decode()
    if body:
        canonical["content-length"] = str(len(body))
    signed_header_names = ";".join(sorted(canonical))
    canonical_headers = "".join(f"{name}:{canonical[name]}\n" for name in sorted(canonical))
    canonical_request = "\n".join(
        (
            method,
            quote(unquote(parsed.path or "/"), safe="/-_.~"),
            _canonical_query(parsed.query),
            canonical_headers,
            signed_header_names,
            hashlib.sha256(body).hexdigest(),
        )
    )
    scope = f"{scope_date}/{region}/{service}/aws4_request"
    string_to_sign = "\n".join(
        (_ALGORITHM, amz_date, scope, hashlib.sha256(canonical_request.encode()).hexdigest())
    )
    key = _hmac(("AWS4" + secret_access_key).encode(), scope_date)
    for part in (region, service, "aws4_request"):
        key = _hmac(key, part)
    signature = hmac.new(key, string_to_sign.encode(), hashlib.sha256).hexdigest()
    signed["Authorization"] = (
        f"{_ALGORITHM} Credential={access_key_id}/{scope}, SignedHeaders={signed_header_names}, "
        f"Signature={signature}"
    )
    return signed


def _hmac(key: bytes, message: str) -> bytes:
    return hmac.new(key, message.encode(), hashlib.sha256).digest()


def _canonical_query(query: str) -> str:
    if not query:
        return ""
    pairs = []
    for item in query.split("&"):
        name, _, value = item.partition("=")
        pairs.append((quote(name, safe="-_.~"), quote(value, safe="-_.~")))
    return "&".join(f"{name}={value}" for name, value in sorted(pairs))
