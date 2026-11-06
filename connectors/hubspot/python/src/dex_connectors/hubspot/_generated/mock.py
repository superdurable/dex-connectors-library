# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CRMObject, GetObjectInput, ObjectPage, SearchFilter, SearchFilterGroup, SearchObjectsInput, SearchSort, UpdateObjectInput, UpsertObjectInput, UpsertedObject
from .definitions import CONNECTOR_ID, CONFLICT, DEFECT, FOUND, GET_OBJECT_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, SEARCHED, SEARCH_OBJECTS_DEFINITION, UPDATED, UPDATE_OBJECT_DEFINITION, UPSERTED, UPSERT_OBJECT_DEFINITION

def search_objects_searched(value: ObjectPage, failure: Failure | None = None) -> Case[ObjectPage]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_objects_searched, '__module__', "dex_connectors.hubspot.hubspotmock")

def search_objects_provider_rejected(value: ObjectPage, failure: Failure | None = None) -> Case[ObjectPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_objects_provider_rejected, '__module__', "dex_connectors.hubspot.hubspotmock")

def search_objects_invalid_response(value: ObjectPage, failure: Failure | None = None) -> Case[ObjectPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_objects_invalid_response, '__module__', "dex_connectors.hubspot.hubspotmock")

def search_objects_defect(value: ObjectPage, failure: Failure | None = None) -> Case[ObjectPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_objects_defect, '__module__', "dex_connectors.hubspot.hubspotmock")

def search_objects_retry(failure: Failure, after_seconds: float = 0) -> Case[ObjectPage]:
    return Case.retry(failure, after_seconds)

setattr(search_objects_retry, '__module__', "dex_connectors.hubspot.hubspotmock")

def search_objects_recent_open_deals() -> Case[ObjectPage]:
    return Case.paged(SEARCHED, (ObjectPage(object_type='deals', total=2, objects=(CRMObject(object_type='deals', id='9001', properties={'dealname': 'Analytical Engines renewal', 'dealstage': 'appointmentscheduled', 'pipeline': 'default', 'hs_object_id': '9001', 'createdate': '2026-10-01T09:00:00.000Z', 'hs_lastmodifieddate': '2026-10-25T10:00:00.000Z'}, created_at=datetime.fromisoformat("2026-10-01T09:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-25T10:00:00Z".replace('Z', '+00:00'))),), next_after='1'), ObjectPage(object_type='deals', total=2, objects=(CRMObject(object_type='deals', id='9001', properties={'dealname': 'Analytical Engines renewal', 'dealstage': 'appointmentscheduled', 'pipeline': 'default', 'hs_object_id': '9001', 'createdate': '2026-10-01T09:00:00.000Z', 'hs_lastmodifieddate': '2026-10-25T10:00:00.000Z'}, created_at=datetime.fromisoformat("2026-10-01T09:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-25T10:00:00Z".replace('Z', '+00:00'))),), next_after='2'), ObjectPage(object_type='deals', total=2, objects=(CRMObject(object_type='deals', id='9004', properties={'dealname': 'Earlier consultation', 'dealstage': 'appointmentscheduled', 'pipeline': 'default', 'hs_object_id': '9004', 'createdate': '2026-10-10T09:00:00.000Z', 'hs_lastmodifieddate': '2026-10-20T10:00:00.000Z'}, created_at=datetime.fromisoformat("2026-10-10T09:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-20T10:00:00Z".replace('Z', '+00:00'))),))))

setattr(search_objects_recent_open_deals, '__module__', "dex_connectors.hubspot.hubspotmock")

def search_objects_search_rejected() -> Case[ObjectPage]:
    return Case.branch_result(PROVIDER_REJECTED, ObjectPage(object_type=''), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "searchObjects", "HubSpot rejected the CRM search filter."))

setattr(search_objects_search_rejected, '__module__', "dex_connectors.hubspot.hubspotmock")

def get_object_found(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_object_found, '__module__', "dex_connectors.hubspot.hubspotmock")

def get_object_not_found(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_object_not_found, '__module__', "dex_connectors.hubspot.hubspotmock")

def get_object_provider_rejected(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_object_provider_rejected, '__module__', "dex_connectors.hubspot.hubspotmock")

def get_object_invalid_response(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_object_invalid_response, '__module__', "dex_connectors.hubspot.hubspotmock")

def get_object_defect(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_object_defect, '__module__', "dex_connectors.hubspot.hubspotmock")

def get_object_retry(failure: Failure, after_seconds: float = 0) -> Case[CRMObject]:
    return Case.retry(failure, after_seconds)

setattr(get_object_retry, '__module__', "dex_connectors.hubspot.hubspotmock")

def get_object_qualified_deal() -> Case[CRMObject]:
    return Case.branch_result(FOUND, CRMObject(object_type='deals', id='9001', properties={'dealname': 'Analytical Engines renewal', 'dealstage': 'qualifiedtobuy', 'pipeline': 'default', 'hs_object_id': '9001', 'createdate': '2026-10-01T09:00:00.000Z', 'hs_lastmodifieddate': '2026-10-25T10:05:00.000Z'}, created_at=datetime.fromisoformat("2026-10-01T09:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-25T10:05:00Z".replace('Z', '+00:00'))))

setattr(get_object_qualified_deal, '__module__', "dex_connectors.hubspot.hubspotmock")

def get_object_object_missing() -> Case[CRMObject]:
    return Case.branch_result(NOT_FOUND, CRMObject(object_type=''), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getObject", "HubSpot could not find the CRM record."))

setattr(get_object_object_missing, '__module__', "dex_connectors.hubspot.hubspotmock")

def get_object_object_read_rejected() -> Case[CRMObject]:
    return Case.branch_result(PROVIDER_REJECTED, CRMObject(object_type=''), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getObject", "HubSpot rejected the CRM record read."))

setattr(get_object_object_read_rejected, '__module__', "dex_connectors.hubspot.hubspotmock")

def upsert_object_upserted(value: UpsertedObject, failure: Failure | None = None) -> Case[UpsertedObject]:
    return Case.branch_result(UPSERTED, value, failure)

setattr(upsert_object_upserted, '__module__', "dex_connectors.hubspot.hubspotmock")

def upsert_object_conflict(value: UpsertedObject, failure: Failure | None = None) -> Case[UpsertedObject]:
    return Case.branch_result(CONFLICT, value, failure)

setattr(upsert_object_conflict, '__module__', "dex_connectors.hubspot.hubspotmock")

def upsert_object_provider_rejected(value: UpsertedObject, failure: Failure | None = None) -> Case[UpsertedObject]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(upsert_object_provider_rejected, '__module__', "dex_connectors.hubspot.hubspotmock")

def upsert_object_invalid_response(value: UpsertedObject, failure: Failure | None = None) -> Case[UpsertedObject]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(upsert_object_invalid_response, '__module__', "dex_connectors.hubspot.hubspotmock")

def upsert_object_defect(value: UpsertedObject, failure: Failure | None = None) -> Case[UpsertedObject]:
    return Case.branch_result(DEFECT, value, failure)

setattr(upsert_object_defect, '__module__', "dex_connectors.hubspot.hubspotmock")

def upsert_object_retry(failure: Failure, after_seconds: float = 0) -> Case[UpsertedObject]:
    return Case.retry(failure, after_seconds)

setattr(upsert_object_retry, '__module__', "dex_connectors.hubspot.hubspotmock")

def upsert_object_upserted_lead() -> Case[UpsertedObject]:
    return Case.branch_result(UPSERTED, UpsertedObject(object=CRMObject(object_type='contacts', id='501', properties={'email': 'ada@example.com', 'firstname': 'Ada', 'lastname': 'Lovelace', 'company': 'Analytical Engines', 'hubspot_owner_id': '77', 'hs_object_id': '501', 'createdate': '2026-09-15T08:00:00.000Z', 'lastmodifieddate': '2026-10-25T10:04:00.000Z'}, created_at=datetime.fromisoformat("2026-09-15T08:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-25T10:04:00Z".replace('Z', '+00:00'))), created=False))

setattr(upsert_object_upserted_lead, '__module__', "dex_connectors.hubspot.hubspotmock")

def upsert_object_upsert_conflict() -> Case[UpsertedObject]:
    return Case.branch_result(CONFLICT, UpsertedObject(object=CRMObject(object_type='')), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "upsertObject", "Another HubSpot contact owns the email."))

setattr(upsert_object_upsert_conflict, '__module__', "dex_connectors.hubspot.hubspotmock")

def upsert_object_upsert_rejected() -> Case[UpsertedObject]:
    return Case.branch_result(PROVIDER_REJECTED, UpsertedObject(object=CRMObject(object_type='')), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "upsertObject", "HubSpot rejected the contact properties."))

setattr(upsert_object_upsert_rejected, '__module__', "dex_connectors.hubspot.hubspotmock")

def update_object_updated(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_object_updated, '__module__', "dex_connectors.hubspot.hubspotmock")

def update_object_not_found(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_object_not_found, '__module__', "dex_connectors.hubspot.hubspotmock")

def update_object_conflict(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(CONFLICT, value, failure)

setattr(update_object_conflict, '__module__', "dex_connectors.hubspot.hubspotmock")

def update_object_provider_rejected(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_object_provider_rejected, '__module__', "dex_connectors.hubspot.hubspotmock")

def update_object_invalid_response(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_object_invalid_response, '__module__', "dex_connectors.hubspot.hubspotmock")

def update_object_defect(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_object_defect, '__module__', "dex_connectors.hubspot.hubspotmock")

def update_object_retry(failure: Failure, after_seconds: float = 0) -> Case[CRMObject]:
    return Case.retry(failure, after_seconds)

setattr(update_object_retry, '__module__', "dex_connectors.hubspot.hubspotmock")

def update_object_advanced_deal() -> Case[CRMObject]:
    return Case.branch_result(UPDATED, CRMObject(object_type='deals', id='9001', properties={'dealname': 'Analytical Engines renewal', 'dealstage': 'qualifiedtobuy', 'pipeline': 'default', 'hs_object_id': '9001', 'createdate': '2026-10-01T09:00:00.000Z', 'hs_lastmodifieddate': '2026-10-25T10:05:00.000Z'}, created_at=datetime.fromisoformat("2026-10-01T09:00:00Z".replace('Z', '+00:00')), updated_at=datetime.fromisoformat("2026-10-25T10:05:00Z".replace('Z', '+00:00'))))

setattr(update_object_advanced_deal, '__module__', "dex_connectors.hubspot.hubspotmock")

def update_object_deal_missing() -> Case[CRMObject]:
    return Case.branch_result(NOT_FOUND, CRMObject(object_type=''), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateObject", "HubSpot could not find the deal."))

setattr(update_object_deal_missing, '__module__', "dex_connectors.hubspot.hubspotmock")

def update_object_deal_conflict() -> Case[CRMObject]:
    return Case.branch_result(CONFLICT, CRMObject(object_type=''), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "updateObject", "HubSpot rejected the conflicting stage update."))

setattr(update_object_deal_conflict, '__module__', "dex_connectors.hubspot.hubspotmock")

def update_object_update_rejected() -> Case[CRMObject]:
    return Case.branch_result(PROVIDER_REJECTED, CRMObject(object_type=''), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "updateObject", "HubSpot rejected the deal stage update."))

setattr(update_object_update_rejected, '__module__', "dex_connectors.hubspot.hubspotmock")


class Mock:

    def __init__(self, connection_name: str = "hubspot") -> None:
        self.search_objects_script: Query[SearchObjectsInput, ObjectPage] = Query(SEARCH_OBJECTS_DEFINITION)
        self.search_objects_script.default(search_objects_recent_open_deals())
        self.get_object_script: Query[GetObjectInput, CRMObject] = Query(GET_OBJECT_DEFINITION)
        self.get_object_script.default(get_object_qualified_deal())
        self.upsert_object_script: Mutation[UpsertObjectInput, UpsertedObject] = Mutation(UPSERT_OBJECT_DEFINITION)
        self.upsert_object_script.default(upsert_object_upserted_lead())
        self.update_object_script: Mutation[UpdateObjectInput, CRMObject] = Mutation(UPDATE_OBJECT_DEFINITION)
        self.update_object_script.default(update_object_advanced_deal())
        self._connection = new_connection_with_operations(
            search_objects=self.search_objects_script,
            get_object=self.get_object_script,
            upsert_object=self.upsert_object_script,
            update_object=self.update_object_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.hubspot.hubspotmock")

__all__ = [
    "Mock",
    "search_objects_searched",
    "search_objects_provider_rejected",
    "search_objects_invalid_response",
    "search_objects_defect",
    "search_objects_retry",
    "search_objects_recent_open_deals",
    "search_objects_search_rejected",
    "get_object_found",
    "get_object_not_found",
    "get_object_provider_rejected",
    "get_object_invalid_response",
    "get_object_defect",
    "get_object_retry",
    "get_object_qualified_deal",
    "get_object_object_missing",
    "get_object_object_read_rejected",
    "upsert_object_upserted",
    "upsert_object_conflict",
    "upsert_object_provider_rejected",
    "upsert_object_invalid_response",
    "upsert_object_defect",
    "upsert_object_retry",
    "upsert_object_upserted_lead",
    "upsert_object_upsert_conflict",
    "upsert_object_upsert_rejected",
    "update_object_updated",
    "update_object_not_found",
    "update_object_conflict",
    "update_object_provider_rejected",
    "update_object_invalid_response",
    "update_object_defect",
    "update_object_retry",
    "update_object_advanced_deal",
    "update_object_deal_missing",
    "update_object_deal_conflict",
    "update_object_update_rejected",
]
