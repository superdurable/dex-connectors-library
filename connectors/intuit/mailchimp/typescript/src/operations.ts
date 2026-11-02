// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { GetMemberDefinition, ListMembersDefinition, UpsertMemberDefinition, UpdateMemberTagsDefinition, SendCampaignDefinition } from './generated/definitions.js';
import type { Campaign, GetMemberInput, ListMembersInput, ListMembersOutput, Member, SendCampaignInput, SendCampaignOutput, UpdateMemberTagsInput, UpdateMemberTagsOutput, UpsertMemberInput, UpsertMemberOutput } from './generated/models.js';
import { mailchimpFailure, type MailchimpClient, type MailchimpExchange } from './client.js';

const emptyMember: Member = { subscriberHash: '', emailAddress: '', status: '', tagCount: 0, listId: '' };
const emptyPage: ListMembersOutput = { members: [], totalItems: 0 };
const emptyUpsert: UpsertMemberOutput = { member: emptyMember };
const emptyTags: UpdateMemberTagsOutput = { subscriberHash: '' };
const emptyCampaign: Campaign = { id: '', type: '', status: '', recipientCount: 0, emailsSent: 0 };
const emptySend: SendCampaignOutput = { campaign: emptyCampaign };
const readableStatuses = new Set(['subscribed', 'unsubscribed', 'pending', 'transactional', 'cleaned', 'archived']);
const writableStatuses = new Set(['subscribed', 'unsubscribed', 'pending', 'transactional']);
const memberFields = 'members.id,members.email_address,members.contact_id,members.full_name,members.web_id,members.email_type,members.status,members.merge_fields,members.vip,members.language,members.timestamp_signup,members.timestamp_opt,members.last_changed,members.tags_count,members.tags,members.list_id,list_id,total_items';
const campaignFields = 'id,web_id,type,status,emails_sent,create_time,send_time,settings.title,settings.subject_line,recipients.list_id,recipients.list_name,recipients.recipient_count';

/** Read one audience contact. */
export class GetMemberOperation implements Query<GetMemberInput, Member> {
  /** Bind the client. */ constructor(private readonly client: MailchimpClient) {}
  /** Return the manifest definition. */ definition() { return GetMemberDefinition; }
  /** Read by Mailchimp subscriber hash. */
  async invoke(call: Call, input: GetMemberInput): Promise<QueryAttempt<Member>> {
    const operation = 'getMember';
    const validation = validateMemberAddress(input.listId, input.emailAddress);
    if (validation) return newQueryBranch('defect', emptyMember, zeroReceipt(), mailchimpFailure(operation, 'VALIDATION', validation));
    const connection = await this.client.resolveConnection(call);
    if (!connection) return credentialQueryDefect(operation, emptyMember);
    const hash = subscriberHash(input.emailAddress);
    const result = await this.client.exchange(call, connection, operation, 'GET', memberPath(input.listId, hash));
    const receipt = this.client.receipt(call, result, hash);
    const classified = queryExchange(result, receipt, emptyMember, true);
    if (classified) return classified;
    try { return newQueryBranch('found', decodeMember(result.body, hash), receipt); }
    catch { return newQueryBranch('invalidResponse', emptyMember, receipt, mailchimpFailure(operation, 'PROTOCOL', 'Mailchimp returned an invalid contact')); }
  }
}
/** Read one ordered audience page. */
export class ListMembersOperation implements Query<ListMembersInput, ListMembersOutput> {
  /** Bind the client. */ constructor(private readonly client: MailchimpClient) {}
  /** Return the manifest definition. */ definition() { return ListMembersDefinition; }
  /** Read one page with optional status and time filters. */
  async invoke(call: Call, input: ListMembersInput): Promise<QueryAttempt<ListMembersOutput>> {
    const operation = 'listMembers';
    const validation = validateListInput(input);
    if (validation) return newQueryBranch('defect', emptyPage, zeroReceipt(), mailchimpFailure(operation, 'VALIDATION', validation));
    const connection = await this.client.resolveConnection(call);
    if (!connection) return credentialQueryDefect(operation, emptyPage);
    const pageSize = input.pageSize || 100;
    const query = new URLSearchParams({ count: String(pageSize), offset: String(input.offset ?? 0), sort_field: 'last_changed', sort_dir: 'ASC', fields: memberFields });
    if (input.status) query.set('status', input.status);
    if (input.changedSince) query.set('since_last_changed', formatMailchimpTime(input.changedSince));
    if (input.optedInSince) query.set('since_timestamp_opt', formatMailchimpTime(input.optedInSince));
    const result = await this.client.exchange(call, connection, operation, 'GET', `/lists/${input.listId}/members`, query);
    const receipt = this.client.receipt(call, result, input.listId);
    const classified = queryExchange(result, receipt, emptyPage, true);
    if (classified) return classified;
    try {
      const page = parseObject(result.body);
      const wires = page.members;
      if (!Array.isArray(wires) || !Number.isInteger(page.total_items) || Number(page.total_items) < 0 || wires.length > pageSize) throw new Error('invalid page');
      const members = wires.map(wire => decodeMemberObject(wire, ''));
      const totalItems = Number(page.total_items);
      const offset = (input.offset ?? 0) + members.length;
      return newQueryBranch('listed', { members, totalItems, ...(members.length > 0 && offset < totalItems ? { nextOffset: offset } : {}) }, receipt);
    } catch { return newQueryBranch('invalidResponse', emptyPage, receipt, mailchimpFailure(operation, 'PROTOCOL', 'Mailchimp returned an invalid page of contacts')); }
  }
}
/** Upsert one contact with absolute, repeatable values. */
export class UpsertMemberOperation implements Mutation<UpsertMemberInput, UpsertMemberOutput> {
  /** Bind the client. */ constructor(private readonly client: MailchimpClient) {}
  /** Return the manifest definition. */ definition() { return UpsertMemberDefinition; }
  /** Use the stable Call ID for receipt correlation. */ idempotencyKey(callId: string) { return callId; }
  /** Write one contact without changing an existing subscription unless requested. */
  async invoke(call: Call, input: UpsertMemberInput): Promise<MutationAttempt<UpsertMemberOutput>> {
    const operation = 'upsertMember';
    const validation = validateUpsertInput(input);
    if (validation) return newMutationBranch('defect', emptyUpsert, zeroReceipt(), mailchimpFailure(operation, 'VALIDATION', validation));
    const connection = await this.client.resolveConnection(call);
    if (!connection) return credentialMutationDefect(operation, emptyUpsert);
    const hash = subscriberHash(input.emailAddress);
    const body = { email_address: input.emailAddress, status_if_new: input.statusIfNew,
      ...(input.status ? { status: input.status } : {}),
      ...(input.mergeFields && Object.keys(input.mergeFields).length ? { merge_fields: input.mergeFields } : {}),
      ...(input.marketingPermissions?.length ? { marketing_permissions: input.marketingPermissions.map(permission => ({ marketing_permission_id: permission.marketingPermissionId, enabled: permission.isEnabled })) } : {}) };
    const query = input.shouldSkipMergeValidation ? new URLSearchParams({ skip_merge_validation: 'true' }) : undefined;
    const result = await this.client.exchange(call, connection, operation, 'PUT', memberPath(input.listId, hash), query, body);
    const receipt = this.client.receipt(call, result, hash);
    const classified = mutationExchange(result, receipt, emptyUpsert, false);
    if (classified) return classified;
    try { return newMutationBranch('upserted', { member: decodeMember(result.body, hash) }, receipt); }
    catch { return newMutationBranch('invalidResponse', emptyUpsert, receipt, mailchimpFailure(operation, 'PROTOCOL', 'Mailchimp accepted the contact but returned an invalid contact')); }
  }
}
/** Set contact tags active or inactive with repeatable declarations. */
export class UpdateMemberTagsOperation implements Mutation<UpdateMemberTagsInput, UpdateMemberTagsOutput> {
  /** Bind the client. */ constructor(private readonly client: MailchimpClient) {}
  /** Return the manifest definition. */ definition() { return UpdateMemberTagsDefinition; }
  /** Use the stable Call ID for receipt correlation. */ idempotencyKey(callId: string) { return callId; }
  /** Declare tag states for one contact. */
  async invoke(call: Call, input: UpdateMemberTagsInput): Promise<MutationAttempt<UpdateMemberTagsOutput>> {
    const operation = 'updateMemberTags';
    const validation = validateTagsInput(input);
    if (validation) return newMutationBranch('defect', emptyTags, zeroReceipt(), mailchimpFailure(operation, 'VALIDATION', validation));
    const connection = await this.client.resolveConnection(call);
    if (!connection) return credentialMutationDefect(operation, emptyTags);
    const hash = subscriberHash(input.emailAddress);
    const tags = [...(input.addTags ?? []).map(name => ({ name, status: 'active' })), ...(input.removeTags ?? []).map(name => ({ name, status: 'inactive' }))];
    const body = { tags, ...(input.shouldSuppressAutomations ? { is_syncing: true } : {}) };
    const result = await this.client.exchange(call, connection, operation, 'POST', `${memberPath(input.listId, hash)}/tags`, undefined, body);
    const receipt = this.client.receipt(call, result, hash);
    const classified = mutationExchange(result, receipt, emptyTags, true);
    if (classified) return classified;
    return newMutationBranch('updated', { subscriberHash: hash,
      ...(input.addTags?.length ? { activeTags: input.addTags } : {}), ...(input.removeTags?.length ? { inactiveTags: input.removeTags } : {}) }, receipt);
  }
}
/** Send a reviewed draft campaign at most once. */
export class SendCampaignOperation implements Mutation<SendCampaignInput, SendCampaignOutput> {
  /** Bind the client. */ constructor(private readonly client: MailchimpClient) {}
  /** Return the manifest definition. */ definition() { return SendCampaignDefinition; }
  /** Use the stable Call ID for receipt correlation. */ idempotencyKey(callId: string) { return callId; }
  /** Read status, checkpoint dispatch, and send only a draft. */
  async invoke(call: Call, input: SendCampaignInput): Promise<MutationAttempt<SendCampaignOutput>> {
    const operation = 'sendCampaign';
    const validation = validateResourceID('campaignId', input.campaignId) || (input.expectedListId ? validateResourceID('expectedListId', input.expectedListId) : '');
    if (validation) return newMutationBranch('defect', emptySend, zeroReceipt(), mailchimpFailure(operation, 'VALIDATION', validation));
    const connection = await this.client.resolveConnection(call);
    if (!connection) return credentialMutationDefect(operation, emptySend);
    const query = new URLSearchParams({ fields: campaignFields });
    const earlier = call.context.hasLastHeartbeatValue();
    const read = await this.client.exchange(call, connection, operation, 'GET', `/campaigns/${input.campaignId}`, query);
    const readReceipt = this.client.receipt(call, read, input.campaignId);
    if (earlier) {
      if (read.outcome === 'succeeded') {
        try { const campaign = decodeCampaign(read.body, input.campaignId);
          if (campaign.status === 'sending' || campaign.status === 'sent') return newMutationBranch('alreadySent', { campaign }, readReceipt);
          return newMutationUncertain({ campaign }, mailchimpFailure(operation, 'TRANSPORT', 'an earlier attempt may have sent the campaign'), readReceipt);
        } catch { /* An unreadable status cannot authorize another send. */ }
      }
      return newMutationUncertain(emptySend, mailchimpFailure(operation, 'TRANSPORT', 'an earlier attempt may have sent the campaign and its status could not be read'), readReceipt);
    }
    const classified = mutationExchange(read, readReceipt, emptySend, true);
    if (classified) return classified;
    let campaign: Campaign;
    try { campaign = decodeCampaign(read.body, input.campaignId); }
    catch { return newMutationBranch('invalidResponse', emptySend, readReceipt, mailchimpFailure(operation, 'PROTOCOL', 'Mailchimp returned an invalid campaign, so it was not sent')); }
    const output = { campaign };
    if (campaign.status === 'sending' || campaign.status === 'sent') return newMutationBranch('alreadySent', output, readReceipt);
    const blocker = campaign.status !== 'save' ? 'the campaign is not a draft' : !['regular', 'plaintext', 'absplit', 'variate'].includes(campaign.type) ? 'the campaign type does not send immediately' : input.expectedListId && campaign.listId !== input.expectedListId ? 'the campaign targets another audience than expectedListId' : '';
    if (blocker) return newMutationBranch('notSendable', output, readReceipt, mailchimpFailure(operation, 'CONFLICT', `${blocker}; nothing was sent`));
    try { await call.context.recordHeartbeat({ mailchimpDispatchedCallId: call.id }); }
    catch { return newMutationRetry(mailchimpFailure(operation, 'AVAILABILITY', 'Dex did not record the dispatch checkpoint; nothing was sent')); }
    const sent = await this.client.exchange(call, connection, operation, 'POST', `/campaigns/${input.campaignId}/actions/send`);
    const receipt = this.client.receipt(call, sent, input.campaignId);
    if (sent.outcome === 'succeeded') return newMutationBranch('sent', output, receipt);
    if (sent.outcome === 'rateLimited' || sent.outcome === 'notSent') {
      try { await call.context.recordHeartbeat(null); } catch { /* A stale checkpoint prevents a repeated send. */ }
      return newMutationRetry(sent.failure!, sent.retryAfterMs);
    }
    if (sent.outcome === 'unavailable' || sent.outcome === 'invalid') return newMutationUncertain(output, sent.failure!, receipt);
    if (sent.outcome === 'defect') return newMutationBranch('defect', output, receipt, sent.failure!);
    return newMutationBranch(sent.outcome === 'notFound' ? 'notFound' : 'providerRejected', output, receipt, sent.failure!);
  }
}

function queryExchange<T>(exchange: MailchimpExchange, receipt: ReceiptDraft, zero: T, hasNotFound: boolean): QueryAttempt<T> | undefined {
  if (exchange.outcome === 'succeeded') return undefined;
  if (['rateLimited', 'notSent', 'unavailable'].includes(exchange.outcome)) return newQueryRetry(exchange.failure!, exchange.retryAfterMs);
  const branch = exchange.outcome === 'notFound' && hasNotFound ? 'notFound' : exchange.outcome === 'invalid' ? 'invalidResponse' : exchange.outcome === 'defect' ? 'defect' : 'providerRejected';
  return newQueryBranch(branch, zero, receipt, exchange.failure!);
}
function mutationExchange<T>(exchange: MailchimpExchange, receipt: ReceiptDraft, zero: T, hasNotFound: boolean): MutationAttempt<T> | undefined {
  if (exchange.outcome === 'succeeded') return undefined;
  if (['rateLimited', 'notSent', 'unavailable'].includes(exchange.outcome)) return newMutationRetry(exchange.failure!, exchange.retryAfterMs);
  const branch = exchange.outcome === 'notFound' && hasNotFound ? 'notFound' : exchange.outcome === 'invalid' ? 'invalidResponse' : exchange.outcome === 'defect' ? 'defect' : 'providerRejected';
  return newMutationBranch(branch, zero, receipt, exchange.failure!);
}
function credentialQueryDefect<T>(operation: string, zero: T): QueryAttempt<T> { return newQueryBranch('defect', zero, zeroReceipt(), mailchimpFailure(operation, 'AUTHENTICATION', 'connection credentials are unavailable')); }
function credentialMutationDefect<T>(operation: string, zero: T): MutationAttempt<T> { return newMutationBranch('defect', zero, zeroReceipt(), mailchimpFailure(operation, 'AUTHENTICATION', 'connection credentials are unavailable')); }
function validateResourceID(name: string, value: string): string { return /^[A-Za-z0-9]{1,64}$/.test(value) ? '' : `${name} must be a Mailchimp ID of 1 to 64 letters and digits`; }
function validateEmail(value: string): string {
  if (!value || Buffer.byteLength(value) > 254) return 'emailAddress must be one email address of at most 254 bytes';
  // Go mail.ParseAddress accepts quoted local parts and requires a bare, unchanged mailbox.
  if (/\s|[<>(),;:\\]/.test(value) || !/^(?:[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+|"[^"\r\n]+")@[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$/.test(value) || value.includes('..')) return 'emailAddress must be one bare email address such as jane@example.com';
  return '';
}
function validateMemberAddress(listID: string, email: string): string { return validateResourceID('listId', listID) || validateEmail(email); }
function subscriberHash(email: string): string { return createHash('md5').update(email.toLowerCase(), 'utf8').digest('hex'); }
function memberPath(listID: string, hash: string): string { return `/lists/${listID}/members/${hash}`; }
function validateListInput(input: ListMembersInput): string {
  if (validateResourceID('listId', input.listId)) return validateResourceID('listId', input.listId);
  if (input.status && !readableStatuses.has(input.status)) return 'status is not a Mailchimp contact status';
  if (input.pageSize !== undefined && (!Number.isInteger(input.pageSize) || input.pageSize < 0 || input.pageSize > 1000)) return 'pageSize must be 1 to 1000, or zero for 100';
  if (input.offset !== undefined && (!Number.isInteger(input.offset) || input.offset < 0 || input.offset > 2_147_483_647)) return 'offset must be a non-negative number of contacts';
  for (const bound of [input.changedSince, input.optedInSince]) if (bound && Number.isNaN(Date.parse(bound))) return 'time bound is invalid';
  return '';
}
function formatMailchimpTime(value: string): string { return new Date(value).toISOString().slice(0, 19) + '+00:00'; }
function validateUpsertInput(input: UpsertMemberInput): string {
  const addressError = validateMemberAddress(input.listId, input.emailAddress); if (addressError) return addressError;
  if (!writableStatuses.has(input.statusIfNew)) return 'statusIfNew is required';
  if (input.status && !writableStatuses.has(input.status)) return 'status is invalid';
  if ((input.status === 'subscribed' || input.status === 'pending') && !input.isResubscribeAllowed) return 'isResubscribeAllowed is required';
  if (input.mergeFields) {
    if (Object.keys(input.mergeFields).length > 30) return 'mergeFields holds at most 30 fields';
    for (const [tag, value] of Object.entries(input.mergeFields)) {
      if (!/^[A-Z0-9_]{1,50}$/.test(tag)) return `merge tag ${tag} is invalid`;
      const validText = (text: string) => Buffer.byteLength(text) <= 1024 && !/[\u0000-\u0008\u000b-\u001f\u007f]/.test(text);
      if (typeof value === 'string' && validText(value)) continue;
      if (typeof value === 'number' && Number.isFinite(value)) continue;
      if (value && typeof value === 'object' && !Array.isArray(value) && Object.entries(value).every(([key, text]) => /^[a-z][a-z0-9_]{0,31}$/.test(key) && typeof text === 'string' && validText(text))) continue;
      return `merge field ${tag} is invalid`;
    }
  }
  if ((input.marketingPermissions?.length ?? 0) > 20) return 'marketingPermissions holds at most 20 permissions';
  const seen = new Set<string>();
  for (const permission of input.marketingPermissions ?? []) {
    const invalid = validateResourceID('marketingPermissionId', permission.marketingPermissionId); if (invalid) return invalid;
    if (seen.has(permission.marketingPermissionId)) return 'marketing permission repeats';
    seen.add(permission.marketingPermissionId);
  }
  return '';
}
function validateTagsInput(input: UpdateMemberTagsInput): string {
  const addressError = validateMemberAddress(input.listId, input.emailAddress); if (addressError) return addressError;
  const tags = [...(input.addTags ?? []), ...(input.removeTags ?? [])];
  if (!tags.length || tags.length > 50) return 'addTags and removeTags must hold 1 to 50 tags together';
  const seen = new Set<string>();
  for (const tag of tags) {
    if (!tag || Buffer.byteLength(tag) > 100 || tag.trim() !== tag || /[\u0000-\u001f\u007f]/.test(tag)) return 'tag name is invalid';
    const lower = tag.toLowerCase(); if (seen.has(lower)) return 'tag appears more than once'; seen.add(lower);
  }
  return '';
}
function parseObject(body: Uint8Array): Record<string, unknown> {
  const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('invalid object');
  return value as Record<string, unknown>;
}
function text(object: Record<string, unknown>, key: string): string { return typeof object[key] === 'string' ? object[key] as string : ''; }
function number(object: Record<string, unknown>, key: string): number { return typeof object[key] === 'number' ? object[key] as number : 0; }
function timestamp(value: string): string | undefined { if (!value) return undefined; const parsed = Date.parse(value); if (Number.isNaN(parsed)) throw new Error('invalid timestamp'); return new Date(parsed).toISOString().replace(/\.000Z$/, 'Z'); }
function decodeMember(body: Uint8Array, hash: string): Member { return decodeMemberObject(parseObject(body), hash); }
function decodeMemberObject(value: unknown, expectedHash: string): Member {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('invalid contact');
  const wire = value as Record<string, unknown>; const id = text(wire, 'id');
  if (!/^[0-9a-f]{32}$/.test(id) || expectedHash && id !== expectedHash || !text(wire, 'email_address') || !text(wire, 'status')) throw new Error('invalid contact');
  const tags = Array.isArray(wire.tags) ? wire.tags.map(value => typeof value === 'object' && value !== null ? text(value as Record<string, unknown>, 'name') : '') : [];
  const mergeFields = wire.merge_fields && typeof wire.merge_fields === 'object' && !Array.isArray(wire.merge_fields) ? wire.merge_fields as Record<string, unknown> : undefined;
  return { subscriberHash: id, emailAddress: text(wire, 'email_address'), status: text(wire, 'status'), tagCount: number(wire, 'tags_count'), listId: text(wire, 'list_id'),
    ...(text(wire, 'contact_id') ? { contactId: text(wire, 'contact_id') } : {}), ...(text(wire, 'full_name') ? { fullName: text(wire, 'full_name') } : {}),
    ...(text(wire, 'email_type') ? { emailType: text(wire, 'email_type') } : {}), ...(mergeFields && Object.keys(mergeFields).length ? { mergeFields } : {}),
    ...(tags.length ? { tags } : {}), ...(wire.vip ? { isVip: true } : {}), ...(text(wire, 'language') ? { language: text(wire, 'language') } : {}),
    ...(number(wire, 'web_id') ? { webId: number(wire, 'web_id') } : {}),
    ...(timestamp(text(wire, 'timestamp_signup')) ? { signedUpAt: timestamp(text(wire, 'timestamp_signup')) } : {}),
    ...(timestamp(text(wire, 'timestamp_opt')) ? { optedInAt: timestamp(text(wire, 'timestamp_opt')) } : {}),
    ...(timestamp(text(wire, 'last_changed')) ? { lastChangedAt: timestamp(text(wire, 'last_changed')) } : {}) };
}
function decodeCampaign(body: Uint8Array, expectedID: string): Campaign {
  const wire = parseObject(body); const settings = wire.settings && typeof wire.settings === 'object' ? wire.settings as Record<string, unknown> : {};
  const recipients = wire.recipients && typeof wire.recipients === 'object' ? wire.recipients as Record<string, unknown> : {};
  if (text(wire, 'id') !== expectedID || !text(wire, 'status') || !text(wire, 'type')) throw new Error('invalid campaign');
  return { id: expectedID, type: text(wire, 'type'), status: text(wire, 'status'), recipientCount: number(recipients, 'recipient_count'), emailsSent: number(wire, 'emails_sent'),
    ...(number(wire, 'web_id') ? { webId: number(wire, 'web_id') } : {}), ...(text(settings, 'title') ? { title: text(settings, 'title') } : {}),
    ...(text(settings, 'subject_line') ? { subjectLine: text(settings, 'subject_line') } : {}), ...(text(recipients, 'list_id') ? { listId: text(recipients, 'list_id') } : {}),
    ...(text(recipients, 'list_name') ? { listName: text(recipients, 'list_name') } : {}),
    ...(timestamp(text(wire, 'create_time')) ? { createdAt: timestamp(text(wire, 'create_time')) } : {}),
    ...(timestamp(text(wire, 'send_time')) ? { sentAt: timestamp(text(wire, 'send_time')) } : {}) };
}
