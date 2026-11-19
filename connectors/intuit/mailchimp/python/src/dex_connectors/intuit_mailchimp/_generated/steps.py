# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from collections.abc import Callable, Mapping
from typing import Any, TypeVar

import dex
from dex_connectors.sdk import (
    MutationResult,
    MutationStep,
    MutationStepConfig,
    QueryResult,
    QueryStep,
    QueryStepConfig,
    StepAnnotations,
)

from ..connection import Connection
from ..models import GetMemberInput, ListMembersInput, ListMembersOutput, Member, SendCampaignInput, SendCampaignOutput, UpdateMemberTagsInput, UpdateMemberTagsOutput, UpsertMemberInput, UpsertMemberOutput

StepInput = TypeVar("StepInput")

def new_get_member_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetMemberInput],
    found: dex.StepTarget[QueryResult[Member]],
    not_found: dex.StepTarget[QueryResult[Member]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Member]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Member]] | None = None,
    defect: dex.StepTarget[QueryResult[Member]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Member]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetMemberInput, Member]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Mailchimp Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Mailchimp Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Mailchimp Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "found": found,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "get_member"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_member_step, '__module__', 'dex_connectors.intuit_mailchimp.steps')

def new_list_members_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListMembersInput],
    listed: dex.StepTarget[QueryResult[ListMembersOutput]],
    not_found: dex.StepTarget[QueryResult[ListMembersOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ListMembersOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ListMembersOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[ListMembersOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ListMembersOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListMembersInput, ListMembersOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Mailchimp Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Mailchimp Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Mailchimp Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "list_members"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_members_step, '__module__', 'dex_connectors.intuit_mailchimp.steps')

def new_upsert_member_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpsertMemberInput],
    upserted: dex.StepTarget[MutationResult[UpsertMemberOutput]],
    provider_rejected: dex.StepTarget[MutationResult[UpsertMemberOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UpsertMemberOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpsertMemberOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpsertMemberOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpsertMemberInput, UpsertMemberOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Mailchimp Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Mailchimp Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Mailchimp Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "upserted": upserted,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "upsert_member"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_upsert_member_step, '__module__', 'dex_connectors.intuit_mailchimp.steps')

def new_update_member_tags_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdateMemberTagsInput],
    updated: dex.StepTarget[MutationResult[UpdateMemberTagsOutput]],
    not_found: dex.StepTarget[MutationResult[UpdateMemberTagsOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpdateMemberTagsOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UpdateMemberTagsOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpdateMemberTagsOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpdateMemberTagsOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdateMemberTagsInput, UpdateMemberTagsOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Mailchimp Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Mailchimp Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Mailchimp Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "updated": updated,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "update_member_tags"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_update_member_tags_step, '__module__', 'dex_connectors.intuit_mailchimp.steps')

def new_send_campaign_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SendCampaignInput],
    sent: dex.StepTarget[MutationResult[SendCampaignOutput]],
    already_sent: dex.StepTarget[MutationResult[SendCampaignOutput]] | None = None,
    not_sendable: dex.StepTarget[MutationResult[SendCampaignOutput]] | None = None,
    not_found: dex.StepTarget[MutationResult[SendCampaignOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[SendCampaignOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[SendCampaignOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[SendCampaignOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[SendCampaignOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[SendCampaignOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, SendCampaignInput, SendCampaignOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Mailchimp Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Mailchimp Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Mailchimp Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "sent": sent,
    }
    if already_sent is not None:
        branches["alreadySent"] = already_sent
    if not_sendable is not None:
        branches["notSendable"] = not_sendable
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "send_campaign"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_send_campaign_step, '__module__', 'dex_connectors.intuit_mailchimp.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
