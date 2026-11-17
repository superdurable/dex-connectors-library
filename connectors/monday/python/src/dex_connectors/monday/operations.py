# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""monday.com Query and Mutation invocations with Go-compatible routes."""

import html
from dataclasses import replace
from datetime import UTC, datetime, timedelta
from typing import Any

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
    Decision,
    DecisionKind,
    DispatchNotAllowedError,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated.definitions import (
    ADD_UPDATE_DEFINITION,
    CREATE_ITEM_DEFINITION,
    GET_ITEM_DEFINITION,
    LIST_ITEMS_DEFINITION,
    UPDATE_ITEM_COLUMN_VALUES_DEFINITION,
)
from .client import Client, Credentials, Exchange, classify_write, failure
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


_MINIMUM_DISPATCH_WINDOW = timedelta(seconds=6)
"""The request deadline margin plus the least request window in which a write may start."""
_REFUSAL_BRANCHES = {"notFound": "notFound", "defect": "defect"}


def _accepted_reference_id(data: Any, mutation_field: str) -> str:
    """Return the numeric ID in the mutation field, even when the rest is unusable."""
    value = data.get(mutation_field) if isinstance(data, dict) else None
    identifier = value.get("id") if isinstance(value, dict) else None
    return identifier if isinstance(identifier, str) and NUMERIC_ID.fullmatch(identifier) else ""


class _FencedWrite:
    """One attempt of a createItem or addUpdate behind the shared single-dispatch fence.

    monday.com's idempotency cache can be bypassed, so an unknown outcome resends under
    the same key only while the send cap allows, and selects `uncertain` at the cap.
    """

    def __init__(self, call: Call, operation: str, fence: Fence, decision: Decision) -> None:
        self.call, self.operation, self.fence, self.decision = call, operation, fence, decision
        self.can_resend_unknown_outcome = decision.prior_send_upper_bound + 1 < call.max_sends
        self.has_possible_earlier_send = decision.prior_send_upper_bound > 0

    @classmethod
    async def start(
        cls, call: Call, operation: str, output: Any, receipt: Receipt
    ) -> "_FencedWrite | MutationAttempt[Any]":
        """Decide whether this attempt may send, or return the attempt that forbids it."""
        try:
            fence = Fence(call, timedelta(seconds=30), max_sends=call.max_sends)
        except ValueError:
            cause = failure(
                operation, FailureKind.LOCAL_DEFECT, "monday.com dispatch fence could not be constructed"
            )
            if call.context is not None and call.context.attempt > 1:
                return MutationAttempt.uncertain(output, cause, receipt)
            return MutationAttempt.branch_result("defect", output, Receipt(), cause)
        decision = await fence.decide()
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "monday.com dispatch checkpoint is unreadable, so nothing was sent in this attempt",
                )
            )
        if decision.kind == DecisionKind.READ_BACK_REQUIRED:
            return MutationAttempt.uncertain(
                output,
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "earlier attempts of this Step may have sent the request and the send cap "
                    "is reached, so it is not sent again",
                ),
                receipt,
            )
        if decision.marker is not None and decision.marker.provider_id:
            return MutationAttempt.uncertain(
                output,
                failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "an earlier attempt of this Step was accepted, so the request is not sent again",
                ),
                receipt,
            )
        return cls(call, operation, fence, decision)

    async def pre_send_failure(self, resolution: Exchange, output: Any) -> MutationAttempt[Any]:
        """Map a credential failure that ended the attempt before any send."""
        assert resolution.problem is not None
        # notSent after a possible earlier send would hide it, so only a first-send decision records it.
        if self.decision.kind == DecisionKind.SEND_ALLOWED:
            try:
                await self.fence.record_not_sent()
            except Exception:
                return MutationAttempt.retry(
                    failure(
                        self.operation,
                        FailureKind.AVAILABILITY,
                        "monday.com not-sent checkpoint could not be recorded",
                    )
                )
        if resolution.outcome == "retry":
            return MutationAttempt.retry(resolution.problem, resolution.retry_after)
        if self.has_possible_earlier_send:
            return MutationAttempt.uncertain(output, resolution.problem, Receipt())
        branch = "providerRejected" if resolution.outcome == "rejected" else "defect"
        return MutationAttempt.branch_result(branch, output, Receipt(), resolution.problem)

    async def dispatch(
        self,
        client: Client,
        credentials: Credentials,
        document: str,
        variables: dict[str, Any],
        output: Any,
    ) -> Exchange | MutationAttempt[Any]:
        """Send once behind the fence, or return the attempt that forbids sending."""
        call, operation = self.call, self.operation
        deadline = getattr(call.context, "deadline", None)
        if deadline is not None and deadline - datetime.now(UTC) < _MINIMUM_DISPATCH_WINDOW:
            # notSent after a possible earlier send would hide it, so only a first-send decision records it.
            if self.decision.kind == DecisionKind.SEND_ALLOWED:
                try:
                    await self.fence.record_not_sent()
                except Exception:
                    return MutationAttempt.retry(
                        failure(
                            operation,
                            FailureKind.AVAILABILITY,
                            "monday.com not-sent checkpoint could not be recorded",
                        )
                    )
            return MutationAttempt.retry(
                failure(operation, FailureKind.AVAILABILITY, "not enough time remains to send the mutation")
            )
        try:
            return await self.fence.dispatch(
                lambda write_deadline: client.send_with_rejection_refresh(
                    call, operation, document, variables, credentials, True, write_deadline
                )
            )
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "dispatch checkpoint could not be recorded, so nothing was sent",
                )
            )
        except DispatchNotAllowedError:
            cause = failure(operation, FailureKind.LOCAL_DEFECT, "monday.com dispatch was not allowed")
            if self.has_possible_earlier_send:
                return MutationAttempt.uncertain(output, cause, Receipt())
            return MutationAttempt.branch_result("defect", output, Receipt(), cause)
        except TimeoutError:
            return Exchange(
                "uncertain",
                problem=failure(
                    operation, FailureKind.TRANSPORT, "monday.com request failed before a response arrived"
                ),
            )

    async def attempt_for_write(
        self, exchange: Exchange, mutation_field: str, output: Any, receipt: Receipt
    ) -> MutationAttempt[Any] | None:
        """Map every sent outcome except an accepted one, as Go's attemptForWrite does."""
        disposition = classify_write(exchange, mutation_field)
        if disposition == "accepted":
            return None
        assert exchange.problem is not None
        if disposition == "notProcessed":
            try:
                await self.fence.record_can_resend()
            except Exception:
                return MutationAttempt.retry(
                    failure(
                        self.operation,
                        FailureKind.AVAILABILITY,
                        "monday.com retry checkpoint could not be recorded",
                    ),
                    exchange.retry_after,
                )
            return MutationAttempt.retry(exchange.problem, exchange.retry_after)
        if disposition == "refused":
            branch = _REFUSAL_BRANCHES.get(exchange.outcome, "providerRejected")
            return await self.reject_or_select_uncertain(branch, output, receipt, exchange.problem)
        return self.resend_or_select_uncertain(output, exchange.problem, exchange.retry_after, receipt)

    async def reject_or_select_uncertain(
        self, branch: str, output: Any, receipt: Receipt, cause: Failure
    ) -> MutationAttempt[Any]:
        """Select a refusal branch only when no earlier send of this call may have written."""
        if self.has_possible_earlier_send:
            return MutationAttempt.uncertain(output, cause, receipt)
        try:
            await self.fence.record_can_resend()
        except Exception:
            return MutationAttempt.retry(
                failure(
                    self.operation,
                    FailureKind.AVAILABILITY,
                    "monday.com rejection checkpoint could not be recorded",
                )
            )
        return MutationAttempt.branch_result(branch, output, receipt, cause)

    def resend_or_select_uncertain(
        self, output: Any, cause: Failure, retry_after: float, receipt: Receipt
    ) -> MutationAttempt[Any]:
        """Retry an unknown outcome while the send cap allows another send."""
        if self.can_resend_unknown_outcome:
            # The claim stays the last marker, so the next attempt counts this send.
            return MutationAttempt.retry(cause, retry_after)
        return MutationAttempt.uncertain(output, cause, receipt)

    async def record_accepted(self, provider_id: str) -> None:
        """Save the accepted identity so a replayed attempt never resends it."""
        try:
            await self.fence.record_provider_id(provider_id)
        except Exception:
            pass  # The accepted result stands; a lost identity checkpoint keeps the claim.


async def _fenced_exchange(
    client: Client,
    call: Call,
    operation: str,
    document: str,
    variables: dict[str, Any],
    output: Any,
) -> tuple[_FencedWrite, Exchange] | MutationAttempt[Any]:
    """Start the fence, resolve credentials, and send once, or return the attempt that ends here."""
    write = await _FencedWrite.start(call, operation, output, client.receipt(call, Exchange("")))
    if isinstance(write, MutationAttempt):
        return write
    credentials, resolution = await client.resolve(call, operation)
    if resolution is not None:
        return await write.pre_send_failure(resolution, output)
    assert credentials is not None
    exchange = await write.dispatch(client, credentials, document, variables, output)
    if isinstance(exchange, MutationAttempt):
        return exchange
    return write, exchange


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
        """Create one item, resending an unknown outcome under the same key only within the send cap."""
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
        started = await _fenced_exchange(self.client, call, "createItem", _CREATE_DOCUMENT, variables, output)
        if isinstance(started, MutationAttempt):
            return started
        write, exchange = started
        receipt = self.client.receipt(call, exchange)
        item_data = exchange.data.get("create_item") if isinstance(exchange.data, dict) else None
        try:
            item = decode_item(item_data) if item_data is not None else None
        except ValueError:
            item = None
        if item is not None:
            await write.record_accepted(item.id)
            output = replace(output, item_id=item.id, item=item, replayed=exchange.replayed)
            return MutationAttempt.branch_result(
                "created", output, replace(receipt, provider_object_id=item.id)
            )
        unusable = failure(
            "createItem", FailureKind.PROTOCOL, "monday.com accepted the item but returned an unusable item"
        )
        reference_id = _accepted_reference_id(exchange.data, "create_item")
        if reference_id:
            await write.record_accepted(reference_id)
            return MutationAttempt.uncertain(
                output, unusable, replace(receipt, provider_object_id=reference_id)
            )
        failed = await write.attempt_for_write(exchange, "create_item", output, receipt)
        if failed is not None:
            return failed
        return write.resend_or_select_uncertain(output, unusable, 0, receipt)


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
        """Set absolute values, retrying every unknown outcome because a repeat writes the same values."""
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
        disposition = classify_write(exchange, "change_multiple_column_values")
        if disposition == "accepted":
            return MutationAttempt.retry(
                failure(
                    "updateItemColumnValues",
                    FailureKind.PROTOCOL,
                    "monday.com returned an unusable item, so the values are set again",
                )
            )
        assert exchange.problem is not None
        if disposition == "refused":
            branch = _REFUSAL_BRANCHES.get(exchange.outcome, "providerRejected")
            return MutationAttempt.branch_result(branch, output, receipt, exchange.problem)
        # Resending would expose the credential to the same faulty endpoint again.
        if exchange.credential_reflected:
            return MutationAttempt.branch_result("invalidResponse", output, receipt, exchange.problem)
        return MutationAttempt.retry(exchange.problem, exchange.retry_after)


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
        """Add one update, resending an unknown outcome under the same key only within the send cap."""
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
        started = await _fenced_exchange(
            self.client, call, "addUpdate", _ADD_DOCUMENT, {"itemId": item_id, "body": escaped}, output
        )
        if isinstance(started, MutationAttempt):
            return started
        write, exchange = started
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
            await write.record_accepted(resource["id"])
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
        unusable = failure(
            "addUpdate",
            FailureKind.PROTOCOL,
            "monday.com accepted the update but returned an unusable update",
        )
        reference_id = _accepted_reference_id(exchange.data, "create_update")
        if reference_id:
            await write.record_accepted(reference_id)
            return MutationAttempt.uncertain(
                output, unusable, replace(receipt, provider_object_id=reference_id)
            )
        failed = await write.attempt_for_write(exchange, "create_update", output, receipt)
        if failed is not None:
            return failed
        return write.resend_or_select_uncertain(output, unusable, 0, receipt)
