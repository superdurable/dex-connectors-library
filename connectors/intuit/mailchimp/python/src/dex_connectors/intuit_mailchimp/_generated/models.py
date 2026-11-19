# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# GetMemberInput identifies one audience contact.
@dataclass(frozen=True, slots=True)
class GetMemberInput:
    # ListID is the audience ID, Mailchimp's list_id, such as 57afe96172.
    list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'listId'})
    # EmailAddress is the contact's bare email address. The connector requests the MD5 hash of its
    # lowercased form, so letter case does not matter.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress'})


setattr(GetMemberInput, '__module__', 'dex_connectors.intuit_mailchimp.models')


# Member is one audience contact as Mailchimp returned it.
@dataclass(frozen=True, slots=True)
class Member:
    # SubscriberHash is Mailchimp's member id: the MD5 hash of the lowercased email address.
    subscriber_hash: str = _dataclass_field(default='', metadata={'dex_json_name': 'subscriberHash'})
    # EmailAddress is the contact's email address.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress'})
    # ContactID is Mailchimp's contact_id, which stays stable when the address changes.
    contact_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'contactId', 'dex_json_omit_default': True})
    # FullName is the contact's full name, or empty.
    full_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'fullName', 'dex_json_omit_default': True})
    # Status is the contact's current status, such as subscribed or unsubscribed.
    status: str = _dataclass_field(kw_only=True)
    # EmailType is html or text, or empty.
    email_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailType', 'dex_json_omit_default': True})
    # MergeFields maps merge tags such as FNAME to Mailchimp's stored values: strings, numbers, or
    # address objects.
    merge_fields: dict[str, Any] = _dataclass_field(default_factory=dict, metadata={'dex_json_name': 'mergeFields', 'dex_json_omit_default': True})
    # Tags holds the names of at most 50 of the contact's tags, as Mailchimp returns them.
    tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # TagCount is the contact's total number of tags, which can exceed len(Tags).
    tag_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'tagCount'})
    # IsVIP reports Mailchimp's VIP flag.
    is_vip: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isVip', 'dex_json_omit_default': True})
    # Language is the contact's language code, or empty.
    language: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ListID is the audience ID.
    list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'listId'})
    # WebID is the ID of the contact's page in the Mailchimp web app, or zero.
    web_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'webId', 'dex_json_omit_default': True})
    # SignedUpAt is when the contact signed up, or nil when Mailchimp has no signup time.
    signed_up_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'signedUpAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # OptedInAt is when the contact confirmed opt-in, or nil.
    opted_in_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'optedInAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # LastChangedAt is when the contact's information last changed, or nil.
    last_changed_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'lastChangedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(Member, '__module__', 'dex_connectors.intuit_mailchimp.models')


# ListMembersInput selects one page of an audience's contacts. Pages are ordered by last change,
# oldest first, and continue with Offset.
@dataclass(frozen=True, slots=True)
class ListMembersInput:
    # ListID is the audience ID, Mailchimp's list_id.
    list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'listId'})
    # Status keeps only contacts in one status, including cleaned or archived; empty lists every status.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # ChangedSince keeps only contacts whose information changed after this time; nil applies no bound.
    changed_since: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'changedSince', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # OptedInSince keeps only contacts who opted in after this time; nil applies no bound.
    opted_in_since: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'optedInSince', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # PageSize is the number of contacts to read, 1 to 1000; zero reads 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Offset is the number of matching contacts to skip; use the previous page's NextOffset.
    offset: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListMembersInput, '__module__', 'dex_connectors.intuit_mailchimp.models')


# ListMembersOutput is one page of contacts.
@dataclass(frozen=True, slots=True)
class ListMembersOutput:
    # Members are this page's contacts, oldest change first.
    members: tuple[Member, ...] = ()
    # TotalItems is Mailchimp's count of every contact that matches the filters.
    total_items: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalItems'})
    # NextOffset is the Offset of the following page, or zero after the last page.
    next_offset: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextOffset', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'members', _replace_nil_slices(self.members, 1))


ListMembersOutput.__annotations__['members'] = tuple[Member, ...] | None
ListMembersOutput.__dataclass_fields__['members'].type = tuple[Member, ...] | None
setattr(ListMembersOutput, '__module__', 'dex_connectors.intuit_mailchimp.models')


# MarketingPermissionInput is one GDPR marketing permission the contact opted in to or out of.
@dataclass(frozen=True, slots=True)
class MarketingPermissionInput:
    # MarketingPermissionID is the audience's marketing_permission_id, listed on a contact read with
    # marketing permissions enabled.
    marketing_permission_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'marketingPermissionId'})
    # IsEnabled reports that the contact opted in.
    is_enabled: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isEnabled'})


setattr(MarketingPermissionInput, '__module__', 'dex_connectors.intuit_mailchimp.models')


# UpsertMemberInput adds a contact to an audience or updates the existing contact with the same
# address. Mailchimp applies StatusIfNew only when it creates the contact, and Status to an existing
# contact too, so an unsubscribed contact keeps its status unless Status says otherwise.
@dataclass(frozen=True, slots=True)
class UpsertMemberInput:
    # ListID is the audience ID, Mailchimp's list_id.
    list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'listId'})
    # EmailAddress is the contact's bare email address; letter case does not change which contact is written.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress'})
    # StatusIfNew is the status of a contact Mailchimp creates: subscribed for a contact who gave
    # permission, pending to send Mailchimp's confirmation email first, unsubscribed, or
    # transactional. It is required and ignored for an existing contact.
    status_if_new: str = _dataclass_field(kw_only=True, metadata={'dex_json_name': 'statusIfNew'})
    # Status, when set, also changes an existing contact's status: subscribed, pending, unsubscribed,
    # or transactional. Empty keeps the existing contact's status. Unsubscribed records an opt-out.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # IsResubscribeAllowed must be true when Status is subscribed or pending, because Mailchimp then
    # resubscribes, or emails a confirmation to, a contact who unsubscribed or was cleaned. Set it
    # only when the contact asked to subscribe again.
    is_resubscribe_allowed: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isResubscribeAllowed', 'dex_json_omit_default': True})
    # MergeFields maps merge tags such as FNAME to values: a string, a number, or an address object
    # with addr1, city, state, and zip. Tags that are omitted keep their stored values.
    merge_fields: dict[str, Any] = _dataclass_field(default_factory=dict, metadata={'dex_json_name': 'mergeFields', 'dex_json_omit_default': True})
    # MarketingPermissions records the contact's consent to the audience's GDPR marketing permissions.
    marketing_permissions: tuple[MarketingPermissionInput, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'marketingPermissions', 'dex_json_omit_default': True})
    # ShouldSkipMergeValidation sends skip_merge_validation=true, so Mailchimp accepts the contact
    # without the audience's required merge fields.
    should_skip_merge_validation: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'shouldSkipMergeValidation', 'dex_json_omit_default': True})


setattr(UpsertMemberInput, '__module__', 'dex_connectors.intuit_mailchimp.models')


# UpsertMemberOutput is the contact after the write.
@dataclass(frozen=True, slots=True)
class UpsertMemberOutput:
    # Member is the contact Mailchimp returned. Its Status shows whether an existing contact kept
    # its status, such as unsubscribed.
    member: Member


setattr(UpsertMemberOutput, '__module__', 'dex_connectors.intuit_mailchimp.models')


# UpdateMemberTagsInput adds and removes tags on one existing audience contact.
@dataclass(frozen=True, slots=True)
class UpdateMemberTagsInput:
    # ListID is the audience ID, Mailchimp's list_id.
    list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'listId'})
    # EmailAddress is the contact's bare email address.
    email_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'emailAddress'})
    # AddTags are declared active. Mailchimp creates a tag that does not exist yet, and a tag the
    # contact already has stays as it is.
    add_tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'addTags', 'dex_json_omit_default': True})
    # RemoveTags are declared inactive; a tag the contact does not have is ignored.
    remove_tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'removeTags', 'dex_json_omit_default': True})
    # ShouldSuppressAutomations sends is_syncing=true, so Mailchimp does not start automations
    # triggered by these tag changes.
    should_suppress_automations: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'shouldSuppressAutomations', 'dex_json_omit_default': True})


setattr(UpdateMemberTagsInput, '__module__', 'dex_connectors.intuit_mailchimp.models')


# UpdateMemberTagsOutput reports the tag declarations Mailchimp accepted.
@dataclass(frozen=True, slots=True)
class UpdateMemberTagsOutput:
    # SubscriberHash is the contact's MD5 subscriber hash.
    subscriber_hash: str = _dataclass_field(default='', metadata={'dex_json_name': 'subscriberHash'})
    # ActiveTags are the tags declared active.
    active_tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'activeTags', 'dex_json_omit_default': True})
    # InactiveTags are the tags declared inactive.
    inactive_tags: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'inactiveTags', 'dex_json_omit_default': True})


setattr(UpdateMemberTagsOutput, '__module__', 'dex_connectors.intuit_mailchimp.models')


# SendCampaignInput identifies one existing campaign to send now. Create and review the campaign in
# Mailchimp first; the connector never edits campaign content or recipients.
@dataclass(frozen=True, slots=True)
class SendCampaignInput:
    # CampaignID is Mailchimp's campaign ID, such as 42694e9e57.
    campaign_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'campaignId'})
    # ExpectedListID, when set, sends only when the campaign targets this audience; a campaign whose
    # audience was changed after it was approved selects notSendable. Empty skips the check.
    expected_list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'expectedListId', 'dex_json_omit_default': True})


setattr(SendCampaignInput, '__module__', 'dex_connectors.intuit_mailchimp.models')


# Campaign is the part of a Mailchimp campaign a send decision needs.
@dataclass(frozen=True, slots=True)
class Campaign:
    # ID is Mailchimp's campaign ID.
    id: str = ''
    # WebID is the ID of the campaign's page in the Mailchimp web app, or zero.
    web_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'webId', 'dex_json_omit_default': True})
    # Type is regular, plaintext, absplit, rss, or variate.
    type: str = ''
    # Status is the campaign status Mailchimp reported when the connector read it.
    status: str = _dataclass_field(kw_only=True)
    # Title is the campaign's internal title, or empty.
    title: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # SubjectLine is the campaign's subject line, or empty.
    subject_line: str = _dataclass_field(default='', metadata={'dex_json_name': 'subjectLine', 'dex_json_omit_default': True})
    # ListID is the audience the campaign sends to.
    list_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'listId', 'dex_json_omit_default': True})
    # ListName is that audience's name.
    list_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'listName', 'dex_json_omit_default': True})
    # RecipientCount is Mailchimp's count of the campaign's recipients.
    recipient_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'recipientCount'})
    # EmailsSent is the number of emails Mailchimp sent for the campaign so far.
    emails_sent: int = _dataclass_field(default=0, metadata={'dex_json_name': 'emailsSent'})
    # CreatedAt is when the campaign was created, or nil.
    created_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # SentAt is when the campaign was sent, or nil.
    sent_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'sentAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})


setattr(Campaign, '__module__', 'dex_connectors.intuit_mailchimp.models')


# SendCampaignOutput is the campaign as the connector read it.
@dataclass(frozen=True, slots=True)
class SendCampaignOutput:
    # Campaign is the campaign Mailchimp returned in the read before the send, or in the read that
    # found it already sending or sent. After sent, its Status is still the draft status save.
    campaign: Campaign


setattr(SendCampaignOutput, '__module__', 'dex_connectors.intuit_mailchimp.models')
