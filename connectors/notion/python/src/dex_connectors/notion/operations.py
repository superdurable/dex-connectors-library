# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Invoke Notion search, query, read, create, and absolute update operations."""

import json
from dataclasses import replace
from datetime import datetime, timedelta
from typing import TypeVar, cast

from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.definitions import (
    CREATE_PAGE_DEFINITION as CREATE_PAGE_DEFINITION,
)
from ._generated.definitions import (
    GET_PAGE_DEFINITION as GET_PAGE_DEFINITION,
)
from ._generated.definitions import (
    QUERY_DATABASE_DEFINITION as QUERY_DATABASE_DEFINITION,
)
from ._generated.definitions import (
    SEARCH_DEFINITION as SEARCH_DEFINITION,
)
from ._generated.definitions import (
    UPDATE_PAGE_PROPERTIES_DEFINITION as UPDATE_PAGE_PROPERTIES_DEFINITION,
)
from .client import (
    Classification,
    Client,
    ProviderResponse,
    classify_read,
    classify_write,
    escaped_path,
    failure,
)
from .encoding import (
    cursor,
    encode_filter,
    encode_paragraphs,
    encode_properties,
    encode_sorts,
    select_data_source,
)
from .models import (
    CreatePageInput,
    CreatePageOutput,
    GetPageInput,
    GetPageOutput,
    Page,
    PageContent,
    Parent,
    QueryDatabaseInput,
    QueryDatabaseOutput,
    SearchInput,
    SearchOutput,
    UpdatePagePropertiesInput,
    UpdatePagePropertiesOutput,
)
from .provider_models import decode_page, decode_search_match, parse_id


def _json(body: bytes) -> dict[str, object]:
    try:
        value = json.loads(body)
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ValueError("Notion returned invalid JSON") from error
    if not isinstance(value, dict):
        raise ValueError("Notion returned an invalid object")
    return value


def _empty_page() -> Page:
    return Page(parent=Parent(type=""), properties={})


T = TypeVar("T")


def _read_attempt(
    operation: str,
    classification: Classification,
    value: T,
    receipt: Receipt,
) -> QueryAttempt[T] | None:
    if classification.outcome == "success":
        return None
    if classification.outcome == "retry":
        return QueryAttempt.retry(cast(Failure, classification.failure), classification.retry_after)
    branch = {
        "notFound": "notFound",
        "rejected": "providerRejected",
        "invalid": "invalidResponse",
        "defect": "defect",
    }[classification.outcome]
    return QueryAttempt.branch_result(branch, value, receipt, classification.failure)


def _mutation_read_attempt(
    classification: Classification,
    value: CreatePageOutput,
    receipt: Receipt,
) -> MutationAttempt[CreatePageOutput] | None:
    if classification.outcome == "success":
        return None
    if classification.outcome == "retry":
        return MutationAttempt.retry(cast(Failure, classification.failure), classification.retry_after)
    branch = {
        "notFound": "notFound",
        "rejected": "providerRejected",
        "invalid": "invalidResponse",
        "defect": "defect",
    }[classification.outcome]
    return MutationAttempt.branch_result(branch, value, receipt, classification.failure)


async def _resolve_data_source(
    client: Client,
    call: Call,
    token: str,
    operation: str,
    data_source_id: str,
    database_id: str,
) -> tuple[str, Classification, ProviderResponse]:
    if data_source_id:
        return data_source_id, Classification("success"), ProviderResponse()
    response = await client.request(token, "GET", "/databases/" + escaped_path(database_id))
    classification = classify_read(operation, response)
    if classification.outcome != "success":
        return "", classification, response
    try:
        database = _json(response.body)
        data_sources = database.get("data_sources")
        if (
            database.get("object") != "database"
            or not isinstance(data_sources, list)
            or len(data_sources) > 100
        ):
            raise ValueError
        identifiers = [
            parse_id(str(entry.get("id", ""))) for entry in data_sources if isinstance(entry, dict)
        ]
        if len(identifiers) != len(data_sources):
            raise ValueError
    except (TypeError, ValueError):
        return (
            "",
            Classification(
                "invalid", failure(operation, FailureKind.PROTOCOL, "Notion returned an invalid database")
            ),
            response,
        )
    if len(identifiers) != 1:
        return (
            "",
            Classification(
                "defect",
                failure(
                    operation,
                    FailureKind.VALIDATION,
                    "database needs exactly one data source; set dataSourceId",
                ),
            ),
            response,
        )
    return identifiers[0], Classification("success"), response


class SearchOperation:
    """Search one bounded page of titles shared with the connection."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable generated search definition."""
        return SEARCH_DEFINITION

    async def invoke(self, call: Call, input: SearchInput) -> QueryAttempt[SearchOutput]:
        """Send one title search and classify its response."""
        empty = SearchOutput()
        try:
            query = input.query.strip()
            if len(query) > 1000:
                raise ValueError("query is at most 1000 characters")
            if input.object_type not in (None, "", "page", "data_source"):
                raise ValueError("objectType is page, data_source, or blank")
            if input.sort_direction not in (None, "", "ascending", "descending"):
                raise ValueError("sortDirection is ascending, descending, or blank")
            size = input.page_size or 20
            if not 1 <= size <= 100:
                raise ValueError("pageSize must be between 1 and 100")
            start = cursor(input.start_cursor)
            body: dict[str, object] = {"page_size": size}
            if query:
                body["query"] = query
            if input.object_type:
                body["filter"] = {"property": "object", "value": input.object_type}
            if input.sort_direction:
                body["sort"] = {"timestamp": "last_edited_time", "direction": input.sort_direction}
            if start:
                body["start_cursor"] = start
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure("search", FailureKind.VALIDATION, str(error))
            )
        token, credential_failure, _ = await self.client.resolve_credentials(call, "search")
        if credential_failure is not None or token is None:
            return QueryAttempt.branch_result("defect", empty, Receipt(), credential_failure)
        response = await self.client.request(token, "POST", "/search", payload=body)
        receipt = self.client.receipt(call, response)
        classified = classify_read("search", response)
        attempt = _read_attempt("search", classified, empty, receipt)
        if attempt is not None:
            return attempt
        try:
            page = _json(response.body)
            results = page.get("results")
            if not isinstance(results, list) or len(results) > size:
                raise ValueError
            next_cursor = cursor(str(page.get("next_cursor") or ""))
            matches = tuple(
                match
                for resource in results
                if isinstance(resource, dict)
                if (match := decode_search_match(resource)) is not None
            )
            output = SearchOutput(
                matches, next_cursor if bool(page.get("has_more")) else "", bool(page.get("has_more"))
            )
        except (TypeError, ValueError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure("search", FailureKind.PROTOCOL, "Notion returned an invalid search page"),
            )
        if not output.matches and not start:
            return QueryAttempt.branch_result(
                "noMatch",
                output,
                receipt,
                failure("search", FailureKind.NOT_FOUND, "no shared Notion title matches"),
            )
        return QueryAttempt.branch_result("searched", output, receipt)


class QueryDatabaseOperation:
    """Query one bounded page of a Notion data source."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable generated query definition."""
        return QUERY_DATABASE_DEFINITION

    async def invoke(self, call: Call, input: QueryDatabaseInput) -> QueryAttempt[QueryDatabaseOutput]:
        """Resolve the source, then query its page rows."""
        empty = QueryDatabaseOutput()
        try:
            source_id, database_id = select_data_source(input.data_source_id, input.database_id)
            size = input.page_size or 50
            if not 1 <= size <= 100:
                raise ValueError("pageSize must be between 1 and 100")
            body: dict[str, object] = {"page_size": size, "result_type": "page"}
            if input.filter is not None:
                body["filter"] = encode_filter(input.filter)
            sorts = encode_sorts(input.sorts)
            if sorts:
                body["sorts"] = sorts
            start = cursor(input.start_cursor)
            if start:
                body["start_cursor"] = start
            if len(input.properties) > 100 or any(
                not item.strip() or item != item.strip() or len(item) > 256 for item in input.properties
            ):
                raise ValueError("properties lists at most 100 valid names or IDs")
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure("queryDatabase", FailureKind.VALIDATION, str(error))
            )
        token, credential_failure, _ = await self.client.resolve_credentials(call, "queryDatabase")
        if credential_failure is not None or token is None:
            return QueryAttempt.branch_result("defect", empty, Receipt(), credential_failure)
        source_id, classified, resolution = await _resolve_data_source(
            self.client, call, token, "queryDatabase", source_id, database_id
        )
        lookup_receipt = self.client.receipt(call, resolution, database_id)
        attempt = _read_attempt("queryDatabase", classified, empty, lookup_receipt)
        if attempt is not None:
            return attempt
        query = [("filter_properties[]", item) for item in input.properties]
        response = await self.client.request(
            token, "POST", "/data_sources/" + escaped_path(source_id) + "/query", payload=body, query=query
        )
        receipt = self.client.receipt(call, response, source_id)
        output = QueryDatabaseOutput(data_source_id=source_id)
        classified = classify_read("queryDatabase", response)
        attempt = _read_attempt("queryDatabase", classified, output, receipt)
        if attempt is not None:
            return attempt
        try:
            page = _json(response.body)
            results = page.get("results")
            if not isinstance(results, list) or len(results) > size:
                raise ValueError
            next_cursor = cursor(str(page.get("next_cursor") or ""))
            rows = tuple(decode_page(item) for item in results)
            status = page.get("request_status") or {}
            limit_reached = (
                isinstance(status, dict) and status.get("incomplete_reason") == "query_result_limit_reached"
            )
            output = QueryDatabaseOutput(
                source_id,
                rows,
                next_cursor if bool(page.get("has_more")) else "",
                bool(page.get("has_more")),
                limit_reached,
            )
        except (TypeError, ValueError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                failure("queryDatabase", FailureKind.PROTOCOL, "Notion returned an invalid query page"),
            )
        return QueryAttempt.branch_result("queried", output, receipt)


class GetPageOperation:
    """Read one page and a bounded rendering of its top-level blocks."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable generated page read definition."""
        return GET_PAGE_DEFINITION

    async def invoke(self, call: Call, input: GetPageInput) -> QueryAttempt[GetPageOutput]:
        """Read properties and optionally page content."""
        empty = GetPageOutput(_empty_page())
        try:
            page_id = parse_id(input.page_id)
            max_blocks = input.max_blocks or 100
            max_characters = input.max_text_characters or 20000
            if not 1 <= max_blocks <= 500 or not 1 <= max_characters <= 100000:
                raise ValueError("page content limits are invalid")
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure("getPage", FailureKind.VALIDATION, str(error))
            )
        token, credential_failure, _ = await self.client.resolve_credentials(call, "getPage")
        if credential_failure is not None or token is None:
            return QueryAttempt.branch_result("defect", empty, Receipt(), credential_failure)
        response = await self.client.request(token, "GET", "/pages/" + escaped_path(page_id))
        receipt = self.client.receipt(call, response, page_id)
        classified = classify_read("getPage", response)
        attempt = _read_attempt("getPage", classified, empty, receipt)
        if attempt is not None:
            return attempt
        try:
            page = decode_page(_json(response.body))
        except ValueError:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure("getPage", FailureKind.PROTOCOL, "Notion returned an unusable page"),
            )
        if input.skip_content:
            return QueryAttempt.branch_result("found", GetPageOutput(page), receipt)
        lines: list[str] = []
        block_count = nested_count = skipped_count = character_count = list_number = 0
        has_more = truncated = False
        start = ""
        while block_count < max_blocks:
            size = min(100, max_blocks - block_count)
            query = [("page_size", str(size))]
            if start:
                query.append(("start_cursor", start))
            blocks_response = await self.client.request(
                token, "GET", "/blocks/" + escaped_path(page.id) + "/children", query=query
            )
            classified = classify_read("getPage", blocks_response)
            attempt = _read_attempt("getPage", classified, empty, receipt)
            if attempt is not None:
                return attempt
            try:
                blocks = _json(blocks_response.body)
                results = blocks.get("results")
                if not isinstance(results, list) or len(results) > size:
                    raise ValueError
                for block in results:
                    if not isinstance(block, dict):
                        raise ValueError
                    block_count += 1
                    nested_count += bool(block.get("has_children"))
                    block_type = str(block.get("type", ""))
                    if block_type != "numbered_list_item":
                        list_number = 0
                    if block_type == "numbered_list_item":
                        list_number += 1
                    line = _render_block(block, list_number)
                    if line is None:
                        skipped_count += 1
                        continue
                    if truncated:
                        continue
                    separator = 1 if lines else 0
                    remaining = max_characters - character_count - separator
                    if len(line) > remaining:
                        truncated = True
                        if remaining <= 0:
                            continue
                        line = line[:remaining]
                    lines.append(line)
                    character_count += len(line) + separator
                next_cursor = str(blocks.get("next_cursor") or "")
                if not bool(blocks.get("has_more")) or not next_cursor:
                    break
                if block_count >= max_blocks:
                    has_more = True
                    break
                start = cursor(next_cursor)
            except (TypeError, ValueError):
                return QueryAttempt.branch_result(
                    "invalidResponse",
                    empty,
                    receipt,
                    failure("getPage", FailureKind.PROTOCOL, "Notion returned an invalid block list"),
                )
        content = PageContent("\n".join(lines), block_count, has_more, truncated, nested_count, skipped_count)
        return QueryAttempt.branch_result("found", GetPageOutput(page, content), receipt)


def _render_block(block: dict[str, object], list_number: int) -> str | None:
    block_type = str(block.get("type", ""))
    content = block.get(block_type)
    if not isinstance(content, dict):
        return None
    rich_text = content.get("rich_text") or []
    text = (
        "".join(str(item.get("plain_text", "")) for item in rich_text if isinstance(item, dict))
        if isinstance(rich_text, list)
        else ""
    )
    if block_type in (
        "paragraph",
        "heading_1",
        "heading_2",
        "heading_3",
        "heading_4",
        "quote",
        "callout",
        "toggle",
        "code",
        "template",
    ):
        return text
    if block_type == "bulleted_list_item":
        return "- " + text
    if block_type == "numbered_list_item":
        return f"{list_number}. {text}"
    if block_type == "to_do":
        return ("[x] " if content.get("checked") else "[ ] ") + text
    if block_type in ("child_page", "child_database"):
        return str(content.get("title")) if content.get("title") else None
    if block_type in ("equation", "bookmark", "embed", "link_preview"):
        value = content.get("expression" if block_type == "equation" else "url")
        return str(value) if value else None
    if block_type == "table_row":
        cells = content.get("cells")
        if not isinstance(cells, list) or not cells:
            return None
        return " | ".join(
            "".join(str(item.get("plain_text", "")) for item in cell if isinstance(item, dict))
            for cell in cells
            if isinstance(cell, list)
        )
    if block_type in ("image", "video", "file", "pdf", "audio"):
        caption = content.get("caption")
        if not isinstance(caption, list):
            return None
        text = "".join(str(item.get("plain_text", "")) for item in caption if isinstance(item, dict))
        return text if text else None
    return None


class UpdatePagePropertiesOperation:
    """Apply absolute property values to one page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the stable generated update definition."""
        return UPDATE_PAGE_PROPERTIES_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdatePagePropertiesInput) -> IdempotencyKey:
        """Record the stable call ID; Notion does not accept provider keys."""
        return IdempotencyKey(call_id)

    async def invoke(
        self, call: Call, input: UpdatePagePropertiesInput
    ) -> MutationAttempt[UpdatePagePropertiesOutput]:
        """Send one retry-safe absolute PATCH."""
        empty = UpdatePagePropertiesOutput(_empty_page())
        try:
            page_id = parse_id(input.page_id)
            if not input.properties:
                raise ValueError("properties needs at least one value")
            body = {"properties": encode_properties(input.properties)}
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure("updatePageProperties", FailureKind.VALIDATION, str(error)),
            )
        token, credential_failure, _ = await self.client.resolve_credentials(call, "updatePageProperties")
        if credential_failure is not None or token is None:
            return MutationAttempt.branch_result("defect", empty, Receipt(), credential_failure)
        response = await self.client.request(
            token, "PATCH", "/pages/" + escaped_path(page_id), payload=body, timeout=60
        )
        receipt = self.client.receipt(call, response, page_id)
        classified = classify_write("updatePageProperties", response, True)
        if classified.outcome == "retry":
            return MutationAttempt.retry(cast(Failure, classified.failure), classified.retry_after)
        if classified.outcome != "accepted":
            branch = {
                "notFound": "notFound",
                "rejected": "providerRejected",
                "invalid": "invalidResponse",
                "defect": "defect",
            }[classified.outcome]
            return MutationAttempt.branch_result(branch, empty, receipt, classified.failure)
        try:
            page = decode_page(_json(response.body))
            if page.id != page_id:
                raise ValueError
        except ValueError:
            return MutationAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(
                    "updatePageProperties",
                    FailureKind.PROTOCOL,
                    "Notion applied the update but returned an unusable page",
                ),
            )
        return MutationAttempt.branch_result("updated", UpdatePagePropertiesOutput(page), receipt)


class CreatePageOperation:
    """Fence one nonrepeatable Notion page create and read back known IDs."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the stable generated create definition."""
        return CREATE_PAGE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreatePageInput) -> IdempotencyKey:
        """Record the stable call ID without sending it to Notion."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: CreatePageInput) -> MutationAttempt[CreatePageOutput]:
        """Create one page or report an unconfirmable outcome."""
        empty = CreatePageOutput()
        try:
            fence = Fence(call, timedelta(seconds=120))
        except ValueError:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure("createPage", FailureKind.LOCAL_DEFECT, "Notion dispatch could not be initialized"),
            )
        decision = await fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.uncertain(
                empty,
                failure("createPage", FailureKind.PROTOCOL, "Notion dispatch checkpoint cannot be trusted"),
                Receipt(),
            )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            marker = decision.marker
            if marker is None or marker.call_id != call.id or not marker.provider_id:
                return MutationAttempt.uncertain(
                    empty,
                    failure("createPage", FailureKind.PROTOCOL, "Notion page may already exist"),
                    Receipt(),
                )
            token, credential_failure, _ = await self.client.resolve_credentials(call, "createPage")
            if credential_failure is not None or token is None:
                return MutationAttempt.retry(cast(Failure, credential_failure))
            return await self._confirm_page(call, token, empty, marker.provider_id, Receipt())
        try:
            source_id, database_id = select_data_source(input.data_source_id, input.database_id)
            properties = encode_properties(input.properties)
            children = encode_paragraphs(input.body_text)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), failure("createPage", FailureKind.VALIDATION, str(error))
            )
        token, credential_failure, transient = await self.client.resolve_credentials(call, "createPage")
        if credential_failure is not None or token is None:
            try:
                await fence.record_not_sent()
            except (CheckpointError, DispatchNotAllowedError):
                return MutationAttempt.retry(
                    failure(
                        "createPage", FailureKind.AVAILABILITY, "Notion credential checkpoint is unavailable"
                    )
                )
            if transient:
                return MutationAttempt.retry(cast(Failure, credential_failure))
            return MutationAttempt.branch_result("defect", empty, Receipt(), credential_failure)
        source_id, classified, resolution = await _resolve_data_source(
            self.client, call, token, "createPage", source_id, database_id
        )
        lookup_receipt = self.client.receipt(call, resolution, database_id)
        if classified.outcome != "success":
            if classified.outcome == "retry":
                try:
                    await fence.record_not_sent()
                except (CheckpointError, DispatchNotAllowedError):
                    return MutationAttempt.retry(
                        failure(
                            "createPage", FailureKind.AVAILABILITY, "Notion lookup checkpoint is unavailable"
                        )
                    )
            attempt = _mutation_read_attempt(classified, empty, lookup_receipt)
            assert attempt is not None
            return attempt
        output = CreatePageOutput(data_source_id=source_id)
        body: dict[str, object] = {
            "parent": {"type": "data_source_id", "data_source_id": source_id},
            "properties": properties,
        }
        if children:
            body["children"] = children
        try:

            async def send(_deadline: datetime) -> ProviderResponse:
                return await self.client.request(token, "POST", "/pages", payload=body, timeout=60)

            response = await fence.dispatch(send)
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                failure("createPage", FailureKind.TRANSPORT, "Notion dispatch checkpoint is unavailable")
            )
        except DispatchNotAllowedError:
            return MutationAttempt.uncertain(
                output,
                failure("createPage", FailureKind.PROTOCOL, "Notion dispatch could not be classified"),
                Receipt(),
            )
        receipt = self.client.receipt(call, response)
        classified = classify_write("createPage", response, False)
        if classified.outcome == "retry":
            try:
                await fence.record_can_resend()
            except (CheckpointError, DispatchNotAllowedError):
                return MutationAttempt.retry(
                    failure("createPage", FailureKind.AVAILABILITY, "Notion retry checkpoint is unavailable"),
                    classified.retry_after,
                )
            return MutationAttempt.retry(cast(Failure, classified.failure), classified.retry_after)
        if classified.outcome == "committed":
            await self._record_provider_id(fence, classified.committed_id)
            return await self._confirm_page(call, token, output, classified.committed_id, receipt, True)
        if classified.outcome == "accepted":
            try:
                page = decode_page(_json(response.body))
            except ValueError:
                try:
                    accepted = _json(response.body)
                    page_id = (
                        parse_id(str(accepted.get("id", ""))) if accepted.get("object") == "page" else ""
                    )
                except ValueError:
                    page_id = ""
                if page_id:
                    await self._record_provider_id(fence, page_id)
                    return await self._confirm_page(call, token, output, page_id, receipt)
                return MutationAttempt.uncertain(
                    output,
                    failure(
                        "createPage",
                        FailureKind.PROTOCOL,
                        "Notion accepted the page but returned an unusable response",
                    ),
                    receipt,
                )
            await self._record_provider_id(fence, page.id)
            return MutationAttempt.branch_result(
                "created",
                replace(output, page_id=page.id, page=page),
                replace(receipt, provider_object_id=page.id),
            )
        if classified.outcome == "uncertain":
            return MutationAttempt.uncertain(output, cast(Failure, classified.failure), receipt)
        branch = {"notFound": "notFound", "rejected": "providerRejected", "defect": "defect"}[
            classified.outcome
        ]
        return MutationAttempt.branch_result(branch, output, receipt, classified.failure)

    async def _record_provider_id(self, fence: Fence, identifier: str) -> None:
        try:
            await fence.record_provider_id(identifier)
        except (CheckpointError, DispatchNotAllowedError):
            # The accepted ID is already in the current response; loss only limits later read-back.
            pass

    async def _confirm_page(
        self,
        call: Call,
        token: str,
        output: CreatePageOutput,
        page_id: str,
        receipt: Receipt,
        confirmed_after_timeout: bool = False,
    ) -> MutationAttempt[CreatePageOutput]:
        output = replace(output, page_id=page_id, confirmed_after_timeout=confirmed_after_timeout)
        receipt = replace(receipt, provider_object_id=page_id)
        response = await self.client.request(token, "GET", "/pages/" + escaped_path(page_id))
        classified = classify_read("createPage", response)
        if classified.outcome == "retry":
            return MutationAttempt.retry(cast(Failure, classified.failure), classified.retry_after)
        if classified.outcome != "success":
            return MutationAttempt.uncertain(
                output,
                failure(
                    "createPage",
                    FailureKind.PROTOCOL,
                    "Notion accepted the page but read-back could not confirm it",
                ),
                receipt,
            )
        try:
            page = decode_page(_json(response.body))
            if page.id != page_id:
                raise ValueError
        except ValueError:
            return MutationAttempt.uncertain(
                output,
                failure(
                    "createPage", FailureKind.PROTOCOL, "Notion read-back did not match the accepted page"
                ),
                receipt,
            )
        if page.parent.type == "data_source_id":
            output = replace(output, data_source_id=page.parent.id)
        return MutationAttempt.branch_result("created", replace(output, page=page), receipt)
