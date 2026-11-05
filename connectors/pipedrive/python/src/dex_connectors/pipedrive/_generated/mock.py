# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CRMObject, CreateObjectInput, GetObjectInput, ListObjectsInput, ObjectPage, SearchObjectsInput, UpdateObjectInput, UpsertObjectInput, UpsertedObject
from .definitions import CONNECTOR_ID, CREATED, CREATE_OBJECT_DEFINITION, DEFECT, FOUND, GET_OBJECT_DEFINITION, INVALID_RESPONSE, LISTED, LIST_OBJECTS_DEFINITION, MULTIPLE_MATCHES, NOT_FOUND, PROVIDER_REJECTED, SEARCHED, SEARCH_OBJECTS_DEFINITION, UNCERTAIN, UPDATED, UPDATE_OBJECT_DEFINITION, UPSERTED, UPSERT_OBJECT_DEFINITION

def search_objects_searched(value: ObjectPage, failure: Failure | None = None) -> Case[ObjectPage]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_objects_searched, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def search_objects_provider_rejected(value: ObjectPage, failure: Failure | None = None) -> Case[ObjectPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_objects_provider_rejected, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def search_objects_invalid_response(value: ObjectPage, failure: Failure | None = None) -> Case[ObjectPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_objects_invalid_response, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def search_objects_defect(value: ObjectPage, failure: Failure | None = None) -> Case[ObjectPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_objects_defect, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def search_objects_retry(failure: Failure, after_seconds: float = 0) -> Case[ObjectPage]:
    return Case.retry(failure, after_seconds)

setattr(search_objects_retry, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def search_objects_searched_default_answer() -> Case[ObjectPage]:
    return Case.paged(SEARCHED, (ObjectPage(object_type='deals', objects=(CRMObject(object_type='deals', id='301', name='Acme renewal', is_partial=True),), next_cursor='page-two'), ObjectPage(object_type='deals', objects=(CRMObject(object_type='deals', id='302', name='Acme expansion', is_partial=True), CRMObject(object_type='deals', id='301', name='Acme renewal', is_partial=True)), next_cursor='page-three'), ObjectPage(object_type='deals', objects=(), next_cursor='')))

setattr(search_objects_searched_default_answer, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def search_objects_provider_rejected_failure() -> Case[ObjectPage]:
    return Case.branch_result(PROVIDER_REJECTED, ObjectPage(object_type=''), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "searchObjects", "Pipedrive denied access (HTTP 403)."))

setattr(search_objects_provider_rejected_failure, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def search_objects_invalid_response_failure() -> Case[ObjectPage]:
    return Case.branch_result(INVALID_RESPONSE, ObjectPage(object_type=''), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "searchObjects", "Pipedrive returned a malformed success body."))

setattr(search_objects_invalid_response_failure, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def get_object_found(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_object_found, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def get_object_not_found(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_object_not_found, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def get_object_provider_rejected(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_object_provider_rejected, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def get_object_invalid_response(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_object_invalid_response, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def get_object_defect(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_object_defect, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def get_object_retry(failure: Failure, after_seconds: float = 0) -> Case[CRMObject]:
    return Case.retry(failure, after_seconds)

setattr(get_object_retry, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def get_object_found_default_answer() -> Case[CRMObject]:
    return Case.branch_result(FOUND, CRMObject(object_type='deals', id='301', name='Acme renewal', organization_id='201', person_id='101', stage_id='3', status='open'))

setattr(get_object_found_default_answer, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def get_object_not_found_failure() -> Case[CRMObject]:
    return Case.branch_result(NOT_FOUND, CRMObject(object_type=''), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getObject", "Pipedrive found no such record (HTTP 404)."))

setattr(get_object_not_found_failure, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def get_object_provider_rejected_failure() -> Case[CRMObject]:
    return Case.branch_result(PROVIDER_REJECTED, CRMObject(object_type=''), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getObject", "Pipedrive denied access (HTTP 403)."))

setattr(get_object_provider_rejected_failure, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def get_object_invalid_response_failure() -> Case[CRMObject]:
    return Case.branch_result(INVALID_RESPONSE, CRMObject(object_type=''), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getObject", "Pipedrive returned a malformed success body."))

setattr(get_object_invalid_response_failure, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def list_objects_listed(value: ObjectPage, failure: Failure | None = None) -> Case[ObjectPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_objects_listed, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def list_objects_provider_rejected(value: ObjectPage, failure: Failure | None = None) -> Case[ObjectPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_objects_provider_rejected, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def list_objects_invalid_response(value: ObjectPage, failure: Failure | None = None) -> Case[ObjectPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_objects_invalid_response, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def list_objects_defect(value: ObjectPage, failure: Failure | None = None) -> Case[ObjectPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_objects_defect, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def list_objects_retry(failure: Failure, after_seconds: float = 0) -> Case[ObjectPage]:
    return Case.retry(failure, after_seconds)

setattr(list_objects_retry, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def list_objects_listed_default_answer() -> Case[ObjectPage]:
    return Case.paged(LISTED, (ObjectPage(object_type='deals', objects=(CRMObject(object_type='deals', id='301', name='Acme renewal', organization_id='201', person_id='101', stage_id='3', status='open'),), next_cursor='page-two'), ObjectPage(object_type='deals', objects=(CRMObject(object_type='deals', id='302', name='Acme expansion', organization_id='201', person_id='101', stage_id='3', status='open'), CRMObject(object_type='deals', id='301', name='Acme renewal', organization_id='201', person_id='101', stage_id='3', status='open')), next_cursor='page-three'), ObjectPage(object_type='deals', objects=(), next_cursor='')))

setattr(list_objects_listed_default_answer, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def list_objects_provider_rejected_failure() -> Case[ObjectPage]:
    return Case.branch_result(PROVIDER_REJECTED, ObjectPage(object_type=''), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listObjects", "Pipedrive denied access (HTTP 403)."))

setattr(list_objects_provider_rejected_failure, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def list_objects_invalid_response_failure() -> Case[ObjectPage]:
    return Case.branch_result(INVALID_RESPONSE, ObjectPage(object_type=''), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listObjects", "Pipedrive returned a malformed success body."))

setattr(list_objects_invalid_response_failure, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def upsert_object_upserted(value: UpsertedObject, failure: Failure | None = None) -> Case[UpsertedObject]:
    return Case.branch_result(UPSERTED, value, failure)

setattr(upsert_object_upserted, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def upsert_object_multiple_matches(value: UpsertedObject, failure: Failure | None = None) -> Case[UpsertedObject]:
    return Case.branch_result(MULTIPLE_MATCHES, value, failure)

setattr(upsert_object_multiple_matches, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def upsert_object_provider_rejected(value: UpsertedObject, failure: Failure | None = None) -> Case[UpsertedObject]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(upsert_object_provider_rejected, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def upsert_object_invalid_response(value: UpsertedObject, failure: Failure | None = None) -> Case[UpsertedObject]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(upsert_object_invalid_response, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def upsert_object_uncertain(value: UpsertedObject, failure: Failure) -> Case[UpsertedObject]:
    return Case.uncertain(value, failure)

setattr(upsert_object_uncertain, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def upsert_object_defect(value: UpsertedObject, failure: Failure | None = None) -> Case[UpsertedObject]:
    return Case.branch_result(DEFECT, value, failure)

setattr(upsert_object_defect, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def upsert_object_retry(failure: Failure, after_seconds: float = 0) -> Case[UpsertedObject]:
    return Case.retry(failure, after_seconds)

setattr(upsert_object_retry, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def upsert_object_upserted_default_answer() -> Case[UpsertedObject]:
    return Case.branch_result(UPSERTED, UpsertedObject(object=CRMObject(object_type='persons', id='101', name='Jane Smith', organization_id='201', emails=('jane@acme.example.com',)), created=False))

setattr(upsert_object_upserted_default_answer, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def upsert_object_multiple_matches_failure() -> Case[UpsertedObject]:
    return Case.branch_result(MULTIPLE_MATCHES, UpsertedObject(object=CRMObject(object_type='')), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "upsertObject", "Two Pipedrive persons share the email address."))

setattr(upsert_object_multiple_matches_failure, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def upsert_object_provider_rejected_failure() -> Case[UpsertedObject]:
    return Case.branch_result(PROVIDER_REJECTED, UpsertedObject(object=CRMObject(object_type='')), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "upsertObject", "Pipedrive rejected the field value (HTTP 400)."))

setattr(upsert_object_provider_rejected_failure, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def upsert_object_invalid_response_failure() -> Case[UpsertedObject]:
    return Case.branch_result(INVALID_RESPONSE, UpsertedObject(object=CRMObject(object_type='')), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "upsertObject", "Pipedrive returned a malformed success body."))

setattr(upsert_object_invalid_response_failure, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def create_object_created(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_object_created, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def create_object_provider_rejected(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_object_provider_rejected, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def create_object_uncertain(value: CRMObject, failure: Failure) -> Case[CRMObject]:
    return Case.uncertain(value, failure)

setattr(create_object_uncertain, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def create_object_defect(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_object_defect, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def create_object_retry(failure: Failure, after_seconds: float = 0) -> Case[CRMObject]:
    return Case.retry(failure, after_seconds)

setattr(create_object_retry, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def create_object_created_default_answer() -> Case[CRMObject]:
    return Case.branch_result(CREATED, CRMObject(object_type='deals', id='301', name='Acme renewal', organization_id='201', person_id='101', stage_id='3', status='open'))

setattr(create_object_created_default_answer, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def create_object_provider_rejected_failure() -> Case[CRMObject]:
    return Case.branch_result(PROVIDER_REJECTED, CRMObject(object_type=''), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "createObject", "Pipedrive rejected the field value (HTTP 400)."))

setattr(create_object_provider_rejected_failure, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def update_object_updated(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_object_updated, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def update_object_not_found(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_object_not_found, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def update_object_provider_rejected(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_object_provider_rejected, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def update_object_invalid_response(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_object_invalid_response, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def update_object_defect(value: CRMObject, failure: Failure | None = None) -> Case[CRMObject]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_object_defect, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def update_object_retry(failure: Failure, after_seconds: float = 0) -> Case[CRMObject]:
    return Case.retry(failure, after_seconds)

setattr(update_object_retry, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def update_object_updated_default_answer() -> Case[CRMObject]:
    return Case.branch_result(UPDATED, CRMObject(object_type='deals', id='301', name='Acme renewal', organization_id='201', person_id='101', stage_id='3', status='open'))

setattr(update_object_updated_default_answer, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def update_object_not_found_failure() -> Case[CRMObject]:
    return Case.branch_result(NOT_FOUND, CRMObject(object_type=''), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateObject", "Pipedrive found no such record (HTTP 404)."))

setattr(update_object_not_found_failure, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def update_object_provider_rejected_failure() -> Case[CRMObject]:
    return Case.branch_result(PROVIDER_REJECTED, CRMObject(object_type=''), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "updateObject", "Pipedrive rejected the field value (HTTP 400)."))

setattr(update_object_provider_rejected_failure, '__module__', "dex_connectors.pipedrive.pipedrivemock")

def update_object_invalid_response_failure() -> Case[CRMObject]:
    return Case.branch_result(INVALID_RESPONSE, CRMObject(object_type=''), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "updateObject", "Pipedrive returned a malformed success body."))

setattr(update_object_invalid_response_failure, '__module__', "dex_connectors.pipedrive.pipedrivemock")


class Mock:

    def __init__(self, connection_name: str = "pipedrive") -> None:
        self.search_objects_script: Query[SearchObjectsInput, ObjectPage] = Query(SEARCH_OBJECTS_DEFINITION)
        self.search_objects_script.default(search_objects_searched_default_answer())
        self.get_object_script: Query[GetObjectInput, CRMObject] = Query(GET_OBJECT_DEFINITION)
        self.get_object_script.default(get_object_found_default_answer())
        self.list_objects_script: Query[ListObjectsInput, ObjectPage] = Query(LIST_OBJECTS_DEFINITION)
        self.list_objects_script.default(list_objects_listed_default_answer())
        self.upsert_object_script: Mutation[UpsertObjectInput, UpsertedObject] = Mutation(UPSERT_OBJECT_DEFINITION)
        self.upsert_object_script.default(upsert_object_upserted_default_answer())
        self.create_object_script: Mutation[CreateObjectInput, CRMObject] = Mutation(CREATE_OBJECT_DEFINITION)
        self.create_object_script.default(create_object_created_default_answer())
        self.update_object_script: Mutation[UpdateObjectInput, CRMObject] = Mutation(UPDATE_OBJECT_DEFINITION)
        self.update_object_script.default(update_object_updated_default_answer())
        self._connection = new_connection_with_operations(
            search_objects=self.search_objects_script,
            get_object=self.get_object_script,
            list_objects=self.list_objects_script,
            upsert_object=self.upsert_object_script,
            create_object=self.create_object_script,
            update_object=self.update_object_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.pipedrive.pipedrivemock")

__all__ = [
    "Mock",
    "search_objects_searched",
    "search_objects_provider_rejected",
    "search_objects_invalid_response",
    "search_objects_defect",
    "search_objects_retry",
    "search_objects_searched_default_answer",
    "search_objects_provider_rejected_failure",
    "search_objects_invalid_response_failure",
    "get_object_found",
    "get_object_not_found",
    "get_object_provider_rejected",
    "get_object_invalid_response",
    "get_object_defect",
    "get_object_retry",
    "get_object_found_default_answer",
    "get_object_not_found_failure",
    "get_object_provider_rejected_failure",
    "get_object_invalid_response_failure",
    "list_objects_listed",
    "list_objects_provider_rejected",
    "list_objects_invalid_response",
    "list_objects_defect",
    "list_objects_retry",
    "list_objects_listed_default_answer",
    "list_objects_provider_rejected_failure",
    "list_objects_invalid_response_failure",
    "upsert_object_upserted",
    "upsert_object_multiple_matches",
    "upsert_object_provider_rejected",
    "upsert_object_invalid_response",
    "upsert_object_uncertain",
    "upsert_object_defect",
    "upsert_object_retry",
    "upsert_object_upserted_default_answer",
    "upsert_object_multiple_matches_failure",
    "upsert_object_provider_rejected_failure",
    "upsert_object_invalid_response_failure",
    "create_object_created",
    "create_object_provider_rejected",
    "create_object_uncertain",
    "create_object_defect",
    "create_object_retry",
    "create_object_created_default_answer",
    "create_object_provider_rejected_failure",
    "update_object_updated",
    "update_object_not_found",
    "update_object_provider_rejected",
    "update_object_invalid_response",
    "update_object_defect",
    "update_object_retry",
    "update_object_updated_default_answer",
    "update_object_not_found_failure",
    "update_object_provider_rejected_failure",
    "update_object_invalid_response_failure",
]
