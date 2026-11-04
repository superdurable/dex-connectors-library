# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Campaign, GetMemberInput, ListMembersInput, ListMembersOutput, MarketingPermissionInput, Member, SendCampaignInput, SendCampaignOutput, UpdateMemberTagsInput, UpdateMemberTagsOutput, UpsertMemberInput, UpsertMemberOutput
from .definitions import CONNECTOR_ID, ALREADY_SENT, DEFECT, FOUND, GET_MEMBER_DEFINITION, INVALID_RESPONSE, LISTED, LIST_MEMBERS_DEFINITION, NOT_FOUND, NOT_SENDABLE, PROVIDER_REJECTED, SEND_CAMPAIGN_DEFINITION, SENT, UNCERTAIN, UPDATED, UPDATE_MEMBER_TAGS_DEFINITION, UPSERTED, UPSERT_MEMBER_DEFINITION

def get_member_found(value: Member, failure: Failure | None = None) -> Case[Member]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_member_found, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def get_member_not_found(value: Member, failure: Failure | None = None) -> Case[Member]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_member_not_found, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def get_member_provider_rejected(value: Member, failure: Failure | None = None) -> Case[Member]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_member_provider_rejected, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def get_member_invalid_response(value: Member, failure: Failure | None = None) -> Case[Member]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_member_invalid_response, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def get_member_defect(value: Member, failure: Failure | None = None) -> Case[Member]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_member_defect, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def get_member_retry(failure: Failure, after_seconds: float = 0) -> Case[Member]:
    return Case.retry(failure, after_seconds)

setattr(get_member_retry, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def get_member_subscribed_ben() -> Case[Member]:
    return Case.branch_result(FOUND, Member(subscriber_hash='2c66f6142933ed5a97948f89cb1c7be0', email_address='ben@example.com', contact_id='contact-ben', full_name='Ben Carter', status='subscribed', email_type='html', merge_fields={'FNAME': 'Ben'}, tags=('spring-launch', 'vip'), tag_count=2, list_id='57afe96172', last_changed_at=datetime.fromisoformat("2026-10-25T10:00:00Z".replace('Z', '+00:00'))))

setattr(get_member_subscribed_ben, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def get_member_member_missing() -> Case[Member]:
    return Case.branch_result(NOT_FOUND, Member(status=''), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getMember", "Mailchimp could not find the contact."))

setattr(get_member_member_missing, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def get_member_member_read_forbidden() -> Case[Member]:
    return Case.branch_result(PROVIDER_REJECTED, Member(status=''), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getMember", "Mailchimp denied access to the audience."))

setattr(get_member_member_read_forbidden, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def list_members_listed(value: ListMembersOutput, failure: Failure | None = None) -> Case[ListMembersOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_members_listed, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def list_members_not_found(value: ListMembersOutput, failure: Failure | None = None) -> Case[ListMembersOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_members_not_found, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def list_members_provider_rejected(value: ListMembersOutput, failure: Failure | None = None) -> Case[ListMembersOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_members_provider_rejected, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def list_members_invalid_response(value: ListMembersOutput, failure: Failure | None = None) -> Case[ListMembersOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_members_invalid_response, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def list_members_defect(value: ListMembersOutput, failure: Failure | None = None) -> Case[ListMembersOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_members_defect, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def list_members_retry(failure: Failure, after_seconds: float = 0) -> Case[ListMembersOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_members_retry, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def list_members_subscribed_audience() -> Case[ListMembersOutput]:
    return Case.paged(LISTED, (ListMembersOutput(members=(Member(subscriber_hash='2c66f6142933ed5a97948f89cb1c7be0', email_address='ben@example.com', contact_id='contact-ben', full_name='Ben Carter', status='subscribed', email_type='html', merge_fields={'FNAME': 'Ben'}, tags=('spring-launch', 'vip'), tag_count=2, list_id='57afe96172', last_changed_at=datetime.fromisoformat("2026-10-25T10:00:00Z".replace('Z', '+00:00'))),), total_items=3, next_offset=1), ListMembersOutput(members=(Member(subscriber_hash='5156901e497275d2675eb7a3859eb159', email_address='ed@example.com', status='subscribed', tag_count=0, list_id='57afe96172'),), total_items=3, next_offset=2), ListMembersOutput(members=(Member(subscriber_hash='f96c806856eae660aa058bd8039a64ef', email_address='fay@example.com', status='subscribed', tag_count=0, list_id='57afe96172'),), total_items=3, next_offset=0)))

setattr(list_members_subscribed_audience, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def list_members_audience_missing() -> Case[ListMembersOutput]:
    return Case.branch_result(NOT_FOUND, ListMembersOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listMembers", "Mailchimp could not find the audience."))

setattr(list_members_audience_missing, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def list_members_member_list_key_revoked() -> Case[ListMembersOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListMembersOutput(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listMembers", "Mailchimp rejected the API key."))

setattr(list_members_member_list_key_revoked, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def upsert_member_upserted(value: UpsertMemberOutput, failure: Failure | None = None) -> Case[UpsertMemberOutput]:
    return Case.branch_result(UPSERTED, value, failure)

setattr(upsert_member_upserted, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def upsert_member_provider_rejected(value: UpsertMemberOutput, failure: Failure | None = None) -> Case[UpsertMemberOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(upsert_member_provider_rejected, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def upsert_member_invalid_response(value: UpsertMemberOutput, failure: Failure | None = None) -> Case[UpsertMemberOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(upsert_member_invalid_response, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def upsert_member_defect(value: UpsertMemberOutput, failure: Failure | None = None) -> Case[UpsertMemberOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(upsert_member_defect, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def upsert_member_retry(failure: Failure, after_seconds: float = 0) -> Case[UpsertMemberOutput]:
    return Case.retry(failure, after_seconds)

setattr(upsert_member_retry, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def upsert_member_upserted_ben() -> Case[UpsertMemberOutput]:
    return Case.branch_result(UPSERTED, UpsertMemberOutput(member=Member(subscriber_hash='2c66f6142933ed5a97948f89cb1c7be0', email_address='ben@example.com', contact_id='contact-ben', full_name='Ben Carter', status='subscribed', email_type='html', merge_fields={'FNAME': 'Ben'}, tags=('spring-launch', 'vip'), tag_count=2, list_id='57afe96172', last_changed_at=datetime.fromisoformat("2026-10-25T10:00:00Z".replace('Z', '+00:00')))))

setattr(upsert_member_upserted_ben, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def upsert_member_permanently_deleted_address() -> Case[UpsertMemberOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpsertMemberOutput(member=Member(status='')), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "upsertMember", "Mailchimp cannot add a permanently deleted address."))

setattr(upsert_member_permanently_deleted_address, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def update_member_tags_updated(value: UpdateMemberTagsOutput, failure: Failure | None = None) -> Case[UpdateMemberTagsOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_member_tags_updated, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def update_member_tags_not_found(value: UpdateMemberTagsOutput, failure: Failure | None = None) -> Case[UpdateMemberTagsOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_member_tags_not_found, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def update_member_tags_provider_rejected(value: UpdateMemberTagsOutput, failure: Failure | None = None) -> Case[UpdateMemberTagsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_member_tags_provider_rejected, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def update_member_tags_invalid_response(value: UpdateMemberTagsOutput, failure: Failure | None = None) -> Case[UpdateMemberTagsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_member_tags_invalid_response, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def update_member_tags_defect(value: UpdateMemberTagsOutput, failure: Failure | None = None) -> Case[UpdateMemberTagsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_member_tags_defect, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def update_member_tags_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateMemberTagsOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_member_tags_retry, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def update_member_tags_tagged_ben() -> Case[UpdateMemberTagsOutput]:
    return Case.branch_result(UPDATED, UpdateMemberTagsOutput(subscriber_hash='2c66f6142933ed5a97948f89cb1c7be0', active_tags=('spring-launch',)))

setattr(update_member_tags_tagged_ben, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def update_member_tags_tagging_missing_member() -> Case[UpdateMemberTagsOutput]:
    return Case.branch_result(NOT_FOUND, UpdateMemberTagsOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateMemberTags", "Mailchimp could not find the contact to tag."))

setattr(update_member_tags_tagging_missing_member, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def update_member_tags_tagging_forbidden() -> Case[UpdateMemberTagsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateMemberTagsOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "updateMemberTags", "Mailchimp denied tag updates."))

setattr(update_member_tags_tagging_forbidden, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def send_campaign_sent(value: SendCampaignOutput, failure: Failure | None = None) -> Case[SendCampaignOutput]:
    return Case.branch_result(SENT, value, failure)

setattr(send_campaign_sent, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def send_campaign_already_sent(value: SendCampaignOutput, failure: Failure | None = None) -> Case[SendCampaignOutput]:
    return Case.branch_result(ALREADY_SENT, value, failure)

setattr(send_campaign_already_sent, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def send_campaign_not_sendable(value: SendCampaignOutput, failure: Failure | None = None) -> Case[SendCampaignOutput]:
    return Case.branch_result(NOT_SENDABLE, value, failure)

setattr(send_campaign_not_sendable, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def send_campaign_not_found(value: SendCampaignOutput, failure: Failure | None = None) -> Case[SendCampaignOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(send_campaign_not_found, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def send_campaign_provider_rejected(value: SendCampaignOutput, failure: Failure | None = None) -> Case[SendCampaignOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(send_campaign_provider_rejected, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def send_campaign_invalid_response(value: SendCampaignOutput, failure: Failure | None = None) -> Case[SendCampaignOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(send_campaign_invalid_response, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def send_campaign_uncertain(value: SendCampaignOutput, failure: Failure) -> Case[SendCampaignOutput]:
    return Case.uncertain(value, failure)

setattr(send_campaign_uncertain, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def send_campaign_defect(value: SendCampaignOutput, failure: Failure | None = None) -> Case[SendCampaignOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(send_campaign_defect, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def send_campaign_retry(failure: Failure, after_seconds: float = 0) -> Case[SendCampaignOutput]:
    return Case.retry(failure, after_seconds)

setattr(send_campaign_retry, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def send_campaign_sent_spring_campaign() -> Case[SendCampaignOutput]:
    return Case.branch_result(SENT, SendCampaignOutput(campaign=Campaign(id='42694e9e57', type='regular', status='save', title='Spring launch', subject_line='Spring news', list_id='57afe96172', list_name='Spring audience', recipient_count=3, emails_sent=0, created_at=datetime.fromisoformat("2026-10-24T10:00:00Z".replace('Z', '+00:00')))))

setattr(send_campaign_sent_spring_campaign, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def send_campaign_spring_campaign_already_sent() -> Case[SendCampaignOutput]:
    return Case.branch_result(ALREADY_SENT, SendCampaignOutput(campaign=Campaign(id='42694e9e57', type='regular', status='sending', title='Spring launch', list_id='57afe96172', recipient_count=3)))

setattr(send_campaign_spring_campaign_already_sent, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def send_campaign_scheduled_campaign_not_sendable() -> Case[SendCampaignOutput]:
    return Case.branch_result(NOT_SENDABLE, SendCampaignOutput(campaign=Campaign(id='42694e9e57', type='regular', status='schedule', title='Spring launch', list_id='57afe96172', recipient_count=3)), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "sendCampaign", "Mailchimp campaign is not a sendable draft."))

setattr(send_campaign_scheduled_campaign_not_sendable, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def send_campaign_campaign_missing() -> Case[SendCampaignOutput]:
    return Case.branch_result(NOT_FOUND, SendCampaignOutput(campaign=Campaign(status='')), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "sendCampaign", "Mailchimp could not find the campaign."))

setattr(send_campaign_campaign_missing, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

def send_campaign_campaign_checklist_rejected() -> Case[SendCampaignOutput]:
    return Case.branch_result(PROVIDER_REJECTED, SendCampaignOutput(campaign=Campaign(status='')), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "sendCampaign", "Mailchimp rejected the campaign send checklist."))

setattr(send_campaign_campaign_checklist_rejected, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")


class Mock:

    def __init__(self, connection_name: str = "mailchimp") -> None:
        self.get_member_script: Query[GetMemberInput, Member] = Query(GET_MEMBER_DEFINITION)
        self.get_member_script.default(get_member_subscribed_ben())
        self.list_members_script: Query[ListMembersInput, ListMembersOutput] = Query(LIST_MEMBERS_DEFINITION)
        self.list_members_script.default(list_members_subscribed_audience())
        self.upsert_member_script: Mutation[UpsertMemberInput, UpsertMemberOutput] = Mutation(UPSERT_MEMBER_DEFINITION)
        self.upsert_member_script.default(upsert_member_upserted_ben())
        self.update_member_tags_script: Mutation[UpdateMemberTagsInput, UpdateMemberTagsOutput] = Mutation(UPDATE_MEMBER_TAGS_DEFINITION)
        self.update_member_tags_script.default(update_member_tags_tagged_ben())
        self.send_campaign_script: Mutation[SendCampaignInput, SendCampaignOutput] = Mutation(SEND_CAMPAIGN_DEFINITION)
        self.send_campaign_script.default(send_campaign_sent_spring_campaign())
        self._connection = new_connection_with_operations(
            get_member=self.get_member_script,
            list_members=self.list_members_script,
            upsert_member=self.upsert_member_script,
            update_member_tags=self.update_member_tags_script,
            send_campaign=self.send_campaign_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.intuit_mailchimp.mailchimpmock")

__all__ = [
    "Mock",
    "get_member_found",
    "get_member_not_found",
    "get_member_provider_rejected",
    "get_member_invalid_response",
    "get_member_defect",
    "get_member_retry",
    "get_member_subscribed_ben",
    "get_member_member_missing",
    "get_member_member_read_forbidden",
    "list_members_listed",
    "list_members_not_found",
    "list_members_provider_rejected",
    "list_members_invalid_response",
    "list_members_defect",
    "list_members_retry",
    "list_members_subscribed_audience",
    "list_members_audience_missing",
    "list_members_member_list_key_revoked",
    "upsert_member_upserted",
    "upsert_member_provider_rejected",
    "upsert_member_invalid_response",
    "upsert_member_defect",
    "upsert_member_retry",
    "upsert_member_upserted_ben",
    "upsert_member_permanently_deleted_address",
    "update_member_tags_updated",
    "update_member_tags_not_found",
    "update_member_tags_provider_rejected",
    "update_member_tags_invalid_response",
    "update_member_tags_defect",
    "update_member_tags_retry",
    "update_member_tags_tagged_ben",
    "update_member_tags_tagging_missing_member",
    "update_member_tags_tagging_forbidden",
    "send_campaign_sent",
    "send_campaign_already_sent",
    "send_campaign_not_sendable",
    "send_campaign_not_found",
    "send_campaign_provider_rejected",
    "send_campaign_invalid_response",
    "send_campaign_uncertain",
    "send_campaign_defect",
    "send_campaign_retry",
    "send_campaign_sent_spring_campaign",
    "send_campaign_spring_campaign_already_sent",
    "send_campaign_scheduled_campaign_not_sendable",
    "send_campaign_campaign_missing",
    "send_campaign_campaign_checklist_rejected",
]
