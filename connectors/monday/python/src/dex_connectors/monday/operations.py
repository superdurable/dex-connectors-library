# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""monday.com Query and Mutation invocations with Go-compatible routes."""

import asyncio
import html
from dataclasses import dataclass, field, replace
from datetime import UTC, datetime, timedelta
from typing import Any

from dex_connectors.sdk import (
    Call,
    CallID,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)

from ._generated.definitions import (
    ADD_UPDATE_DEFINITION,
    CREATE_ITEM_DEFINITION,
    GET_ITEM_DEFINITION,
    LIST_ITEMS_DEFINITION,
    UPDATE_ITEM_COLUMN_VALUES_DEFINITION,
)
from .client import Client, Exchange, failure
from .item import COLUMN_ID, NUMERIC_ID, column_ids, decode_item, encode_column_values, numeric_id
from .models import (
    AddUpdateInput,
    AddUpdateOutput,
    CreateItemInput,
    CreateItemOutput,
    GetItemInput,
    Item,
    ItemFilter,
    ItemFilterRule,
    ItemOrder,
    ListItemsInput,
    ListItemsOutput,
    UpdateItemColumnValuesInput,
    UpdateItemColumnValuesOutput,
)

_ITEM_FIELDS = "id name state created_at updated_at url creator_id board { id } group { id title } column_values(ids: $columnIds) { id type text value }"  # noqa: E501
_CREATED_ITEM_FIELDS = "id name state created_at updated_at url creator_id board { id } group { id title }"
_LIST_DOCUMENT = (
    "query ListBoardItems($boardIds: [ID!], $limit: Int!, $queryParams: ItemsQuery, $columnIds: [String!]) {\n  boards(ids: $boardIds) { id items_page(limit: $limit, query_params: $queryParams) { cursor items { "  # noqa: E501
    + _ITEM_FIELDS
    + " } } }\n}"
)
_NEXT_DOCUMENT = (
    "query ListNextBoardItems($cursor: String!, $limit: Int!, $columnIds: [String!]) {\n  next_items_page(cursor: $cursor, limit: $limit) { cursor items { "  # noqa: E501
    + _ITEM_FIELDS
    + " } }\n}"
)
_GET_DOCUMENT = (
    "query GetItem($itemIds: [ID!], $excludeNonactive: Boolean, $columnIds: [String!]) {\n  items(ids: $itemIds, exclude_nonactive: $excludeNonactive) { "  # noqa: E501
    + _ITEM_FIELDS
    + " }\n}"
)
_CREATE_DOCUMENT = (
    "mutation CreateItem($boardId: ID!, $groupId: String, $itemName: String!, $columnValues: JSON, $createLabelsIfMissing: Boolean) {\n  create_item(board_id: $boardId, group_id: $groupId, item_name: $itemName, column_values: $columnValues, create_labels_if_missing: $createLabelsIfMissing) { "  # noqa: E501
    + _CREATED_ITEM_FIELDS
    + " }\n}"
)
_UPDATE_DOCUMENT = (
    "mutation UpdateItemColumnValues($boardId: ID!, $itemId: ID, $columnValues: JSON!, $createLabelsIfMissing: Boolean, $columnIds: [String!]) {\n  change_multiple_column_values(board_id: $boardId, item_id: $itemId, column_values: $columnValues, create_labels_if_missing: $createLabelsIfMissing) { "  # noqa: E501
    + _ITEM_FIELDS
    + " }\n}"
)
_ADD_DOCUMENT = "mutation AddUpdate($itemId: ID, $body: String!) {\n  create_update(item_id: $itemId, body: $body) { id item_id created_at creator_id }\n}"  # noqa: E501

_SUPPORTED_OPERATORS = {
    "any_of",
    "not_any_of",
    "is_empty",
    "is_not_empty",
    "greater_than",
    "greater_than_or_equals",
    "lower_than",
    "lower_than_or_equal",
    "between",
    "contains_text",
    "not_contains_text",
    "contains_terms",
    "starts_with",
    "ends_with",
    "within_the_next",
    "within_the_last",
}
_SINGLE_TEXT = {"contains_text", "not_contains_text", "contains_terms", "starts_with", "ends_with"}


def _query_failure(exchange: Exchange, output: Any, receipt: Receipt) -> QueryAttempt[Any] | None:
    if exchange.outcome == "succeeded":
        return None
    assert exchange.problem is not None
    if exchange.outcome == "retry":
        return QueryAttempt.retry(exchange.problem, exchange.retry_after)
    branch = {"notFound": "notFound", "invalid": "invalidResponse", "defect": "defect"}.get(
        exchange.outcome, "providerRejected"
    )
    return QueryAttempt.branch_result(branch, output, receipt, exchange.problem)


def _mutation_failure(
    exchange: Exchange,
    output: Any,
    receipt: Receipt,
    *,
    invalid_branch: bool = False,
) -> MutationAttempt[Any] | None:
    if exchange.outcome == "succeeded":
        return None
    assert exchange.problem is not None
    if exchange.outcome == "retry":
        return MutationAttempt.retry(exchange.problem, exchange.retry_after)
    if exchange.outcome == "uncertain" or exchange.outcome == "invalid" and not invalid_branch:
        return MutationAttempt.uncertain(output, exchange.problem, receipt)
    branch = {"notFound": "notFound", "invalid": "invalidResponse", "defect": "defect"}.get(
        exchange.outcome, "providerRejected"
    )
    return MutationAttempt.branch_result(branch, output, receipt, exchange.problem)


@dataclass(frozen=True, slots=True)
class _Checkpoint:
    call_id: CallID = field(metadata={"dex_json_name": "callId"})
    attempt: int
    can_resend: bool = field(
        default=False, metadata={"dex_json_name": "canResend", "dex_json_omit_default": True}
    )


async def _checkpointed_exchange(
    client: Client,
    call: Call,
    operation: str,
    document: str,
    variables: dict[str, Any],
) -> Exchange:
    context = call.context
    if context is None:
        return Exchange(
            "defect", problem=failure(operation, FailureKind.LOCAL_DEFECT, "mutation checkpoint is invalid")
        )
    is_invalid_checkpoint = False
    try:
        previous = (
            context.get_last_heartbeat_value(_Checkpoint) if context.has_last_heartbeat_value() else None
        )
    except (ValueError, TypeError):
        previous = None
        is_invalid_checkpoint = True
    if is_invalid_checkpoint or previous is not None and previous.call_id != call.id:
        kind = FailureKind.TRANSPORT if context.attempt > 1 else FailureKind.LOCAL_DEFECT
        return Exchange(
            "uncertain" if context.attempt > 1 else "defect",
            problem=failure(operation, kind, "mutation checkpoint is invalid"),
        )
    if not (
        previous is None
        and context.attempt == 1
        or isinstance(previous, _Checkpoint)
        and previous.can_resend
        and previous.attempt == context.attempt - 1
    ):
        return Exchange(
            "uncertain",
            problem=failure(
                operation, FailureKind.TRANSPORT, "an earlier attempt may have applied the mutation"
            ),
        )
    credentials, resolution = await client.resolve(call, operation)
    if resolution is not None:
        if resolution.outcome == "retry":
            try:
                await context.heartbeat(_Checkpoint(call.id, context.attempt, True))
            except Exception:
                pass  # The credential retry remains safe even if this best-effort checkpoint fails.
        return resolution
    assert credentials is not None
    if context.deadline is not None and context.deadline - datetime.now(UTC) < timedelta(seconds=6):
        try:
            await context.heartbeat(_Checkpoint(call.id, context.attempt, True))
        except Exception:
            pass  # No provider dispatch occurred, so the retry remains safe.
        return Exchange(
            "retry",
            problem=failure(
                operation, FailureKind.AVAILABILITY, "not enough time remains to send the mutation"
            ),
        )
    checkpoint = _Checkpoint(call.id, context.attempt)
    try:
        await context.heartbeat(checkpoint)
    except Exception:
        try:
            await context.heartbeat(_Checkpoint(call.id, context.attempt, True))
        except Exception:
            pass  # The failed pre-send checkpoint still proves no provider request was sent.
        return Exchange(
            "retry",
            problem=failure(operation, FailureKind.AVAILABILITY, "dispatch checkpoint could not be recorded"),
        )
    stop_repetition = asyncio.Event()

    async def repeat_checkpoint() -> None:
        while True:
            try:
                await asyncio.wait_for(stop_repetition.wait(), 5)
                return
            except TimeoutError:
                try:
                    await context.heartbeat(checkpoint)
                except Exception:
                    pass  # The first persisted claim still fences dispatch.

    repetition = asyncio.create_task(repeat_checkpoint())
    try:
        result = await client._send(call, operation, document, variables, credentials, True)
    finally:
        stop_repetition.set()
        await repetition
    if result.status == 401 and credentials.auth_method_id == "monday-oauth":
        refresh = getattr(client.credentials, "resolve_after_rejection", None)
        if refresh is not None:
            try:
                replacement = await refresh(call)
                token = replacement.access_token
                if token is not None and token.reveal():
                    result = await client._send(call, operation, document, variables, replacement, True)
            except (ValueError, LookupError, OSError):
                pass  # The original 401 remains the provider's terminal rejection.
    if result.outcome == "retry":
        if 200 <= result.status < 300 and (not result.confirmed_not_processed or result.data is not None):
            result = replace(result, outcome="uncertain")
        elif result.data is None:
            try:
                await context.heartbeat(_Checkpoint(call.id, context.attempt, True))
            except Exception:
                pass  # A failed positive checkpoint cannot make the current retry unsafe.
    return result


def _filter_query(filter_value: ItemFilter | None, order: ItemOrder | None) -> dict[str, Any] | None:
    if isinstance(order, dict):
        order = ItemOrder(order.get("columnId", ""), order.get("descending", False))
    rules = filter_value.rules if filter_value else ()
    combination = filter_value.combination if filter_value else None
    if not rules and not combination and order is None:
        return None
    if len(rules) > 10:
        raise ValueError("filter accepts at most 10 rules")
    query: dict[str, Any] = {}
    if combination:
        if combination not in ("and", "or"):
            raise ValueError("filter combination must be and or or")
        if not rules:
            raise ValueError("filter combination needs at least one rule")
        query["operator"] = combination
    if rules:
        query["rules"] = [_filter_rule(rule) for rule in rules]
    if order is not None:
        if not COLUMN_ID.fullmatch(order.column_id):
            raise ValueError(
                "order columnId must be a monday.com column ID or __creation_log__ or __last_updated__"
            )
        query["order_by"] = [
            {"column_id": order.column_id, "direction": "desc" if order.descending else "asc"}
        ]
    return query


def _filter_rule(rule: ItemFilterRule | dict[str, Any]) -> dict[str, Any]:
    if isinstance(rule, dict):
        rule = ItemFilterRule(
            rule.get("columnId", ""),
            rule.get("operator"),
            tuple(rule.get("values", ())),
            tuple(rule.get("numbers", ())),
            rule.get("compareAttribute", ""),
        )
    if not COLUMN_ID.fullmatch(rule.column_id):
        raise ValueError("columnId must be a monday.com column ID such as status, date4, or name")
    operator = rule.operator or "any_of"
    if operator not in _SUPPORTED_OPERATORS:
        raise ValueError(
            "operator must be a monday.com items_page rule operator such as any_of or contains_text"
        )
    if rule.values and rule.numbers:
        raise ValueError("set at most one of values and numbers")
    if any(not value.strip() or len(value) > 1000 for value in rule.values):
        raise ValueError("compare values cannot be blank or exceed 1000 characters")
    count = len(rule.values) + len(rule.numbers)
    if operator in ("is_empty", "is_not_empty"):
        if count:
            raise ValueError(f"{operator} takes no compare value")
        compare: object = []
    elif operator in _SINGLE_TEXT:
        if len(rule.values) != 1:
            raise ValueError(f"{operator} takes exactly one string value")
        compare = rule.values[0]
    elif operator == "between" and count != 2:
        raise ValueError("between takes exactly two compare values")
    elif not 1 <= count <= 50:
        raise ValueError(f"{operator} takes 1 to 50 compare values")
    else:
        compare = list(rule.numbers or rule.values)
    result = {"column_id": rule.column_id, "operator": operator, "compare_value": compare}
    if rule.compare_attribute:
        if not COLUMN_ID.fullmatch(rule.compare_attribute):
            raise ValueError("compareAttribute must be letters, digits, underscores, or hyphens")
        result["compare_attribute"] = rule.compare_attribute
    return result


class ListItemsOperation:
    """Read one bounded board item page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest operation identity."""
        return LIST_ITEMS_DEFINITION

    async def invoke(self, call: Call, input: ListItemsInput) -> QueryAttempt[ListItemsOutput]:
        """Validate the page request, then classify one GraphQL answer."""
        output = ListItemsOutput(input.board_id)
        try:
            board_id = numeric_id(input.board_id, "boardId")
            output = replace(output, board_id=board_id)
            limit = input.limit or 25
            if not 1 <= limit <= 100:
                raise ValueError("limit must be from 1 to 100")
            requested_columns = column_ids(input.column_ids)
            if input.cursor:
                filter_value = input.filter
                if (
                    filter_value
                    and (filter_value.rules or filter_value.combination)
                    or input.order is not None
                ):
                    raise ValueError(
                        "a cursor continues its first page's filter and order, so set neither with it"
                    )
                if len(input.cursor.encode()) > 4096 or not all(
                    33 <= ord(char) <= 126 for char in input.cursor
                ):
                    raise ValueError("cursor must be the nextCursor value of a previous page")
                document = _NEXT_DOCUMENT
                variables = {"cursor": input.cursor, "limit": limit, "columnIds": requested_columns}
            else:
                document = _LIST_DOCUMENT
                variables = {"boardIds": [board_id], "limit": limit, "columnIds": requested_columns}
                filter_value = input.filter
                if isinstance(filter_value, dict):
                    filter_value = ItemFilter(**filter_value)
                query = _filter_query(filter_value, input.order)
                if query is not None:
                    variables["queryParams"] = query
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", output, Receipt(), failure("listItems", FailureKind.VALIDATION, str(error))
            )
        exchange = await self.client.exchange(call, "listItems", document, variables)
        receipt = self.client.receipt(call, exchange, output.board_id)
        failed = _query_failure(exchange, output, receipt)
        if failed is not None:
            return failed
        data = exchange.data
        try:
            if not isinstance(data, dict):
                raise ValueError("boards is not a list")
            if input.cursor:
                page = data.get("next_items_page")
                if not isinstance(page, dict):
                    raise ValueError("next_items_page is missing")
            else:
                boards = data.get("boards")
                if not isinstance(boards, list):
                    raise ValueError("boards is not a list")
                if not boards or boards[0] is None:
                    return QueryAttempt.branch_result(
                        "notFound",
                        output,
                        receipt,
                        failure(
                            "listItems",
                            FailureKind.NOT_FOUND,
                            "monday.com found no board with this ID that the connection can see",
                        ),
                    )
                if (
                    len(boards) != 1
                    or not isinstance(boards[0], dict)
                    or not isinstance(boards[0].get("items_page"), dict)
                ):
                    raise ValueError("expected one board with an items_page")
                page = boards[0]["items_page"]
            cursor = page.get("cursor") or ""
            if (
                not isinstance(cursor, str)
                or cursor
                and (len(cursor.encode()) > 4096 or not all(33 <= ord(char) <= 126 for char in cursor))
            ):
                raise ValueError("cursor is not a printable token")
            items = page.get("items") or []
            if not isinstance(items, list) or len(items) > 100:
                raise ValueError("page holds more than 100 items")
            decoded = tuple(decode_item(item) for item in items)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                output,
                receipt,
                failure(
                    "listItems",
                    FailureKind.PROTOCOL,
                    "monday.com returned an invalid page of items: " + str(error),
                ),
            )
        return QueryAttempt.branch_result(
            "listed", replace(output, items=decoded, next_cursor=cursor), receipt
        )


class GetItemOperation:
    """Read one active or inactive monday.com item."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest operation identity."""
        return GET_ITEM_DEFINITION

    async def invoke(self, call: Call, input: GetItemInput) -> QueryAttempt[Item]:
        """Return found, notFound, rejected, invalidResponse, or Retry."""
        try:
            item_id = numeric_id(input.item_id, "itemId")
            requested_columns = column_ids(input.column_ids)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", Item(), Receipt(), failure("getItem", FailureKind.VALIDATION, str(error))
            )
        exchange = await self.client.exchange(
            call,
            "getItem",
            _GET_DOCUMENT,
            {
                "itemIds": [item_id],
                "excludeNonactive": not input.includes_inactive,
                "columnIds": requested_columns,
            },
        )
        receipt = self.client.receipt(call, exchange, item_id)
        failed = _query_failure(exchange, Item(), receipt)
        if failed is not None:
            return failed
        items = exchange.data.get("items") if isinstance(exchange.data, dict) else None
        if not isinstance(items, list) or len(items) > 1:
            return QueryAttempt.branch_result(
                "invalidResponse",
                Item(),
                receipt,
                failure("getItem", FailureKind.PROTOCOL, "monday.com returned an invalid item list"),
            )
        if not items or items[0] is None:
            return QueryAttempt.branch_result(
                "notFound",
                Item(),
                receipt,
                failure(
                    "getItem",
                    FailureKind.NOT_FOUND,
                    "monday.com found no item with this ID that the connection can see",
                ),
            )
        try:
            item = decode_item(items[0])
            if item.id != item_id:
                raise ValueError("wrong item")
        except ValueError:
            return QueryAttempt.branch_result(
                "invalidResponse",
                Item(),
                receipt,
                failure("getItem", FailureKind.PROTOCOL, "monday.com returned an invalid item"),
            )
        return QueryAttempt.branch_result("found", item, receipt)


class CreateItemOperation:
    """Create one item under a stable key and a durable dispatch checkpoint."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest operation identity."""
        return CREATE_ITEM_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateItemInput) -> IdempotencyKey:
        """Use the Call ID as monday.com's idempotency key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: CreateItemInput) -> MutationAttempt[CreateItemOutput]:
        """Create once, resending only after confirmed nonprocessing."""
        output = CreateItemOutput(input.board_id.strip(), input.group_id.strip(), input.item_name)
        try:
            board_id = numeric_id(input.board_id, "boardId")
            if output.group_id and not COLUMN_ID.fullmatch(output.group_id):
                raise ValueError("groupId must be a monday.com group ID such as topics")
            name = input.item_name.strip()
            if not name or len(name) > 255 or any(ord(char) < 32 or ord(char) == 127 for char in name):
                raise ValueError("itemName must be one line of 1 to 255 characters")
            output = replace(output, item_name=name)
            variables: dict[str, Any] = {
                "boardId": board_id,
                "itemName": name,
                "createLabelsIfMissing": input.creates_labels_if_missing,
            }
            if output.group_id:
                variables["groupId"] = output.group_id
            if input.column_values:
                variables["columnValues"] = encode_column_values(input.column_values)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", output, Receipt(), failure("createItem", FailureKind.VALIDATION, str(error))
            )
        exchange = await _checkpointed_exchange(self.client, call, "createItem", _CREATE_DOCUMENT, variables)
        receipt = self.client.receipt(call, exchange)
        item_data = exchange.data.get("create_item") if isinstance(exchange.data, dict) else None
        try:
            item = decode_item(item_data) if item_data is not None else None
        except ValueError:
            item = None
        if item is not None:
            output = replace(output, item_id=item.id, item=item, replayed=exchange.replayed)
            return MutationAttempt.branch_result(
                "created", output, replace(receipt, provider_object_id=item.id)
            )
        failed = _mutation_failure(exchange, output, receipt)
        if failed is not None:
            return failed
        return MutationAttempt.uncertain(
            output,
            failure(
                "createItem",
                FailureKind.PROTOCOL,
                "monday.com accepted the item but returned an unusable item",
            ),
            receipt,
        )


class UpdateItemColumnValuesOperation:
    """Replace one item's typed column values with absolute values."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest operation identity."""
        return UPDATE_ITEM_COLUMN_VALUES_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateItemColumnValuesInput) -> IdempotencyKey:
        """Use the Call ID as the stable provider key."""
        return IdempotencyKey(call_id)

    async def invoke(
        self, call: Call, input: UpdateItemColumnValuesInput
    ) -> MutationAttempt[UpdateItemColumnValuesOutput]:
        """Set absolute values, which remain safe to retry after an unknown response."""
        output = UpdateItemColumnValuesOutput(input.board_id, input.item_id)
        try:
            board_id = numeric_id(input.board_id, "boardId")
            item_id = numeric_id(input.item_id, "itemId")
            output = replace(output, board_id=board_id, item_id=item_id)
            if not input.column_values:
                raise ValueError("columnValues needs at least one column")
            encoded = encode_column_values(input.column_values)
            variables = {
                "boardId": board_id,
                "itemId": item_id,
                "columnValues": encoded,
                "createLabelsIfMissing": input.creates_labels_if_missing,
                "columnIds": sorted(input.column_values),
            }
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                output,
                Receipt(),
                failure("updateItemColumnValues", FailureKind.VALIDATION, str(error)),
            )
        exchange = await self.client.exchange(
            call, "updateItemColumnValues", _UPDATE_DOCUMENT, variables, mutation=True
        )
        receipt = self.client.receipt(call, exchange, item_id)
        item_data = (
            exchange.data.get("change_multiple_column_values") if isinstance(exchange.data, dict) else None
        )
        try:
            item = decode_item(item_data) if item_data is not None else None
        except ValueError:
            item = None
        if item is not None and item.id == item_id:
            return MutationAttempt.branch_result(
                "updated", replace(output, item=item, replayed=exchange.replayed), receipt
            )
        failed = _mutation_failure(exchange, output, receipt, invalid_branch=True)
        if failed is not None:
            return failed
        return MutationAttempt.branch_result(
            "invalidResponse",
            output,
            receipt,
            failure(
                "updateItemColumnValues",
                FailureKind.PROTOCOL,
                "monday.com returned an unusable item, so the values may have been set",
            ),
        )


class AddUpdateOperation:
    """Post an escaped plain-text update to one item under a dispatch checkpoint."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest operation identity."""
        return ADD_UPDATE_DEFINITION

    def idempotency_key(self, call_id: CallID, input: AddUpdateInput) -> IdempotencyKey:
        """Use the Call ID as monday.com's idempotency key."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: AddUpdateInput) -> MutationAttempt[AddUpdateOutput]:
        """Add one update, treating an accepted but unusable answer as uncertain."""
        output = AddUpdateOutput(input.item_id.strip())
        try:
            item_id = numeric_id(input.item_id, "itemId")
            body = input.body
            if not body.strip():
                raise ValueError("body is required")
            if len(body) > 10000:
                raise ValueError("body is at most 10000 characters")
            if any(ord(char) < 32 and char not in "\r\n\t" or ord(char) == 127 for char in body):
                raise ValueError("body cannot contain control characters other than line breaks and tabs")
            escaped = "<br>".join(
                html.escape(line, quote=True).replace("&#x27;", "&#39;").replace("&quot;", "&#34;")
                for line in body.strip().replace("\r\n", "\n").split("\n")
            )
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", output, Receipt(), failure("addUpdate", FailureKind.VALIDATION, str(error))
            )
        exchange = await _checkpointed_exchange(
            self.client, call, "addUpdate", _ADD_DOCUMENT, {"itemId": item_id, "body": escaped}
        )
        receipt = self.client.receipt(call, exchange)
        resource = exchange.data.get("create_update") if isinstance(exchange.data, dict) else None
        if (
            isinstance(resource, dict)
            and isinstance(resource.get("id"), str)
            and NUMERIC_ID.fullmatch(resource["id"])
            and resource.get("item_id") in (None, "", item_id)
        ):
            creator = resource.get("creator_id")
            creator_id = creator if isinstance(creator, str) and NUMERIC_ID.fullmatch(creator) else ""
            created_at = datetime.min.replace(tzinfo=UTC)
            try:
                created_at = datetime.fromisoformat(
                    str(resource.get("created_at", "")).replace("Z", "+00:00")
                ).astimezone(UTC)
            except ValueError:
                pass
            output = replace(
                output,
                update_id=resource["id"],
                creator_id=creator_id,
                created_at=created_at,
                replayed=exchange.replayed,
            )
            return MutationAttempt.branch_result(
                "added", output, replace(receipt, provider_object_id=resource["id"])
            )
        failed = _mutation_failure(exchange, output, receipt)
        if failed is not None:
            return failed
        return MutationAttempt.uncertain(
            output,
            failure(
                "addUpdate",
                FailureKind.PROTOCOL,
                "monday.com accepted the update but returned an unusable update",
            ),
            receipt,
        )
