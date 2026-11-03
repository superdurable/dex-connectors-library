# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Provider parity scenarios for the Airtable operations."""

import json
from collections.abc import Callable
from datetime import UTC, datetime
from uuid import uuid4

import httpx
import pytest
from dex_connectors.sdk import (
    Call,
    CallID,
    ConnectionRef,
    FailureKind,
    IdempotencyKey,
    RetryError,
    SecretString,
    StaticCredentialProvider,
    run_mutation,
    run_query,
)

from dex_connectors.airtable import (
    DEFECT,
    FOUND,
    GET_RECORD_DEFINITION,
    INVALID_RESPONSE,
    LIST_RECORDS_DEFINITION,
    LISTED,
    NOT_FOUND,
    OFFSET_EXPIRED,
    PROVIDER_REJECTED,
    UPDATE_RECORDS_DEFINITION,
    UPDATED,
    UPSERT_RECORDS_DEFINITION,
    UPSERTED,
    Client,
    Config,
    Credentials,
    FieldEqualityFilter,
    GetRecordInput,
    ListRecordsInput,
    RecordSort,
    RecordUpdate,
    RecordUpsert,
    UpdateRecordsInput,
    UpsertRecordsInput,
    build_filter_formula,
)

CONNECTION = ConnectionRef("airtable", "airtable")
TOKEN = "patTEST.token"
BASE = "appABCDEFGHIJKLMN"
RECORD_ID = "recABCDEFGHIJKLMN"
OTHER_ID = "recNOPQRSTUVWXYZ0"
WIRE_RECORD = {
    "id": RECORD_ID,
    "createdTime": "2026-09-01T09:00:00.000Z",
    "fields": {"Policy Key": "refund-standard"},
}


def make_client(
    respond: Callable[[httpx.Request], httpx.Response], requests: list[httpx.Request] | None = None
) -> Client:
    def record(request: httpx.Request) -> httpx.Response:
        if requests is not None:
            requests.append(request)
        return respond(request)

    return Client(
        Config("http://localhost:8848", max_response_bytes=4000),
        StaticCredentialProvider({CONNECTION: Credentials(SecretString(TOKEN))}),
        httpx.AsyncClient(transport=httpx.MockTransport(record)),
    )


def fixed(response: httpx.Response) -> Callable[[httpx.Request], httpx.Response]:
    return lambda request: response


def call_for(definition: object) -> Call:
    call_id = CallID(str(uuid4()))
    return Call(call_id, CONNECTION, definition.operation, IdempotencyKey(str(call_id)))  # type: ignore[attr-defined]


def test_filter_formula_escapes_like_go() -> None:
    formula = build_filter_formula(
        (
            FieldEqualityFilter("Policy Key", text='refund "standard"'),
            FieldEqualityFilter("Amount", number=1e21),
            FieldEqualityFilter("Approved", checkbox=True),
            FieldEqualityFilter("Archived", checkbox=False),
        ),
        "{Amount} > 100",
    )
    assert formula == (
        'AND({Policy Key} = "refund \\"standard\\"", {Amount} = 1000000000000000000000, '
        "{Approved} = TRUE(), NOT({Archived}), ({Amount} > 100))"
    )
    assert build_filter_formula((), " ") == ""
    assert build_filter_formula((FieldEqualityFilter("N", number=0.5),), "") == "{N} = 0.5"


@pytest.mark.parametrize(
    "equality_filter",
    [
        FieldEqualityFilter("a{b", text="x"),
        FieldEqualityFilter("a", text="x\\y"),
        FieldEqualityFilter("a", text="line\nbreak"),
        FieldEqualityFilter("a"),
        FieldEqualityFilter("a", text="x", number=1),
        FieldEqualityFilter("a", number=float("inf")),
    ],
)
def test_invalid_filters_raise(equality_filter: FieldEqualityFilter) -> None:
    with pytest.raises(ValueError):
        build_filter_formula((equality_filter,), "")


@pytest.mark.asyncio
async def test_list_records_posts_body_and_decodes_page() -> None:
    requests: list[httpx.Request] = []
    body = {"records": [WIRE_RECORD], "offset": "itrNEXT/recNEXT"}
    result = await run_query(
        make_client(fixed(httpx.Response(200, json=body)), requests).list_records(),
        call_for(LIST_RECORDS_DEFINITION),
        ListRecordsInput(
            BASE,
            "Policies",
            (FieldEqualityFilter("Policy Key", text="refund-standard"),),
            fields=("Policy Key", "Policy Key"),
            sort=(RecordSort("Policy Key", "desc"),),
            view_id_or_name="Grid view",
            page_size=5,
        ),
    )
    assert result.branch == LISTED
    assert result.value.records[0].created_time == datetime(2026, 9, 1, 9, 0, tzinfo=UTC)
    assert result.value.offset == "itrNEXT/recNEXT"
    assert result.value.filter_formula == '{Policy Key} = "refund-standard"'
    request = requests[0]
    assert request.method == "POST"
    assert request.url.raw_path == f"/v0/{BASE}/Policies/listRecords".encode()
    assert request.headers["Authorization"] == "Bearer " + TOKEN
    assert json.loads(request.content) == {
        "pageSize": 5,
        "filterByFormula": '{Policy Key} = "refund-standard"',
        "fields": ["Policy Key"],
        "sort": [{"field": "Policy Key", "direction": "desc"}],
        "view": "Grid view",
    }
    assert result.receipt.provider_object_id == f"{BASE}/Policies"


@pytest.mark.asyncio
async def test_expired_offset_selects_offset_expired() -> None:
    body = {"error": {"type": "LIST_RECORDS_ITERATOR_NOT_AVAILABLE", "message": "secret text"}}
    result = await run_query(
        make_client(fixed(httpx.Response(422, json=body))).list_records(),
        call_for(LIST_RECORDS_DEFINITION),
        ListRecordsInput(BASE, "Policies", offset="itrOLD"),
    )
    assert result.branch == OFFSET_EXPIRED
    assert result.failure is not None and result.failure.kind == FailureKind.NOT_FOUND


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        ListRecordsInput("bad", "Policies"),
        ListRecordsInput(BASE, ""),
        ListRecordsInput(BASE, "Policies", page_size=101),
        ListRecordsInput(BASE, "Policies", sort=(RecordSort("a", "up"),)),
        ListRecordsInput(BASE, "Policies", offset="x" * 1025),
    ],
)
async def test_invalid_list_is_defect_without_request(value: ListRecordsInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_query(
        make_client(fixed(httpx.Response(200, json={"records": []})), requests).list_records(),
        call_for(LIST_RECORDS_DEFINITION),
        value,
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("body", "branch"),
    [
        (WIRE_RECORD, FOUND),
        ({**WIRE_RECORD, "id": OTHER_ID}, INVALID_RESPONSE),
        ({**WIRE_RECORD, "createdTime": "yesterday"}, INVALID_RESPONSE),
        ({**WIRE_RECORD, "fields": None}, FOUND),
    ],
)
async def test_get_record_decoding(body: object, branch: str) -> None:
    result = await run_query(
        make_client(fixed(httpx.Response(200, json=body))).get_record(),
        call_for(GET_RECORD_DEFINITION),
        GetRecordInput(BASE, "tblABCDEFGHIJKLMN", RECORD_ID),
    )
    assert result.branch == branch


@pytest.mark.asyncio
async def test_upsert_sends_perform_upsert_and_validates_answer() -> None:
    requests: list[httpx.Request] = []
    body = {
        "records": [WIRE_RECORD],
        "createdRecords": [RECORD_ID],
        "updatedRecords": [],
        "details": {"message": "partialSuccess", "reasons": ["attachmentsFailedUploading", "other"]},
    }
    result = await run_mutation(
        make_client(fixed(httpx.Response(200, json=body)), requests).upsert_records(),
        call_for(UPSERT_RECORDS_DEFINITION),
        UpsertRecordsInput(
            BASE,
            "Log",
            ("Case ID",),
            (RecordUpsert({"Case ID": "case-1", "Amount": 120, "Policy": [RECORD_ID]}),),
            True,
        ),
    )
    assert result.branch == UPSERTED
    assert result.value.created_record_ids == (RECORD_ID,)
    assert result.value.partial_success_reasons == ("attachmentsFailedUploading",)
    assert requests[0].method == "PATCH"
    assert json.loads(requests[0].content) == {
        "performUpsert": {"fieldsToMergeOn": ["Case ID"]},
        "records": [{"fields": {"Case ID": "case-1", "Amount": 120, "Policy": [RECORD_ID]}}],
        "typecast": True,
    }


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "value",
    [
        UpsertRecordsInput(BASE, "Log", (), (RecordUpsert({"a": 1}),)),
        UpsertRecordsInput(BASE, "Log", ("a", "a"), (RecordUpsert({"a": 1}),)),
        UpsertRecordsInput(BASE, "Log", ("a",), (RecordUpsert({"b": 1}),)),
        UpsertRecordsInput(BASE, "Log", ("a",), (RecordUpsert({"a": None}),)),
        UpsertRecordsInput(BASE, "Log", ("a",), (RecordUpsert({"a": 1}), RecordUpsert({"a": 1}))),
        UpsertRecordsInput(BASE, "Log", ("a",), (RecordUpsert({}),)),
        UpsertRecordsInput(BASE, "Log", ("a",), (RecordUpsert({"a": float("nan")}),)),
        UpsertRecordsInput(BASE, "Log", ("a",), tuple(RecordUpsert({"a": index}) for index in range(11))),
    ],
)
async def test_invalid_upsert_is_defect(value: UpsertRecordsInput) -> None:
    requests: list[httpx.Request] = []
    result = await run_mutation(
        make_client(fixed(httpx.Response(200)), requests).upsert_records(),
        call_for(UPSERT_RECORDS_DEFINITION),
        value,
    )
    assert result.branch == DEFECT
    assert requests == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "body",
    [
        {"records": [WIRE_RECORD], "createdRecords": [RECORD_ID]},
        {"records": [], "createdRecords": [], "updatedRecords": []},
        {"records": [WIRE_RECORD], "createdRecords": [OTHER_ID], "updatedRecords": []},
        {"records": [WIRE_RECORD], "createdRecords": [RECORD_ID], "updatedRecords": [RECORD_ID]},
    ],
)
async def test_inconsistent_upsert_answer_is_invalid(body: object) -> None:
    result = await run_mutation(
        make_client(fixed(httpx.Response(200, json=body))).upsert_records(),
        call_for(UPSERT_RECORDS_DEFINITION),
        UpsertRecordsInput(BASE, "Log", ("Case ID",), (RecordUpsert({"Case ID": "case-1"}),)),
    )
    assert result.branch == INVALID_RESPONSE


@pytest.mark.asyncio
async def test_update_patches_by_id_and_maps_not_found() -> None:
    requests: list[httpx.Request] = []
    result = await run_mutation(
        make_client(fixed(httpx.Response(200, json={"records": [WIRE_RECORD]})), requests).update_records(),
        call_for(UPDATE_RECORDS_DEFINITION),
        UpdateRecordsInput(BASE, "Policies", (RecordUpdate(RECORD_ID, {"Last Decided Case": None}),)),
    )
    assert result.branch == UPDATED
    assert json.loads(requests[0].content) == {
        "records": [{"id": RECORD_ID, "fields": {"Last Decided Case": None}}]
    }
    missing = await run_mutation(
        make_client(fixed(httpx.Response(404, json={"error": "NOT_FOUND"}))).update_records(),
        call_for(UPDATE_RECORDS_DEFINITION),
        UpdateRecordsInput(BASE, "Policies", (RecordUpdate(RECORD_ID, {"a": 1}),)),
    )
    assert missing.branch == NOT_FOUND
    assert missing.failure is not None and missing.failure.message.endswith("(HTTP 404, NOT_FOUND)")


@pytest.mark.asyncio
async def test_upsert_maps_404_to_provider_rejected() -> None:
    result = await run_mutation(
        make_client(fixed(httpx.Response(404))).upsert_records(),
        call_for(UPSERT_RECORDS_DEFINITION),
        UpsertRecordsInput(BASE, "Log", ("a",), (RecordUpsert({"a": 1}),)),
    )
    assert result.branch == PROVIDER_REJECTED
    assert result.failure is not None and result.failure.kind == FailureKind.NOT_FOUND


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("status", "kind"),
    [
        (401, FailureKind.AUTHENTICATION),
        (403, FailureKind.AUTHORIZATION),
        (400, FailureKind.VALIDATION),
        (302, FailureKind.PROTOCOL),
        (501, FailureKind.PROVIDER_REJECTION),
    ],
)
async def test_rejections(status: int, kind: FailureKind) -> None:
    result = await run_query(
        make_client(fixed(httpx.Response(status))).get_record(),
        call_for(GET_RECORD_DEFINITION),
        GetRecordInput(BASE, "Policies", RECORD_ID),
    )
    assert result.branch == PROVIDER_REJECTED
    assert result.failure is not None and result.failure.kind == kind


@pytest.mark.asyncio
async def test_rate_limit_holds_the_base_and_retries_after_cooldown() -> None:
    requests: list[httpx.Request] = []
    client = make_client(fixed(httpx.Response(429, headers={"Retry-After": "5"})), requests)
    with pytest.raises(RetryError) as raised:
        await run_query(
            client.get_record(), call_for(GET_RECORD_DEFINITION), GetRecordInput(BASE, "Policies", RECORD_ID)
        )
    assert raised.value.failure.kind == FailureKind.RATE_LIMIT
    assert raised.value.retry_after_seconds == 30
    with pytest.raises(RetryError) as paced:
        await run_query(
            client.get_record(), call_for(GET_RECORD_DEFINITION), GetRecordInput(BASE, "Policies", RECORD_ID)
        )
    assert paced.value.failure.message == "Airtable requests to this base are paced to its rate limit"
    assert len(requests) == 1


@pytest.mark.asyncio
async def test_oversized_answer_is_invalid_response() -> None:
    result = await run_query(
        make_client(fixed(httpx.Response(200, content=b"x" * 5000))).get_record(),
        call_for(GET_RECORD_DEFINITION),
        GetRecordInput(BASE, "Policies", RECORD_ID),
    )
    assert result.branch == INVALID_RESPONSE
    assert result.failure is not None and result.failure.kind == FailureKind.RESPONSE_TOO_LARGE
