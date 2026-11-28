# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zilliz provider transport and Go-compatible Query decisions."""

import json
from datetime import UTC, datetime

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_CLUSTERS_DEFINITION
from .models import ClusterSummary, ListClustersInput, ListClustersOutput


def _opaque(value: str, limit: int = 1024) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= limit and all(0x20 < ord(char) < 0x7F for char in value)
    )


class Client:
    """Resolve credentials for each listClusters request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("Zilliz configuration and credentials are required")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def list_clusters(self) -> "ListClustersOperation":
        """Bind the listClusters Query."""
        return ListClustersOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListClustersOperation:
    """Query Zilliz and classify the provider outcome."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return LIST_CLUSTERS_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[ListClustersOutput]:
        return QueryAttempt.branch_result(
            branch,
            ListClustersOutput(),
            Receipt(),
            Failure(kind, "zilliz", "listClusters", "Zilliz request failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[ListClustersOutput]:
        return QueryAttempt.retry(Failure(kind, "zilliz", "listClusters", "Zilliz request failed"), delay)

    def _request(
        self, input: ListClustersInput, credentials: Credentials
    ) -> httpx.Request | QueryAttempt[ListClustersOutput]:
        token = credentials.api_key.reveal()
        if not _opaque(token):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        return self.client.http_client.build_request(
            "GET",
            "https://api.cloud.zilliz.com/v2/clusters?pageSize=25&currentPage=1",
            headers={"Accept": "application/json", "Authorization": "Bearer " + token},
        )

    def _decode(
        self, wire: object, input: ListClustersInput
    ) -> ListClustersOutput | QueryAttempt[ListClustersOutput] | None:
        if not isinstance(wire, dict) or type(wire.get("code")) is not int:
            return None
        if wire["code"] != 0:
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        data = wire.get("data")
        if not isinstance(data, dict):
            return None
        count = data.get("count")
        items = data.get("clusters")
        if type(count) is not int or count < 0 or not isinstance(items, list) or len(items) > 25:
            return None
        summaries = []
        for item in items:
            if not isinstance(item, dict):
                return None
            cluster_id = "" if item.get("clusterId") is None else item["clusterId"]
            name = "" if item.get("clusterName") is None else item["clusterName"]
            region_id = "" if item.get("regionId") is None else item["regionId"]
            plan = "" if item.get("plan") is None else item["plan"]
            status = "" if item.get("status") is None else item["status"]
            if not all(isinstance(value, str) for value in (cluster_id, name, region_id, plan, status)):
                return None
            if (
                not cluster_id
                or len(cluster_id.encode("utf-8", "replace")) > 128
                or cluster_id.strip() != cluster_id
                or len(name.encode("utf-8", "replace")) > 200
                or len(region_id.encode("utf-8", "replace")) > 100
                or len(plan.encode("utf-8", "replace")) > 100
                or len(status.encode("utf-8", "replace")) > 100
            ):
                return None
            summaries.append(ClusterSummary(cluster_id, name, region_id, plan, status))
        return ListClustersOutput(total=count, clusters=tuple(summaries))

    async def invoke(self, call: Call, input: ListClustersInput) -> QueryAttempt[ListClustersOutput]:
        """Read one bounded provider response without persisting credentials."""
        if not isinstance(input, ListClustersInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        request = self._request(input, credentials)
        if isinstance(request, QueryAttempt):
            return request
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return self._retry(FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if status == 408 or status >= 500:
                return self._retry(FailureKind.AVAILABILITY, delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        result = self._decode(wire, input)
        if isinstance(result, QueryAttempt):
            return result
        if result is None:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        return QueryAttempt.branch_result("found", result, Receipt())
