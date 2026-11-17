// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { CheckpointError, newFence, type DecisionKind, type Fence } from '@superdurable/connectors-sdk/singledispatch';
import { GetMemberDefinition, ListMembersDefinition, UpsertMemberDefinition, UpdateMemberTagsDefinition, SendCampaignDefinition } from './generated/definitions.js';
import type { Campaign, GetMemberInput, ListMembersInput, ListMembersOutput, Member, SendCampaignInput, SendCampaignOutput, UpdateMemberTagsInput, UpdateMemberTagsOutput, UpsertMemberInput, UpsertMemberOutput } from './generated/models.js';
import { mailchimpFailure, type MailchimpClient, type MailchimpExchange } from './client.js';
import { goRFC3339UTC, goSimpleLower, goTrimSpace, isGoBareEmailAddress } from './go-parity.js';

const emptyMember: Member = { subscriberHash: '', emailAddress: '', status: '', tagCount: 0, listId: '' };
const emptyPage: ListMembersOutput = { members: [], totalItems: 0 };
const emptyUpsert: UpsertMemberOutput = { member: emptyMember };
const emptyTags: UpdateMemberTagsOutput = { subscriberHash: '' };
const emptyCampaign: Campaign = { id: '', type: '', status: '', recipientCount: 0, emailsSent: 0 };
const emptySend: SendCampaignOutput = { campaign: emptyCampaign };
const readableStatuses = new Set(['subscribed', 'unsubscribed', 'pending', 'transactional', 'cleaned', 'archived']);
const writableStatuses = new Set(['subscribed', 'unsubscribed', 'pending', 'transactional']);
export const memberFields = 'members.id,members.email_address,members.contact_id,members.full_name,members.web_id,members.email_type,members.status,members.merge_fields,members.vip,members.language,members.timestamp_signup,members.timestamp_opt,members.last_changed,members.tags_count,members.tags,members.list_id,list_id,total_items';
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
      const wires = page.members ?? [];
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
  /** Read status and send only a draft under the single-dispatch fence; resend an unknown outcome only below the cap. */
  async invoke(call: Call, input: SendCampaignInput): Promise<MutationAttempt<SendCampaignOutput>> {
    const operation = 'sendCampaign';
    const validation = validateResourceID('campaignId', input.campaignId) || (input.expectedListId ? validateResourceID('expectedListId', input.expectedListId) : '');
    if (validation) return newMutationBranch('defect', emptySend, zeroReceipt(), mailchimpFailure(operation, 'VALIDATION', validation));
    if (hasLegacyDispatch(call)) return readBackWithoutSend(this.client, call, input, 'an earlier attempt of this Step may have sent the campaign');
    const operationMaxSends = definitionMaxSends(SendCampaignDefinition);
    let fence: Fence;
    try { fence = newFence(call, SendCampaignDefinition.stepDefaults.executeMethodTimeoutMs ?? 60_000, { maxSends: operationMaxSends }); }
    catch { return newMutationBranch('defect', emptySend, zeroReceipt(), mailchimpFailure(operation, 'LOCAL_DEFECT', 'the Mailchimp dispatch fence could not be constructed')); }
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return newMutationRetry(mailchimpFailure(operation, 'AVAILABILITY', 'the dispatch checkpoint could not be read; nothing was sent in this attempt'));
    if (decision.kind === 'readBackRequired') return readBackWithoutSend(this.client, call, input, 'the send cap is reached');
    const priorSends = decision.priorSendUpperBound ?? 0;
    return new SendCampaignAttempt(this.client, call, input, fence, decision.kind,
      priorSends + 1 < effectiveMaxSends(call, operationMaxSends), priorSends > 0).readThenSend();
  }
}

/** One fenced sendCampaign attempt after the Fence decided it may send. */
class SendCampaignAttempt {
  /**
   * Bind one attempt. canResendUnknownOutcome is false when this send is the last the cap allows;
   * hasPossibleEarlierSend means a refusal or a changed campaign cannot prove nothing was sent.
   */
  constructor(private readonly client: MailchimpClient, private readonly call: Call, private readonly input: SendCampaignInput,
    private readonly fence: Fence, private readonly decisionKind: DecisionKind,
    private readonly canResendUnknownOutcome: boolean, private readonly hasPossibleEarlierSend: boolean) {}

  /** Read the campaign, which is also the read-back of an earlier send, and send a draft. */
  async readThenSend(): Promise<MutationAttempt<SendCampaignOutput>> {
    const operation = 'sendCampaign';
    const connection = await this.client.resolveConnection(this.call);
    if (!connection) {
      if (this.hasPossibleEarlierSend) return newMutationUncertain(emptySend, mailchimpFailure(operation, 'AUTHENTICATION', 'an earlier attempt may have sent the campaign, and the connection credentials are unavailable to read it; reconnect the connection'), zeroReceipt());
      return credentialMutationDefect(operation, emptySend);
    }
    const read = await this.client.exchange(this.call, connection, operation, 'GET', `/campaigns/${this.input.campaignId}`, new URLSearchParams({ fields: campaignFields }));
    const readReceipt = this.client.receipt(this.call, read, this.input.campaignId);
    if (read.outcome === 'rateLimited' || read.outcome === 'notSent' || read.outcome === 'unavailable') return this.retryBeforeSend(read.failure!, read.retryAfterMs);
    if (read.outcome !== 'succeeded') return this.refuseOrSelectUncertain(campaignReadBranch(read.outcome), emptySend, read.failure!, readReceipt);
    let campaign: Campaign;
    try { campaign = decodeCampaign(read.body, this.input.campaignId); }
    catch { return this.refuseOrSelectUncertain('invalidResponse', emptySend, mailchimpFailure(operation, 'PROTOCOL', 'Mailchimp returned an invalid campaign, so it was not sent'), readReceipt); }
    const output = { campaign };
    if (campaign.status === 'sending' || campaign.status === 'sent') return newMutationBranch('alreadySent', output, readReceipt);
    const blocker = campaign.status !== 'save' ? 'the campaign is not a draft' : !['regular', 'plaintext', 'absplit', 'variate'].includes(campaign.type) ? 'the campaign type does not send immediately' : this.input.expectedListId && campaign.listId !== this.input.expectedListId ? 'the campaign targets another audience than expectedListId' : '';
    // A campaign canceled or changed after a possible earlier send may have partly sent.
    if (blocker) return this.refuseOrSelectUncertain('notSendable', output, mailchimpFailure(operation, 'CONFLICT', `${blocker}; nothing was sent in this attempt`), readReceipt);
    return this.send(connection, output);
  }

  private async send(connection: { key: string; baseURL: string }, output: SendCampaignOutput): Promise<MutationAttempt<SendCampaignOutput>> {
    const operation = 'sendCampaign';
    let sent: MailchimpExchange;
    try { sent = await this.fence.dispatch(signal => this.client.exchange(this.call, connection, operation, 'POST', `/campaigns/${this.input.campaignId}/actions/send`, undefined, undefined, signal)); }
    catch (error) {
      if (error instanceof CheckpointError) return newMutationRetry(mailchimpFailure(operation, 'AVAILABILITY', 'Dex did not record the dispatch checkpoint; nothing was sent in this attempt'));
      return newMutationRetry(mailchimpFailure(operation, 'AVAILABILITY', 'too little of the Step deadline remained to send; nothing was sent in this attempt'));
    }
    const receipt = this.client.receipt(this.call, sent, this.input.campaignId);
    if (sent.outcome === 'succeeded') return newMutationBranch('sent', output, receipt);
    if (sent.outcome === 'rateLimited' || sent.outcome === 'notSent' || (sent.outcome === 'unavailable' && sent.status === 503)) {
      try { await this.fence.recordCanResend(); }
      catch { return newMutationRetry(mailchimpFailure(operation, 'AVAILABILITY', 'the Mailchimp retry checkpoint could not be recorded')); }
      return newMutationRetry(sent.failure!, sent.retryAfterMs);
    }
    if (isOutcomeUnknownAfterDispatch(sent)) {
      // The claim stays the last marker, so the next attempt counts this send and reads first.
      return this.canResendUnknownOutcome ? newMutationRetry(sent.failure!, sent.retryAfterMs) : newMutationUncertain(output, sent.failure!, receipt);
    }
    // A refused resend usually means an earlier send landed, so the campaign is read instead.
    if (this.hasPossibleEarlierSend) return readBackWithoutSend(this.client, this.call, this.input, 'Mailchimp refused a resend after a possible earlier send');
    try { await this.fence.recordCanResend(); } catch { /* The refusal proves nothing was sent; a lost checkpoint only makes a replay read first. */ }
    return newMutationBranch(sent.outcome === 'notFound' ? 'notFound' : sent.outcome === 'defect' ? 'defect' : 'providerRejected', output, receipt, sent.failure!);
  }

  /** Record notSent before a pre-send Retry so the next attempt does not count a send. */
  private async retryBeforeSend(failure: Failure, retryAfterMs: number): Promise<MutationAttempt<SendCampaignOutput>> {
    // After readBackThenResendAllowed, notSent would hide the earlier possible send.
    if (this.decisionKind === 'sendAllowed') {
      try { await this.fence.recordNotSent(); }
      catch { return newMutationRetry(mailchimpFailure('sendCampaign', 'AVAILABILITY', 'the no-send checkpoint could not be recorded; nothing was sent in this attempt')); }
    }
    return newMutationRetry(failure, retryAfterMs);
  }

  /** Select branch only when no earlier attempt may have sent the campaign. */
  private refuseOrSelectUncertain(branch: string, output: SendCampaignOutput, refusal: Failure, receipt: ReceiptDraft): MutationAttempt<SendCampaignOutput> {
    if (this.hasPossibleEarlierSend) return newMutationUncertain(output, mailchimpFailure('sendCampaign', refusal.kind, `an earlier attempt may have sent the campaign and Mailchimp does not show it sending or sent: ${refusal.message}`), receipt);
    return newMutationBranch(branch, output, receipt, refusal);
  }
}

/** Report a pre-fence checkpoint of this call; such a Step execution never sends again. */
function hasLegacyDispatch(call: Call): boolean {
  try {
    if (!call.context.hasLastHeartbeatValue()) return false;
    const previous = call.context.getLastHeartbeatValue() as { mailchimpDispatchedCallId?: unknown } | null | undefined;
    return typeof previous === 'object' && previous !== null && previous.mailchimpDispatchedCallId === call.id;
  } catch { return false; }
}

/** Never send; report alreadySent only when Mailchimp shows the campaign sending or sent. */
async function readBackWithoutSend(client: MailchimpClient, call: Call, input: SendCampaignInput, reason: string): Promise<MutationAttempt<SendCampaignOutput>> {
  const operation = 'sendCampaign';
  const connection = await client.resolveConnection(call);
  if (!connection) return newMutationUncertain(emptySend, mailchimpFailure(operation, 'AUTHENTICATION', `${reason}, and the connection credentials are unavailable to read the campaign; reconnect the connection`), zeroReceipt());
  const read = await client.exchange(call, connection, operation, 'GET', `/campaigns/${input.campaignId}`, new URLSearchParams({ fields: campaignFields }));
  const receipt = client.receipt(call, read, input.campaignId);
  // Reading is harmless to repeat, and a retry of this path never sends.
  if (read.outcome === 'rateLimited' || read.outcome === 'notSent' || read.outcome === 'unavailable') return newMutationRetry(read.failure!, read.retryAfterMs);
  if (read.outcome === 'succeeded') {
    let campaign: Campaign | undefined;
    try { campaign = decodeCampaign(read.body, input.campaignId); } catch { /* An undecodable campaign cannot confirm the earlier send. */ }
    if (campaign) {
      if (campaign.status === 'sending' || campaign.status === 'sent') return newMutationBranch('alreadySent', { campaign }, receipt);
      return newMutationUncertain({ campaign }, mailchimpFailure(operation, 'TRANSPORT', `${reason} and Mailchimp does not show it sending or sent, so it is not sent again`), receipt);
    }
  }
  return newMutationUncertain(emptySend, mailchimpFailure(operation, 'TRANSPORT', `${reason} and its status could not be read, so it is not sent again`), receipt);
}

/** The Fence's own rule: a cap of 1 never resends, and otherwise an application override wins. */
function effectiveMaxSends(call: Call, operationMaxSends: number): number {
  return operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
}

/** Map a definite read failure before a send; nothing was sent by this attempt. */
function campaignReadBranch(outcome: MailchimpExchange['outcome']): string {
  return outcome === 'notFound' ? 'notFound' : outcome === 'invalid' ? 'invalidResponse' : outcome === 'defect' ? 'defect' : 'providerRejected';
}

/** Statuses that prove a write was refused; 409, 3xx, and other statuses are unknown. */
const definiteRefusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

/** Report an outcome that does not prove Mailchimp refused or skipped a write. */
function isOutcomeUnknownAfterDispatch(exchange: MailchimpExchange): boolean {
  if (['rateLimited', 'notSent', 'unavailable', 'invalid'].includes(exchange.outcome)) return true;
  return exchange.outcome === 'rejected' && !definiteRefusalStatuses.has(exchange.status);
}

function queryExchange<T>(exchange: MailchimpExchange, receipt: ReceiptDraft, zero: T, hasNotFound: boolean): QueryAttempt<T> | undefined {
  if (exchange.outcome === 'succeeded') return undefined;
  if (['rateLimited', 'notSent', 'unavailable'].includes(exchange.outcome)) return newQueryRetry(exchange.failure!, exchange.retryAfterMs);
  const branch = exchange.outcome === 'notFound' && hasNotFound ? 'notFound' : exchange.outcome === 'invalid' ? 'invalidResponse' : exchange.outcome === 'defect' ? 'defect' : 'providerRejected';
  return newQueryBranch(branch, zero, receipt, exchange.failure!);
}
function mutationExchange<T>(exchange: MailchimpExchange, receipt: ReceiptDraft, zero: T, hasNotFound: boolean): MutationAttempt<T> | undefined {
  if (exchange.outcome === 'succeeded') return undefined;
  // A status that is not a definite refusal is retried; an unusable 2xx keeps invalidResponse.
  if (exchange.outcome !== 'invalid' && isOutcomeUnknownAfterDispatch(exchange)) return newMutationRetry(exchange.failure!, exchange.retryAfterMs);
  const branch = exchange.outcome === 'notFound' && hasNotFound ? 'notFound' : exchange.outcome === 'invalid' ? 'invalidResponse' : exchange.outcome === 'defect' ? 'defect' : 'providerRejected';
  return newMutationBranch(branch, zero, receipt, exchange.failure!);
}
function credentialQueryDefect<T>(operation: string, zero: T): QueryAttempt<T> { return newQueryBranch('defect', zero, zeroReceipt(), mailchimpFailure(operation, 'AUTHENTICATION', 'connection credentials are unavailable')); }
function credentialMutationDefect<T>(operation: string, zero: T): MutationAttempt<T> { return newMutationBranch('defect', zero, zeroReceipt(), mailchimpFailure(operation, 'AUTHENTICATION', 'connection credentials are unavailable')); }
function validateResourceID(name: string, value: string): string { return /^[A-Za-z0-9]{1,64}$/.test(value) ? '' : `${name} must be a Mailchimp ID of 1 to 64 letters and digits`; }
function validateEmail(value: string): string {
  if (!value || Buffer.byteLength(value) > 254) return 'emailAddress must be one email address of at most 254 bytes';
  if (!isGoBareEmailAddress(value)) return 'emailAddress must be one bare email address such as jane@example.com';
  return '';
}
function validateMemberAddress(listID: string, email: string): string { return validateResourceID('listId', listID) || validateEmail(email); }
function subscriberHash(email: string): string { return createHash('md5').update(goSimpleLower(email), 'utf8').digest('hex'); }
function memberPath(listID: string, hash: string): string { return `/lists/${listID}/members/${hash}`; }
function validateListInput(input: ListMembersInput): string {
  if (validateResourceID('listId', input.listId)) return validateResourceID('listId', input.listId);
  if (input.status && !readableStatuses.has(input.status)) return 'status is not a Mailchimp contact status';
  if (input.pageSize !== undefined && (!Number.isInteger(input.pageSize) || input.pageSize < 0 || input.pageSize > 1000)) return 'pageSize must be 1 to 1000, or zero for 100';
  if (input.offset !== undefined && (!Number.isInteger(input.offset) || input.offset < 0 || input.offset > 2_147_483_647)) return 'offset must be a non-negative number of contacts';
  for (const bound of [input.changedSince, input.optedInSince]) {
    if (!bound) continue;
    try { if (timestamp(bound) === '0001-01-01T00:00:00Z') return 'time bound is invalid'; } catch { return 'time bound is invalid'; }
  }
  return '';
}
function formatMailchimpTime(value: string): string { return timestamp(value)!.slice(0, 19) + '+00:00'; }
function validateUpsertInput(input: UpsertMemberInput): string {
  const addressError = validateMemberAddress(input.listId, input.emailAddress); if (addressError) return addressError;
  if (!writableStatuses.has(input.statusIfNew)) return 'statusIfNew is required';
  if (input.status && !writableStatuses.has(input.status)) return 'status is invalid';
  if ((input.status === 'subscribed' || input.status === 'pending') && !input.isResubscribeAllowed) return 'isResubscribeAllowed is required';
  if (input.mergeFields) {
    if (Object.keys(input.mergeFields).length > 30) return 'mergeFields holds at most 30 fields';
    for (const [tag, value] of Object.entries(input.mergeFields)) {
      if (!/^[A-Z0-9_]{1,50}$/.test(tag)) return `merge tag ${tag} is invalid`;
      const validText = (text: string) => Buffer.byteLength(text) <= 1024 && !/[\u0000-\u0008\u000b-\u001f\u007f-\u009f]/.test(text); // eslint-disable-line no-control-regex
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
    if (!tag || Buffer.byteLength(tag) > 100 || goTrimSpace(tag) !== tag || /[\u0000-\u001f\u007f-\u009f]/.test(tag)) return 'tag name is invalid'; // eslint-disable-line no-control-regex
    const lower = goSimpleLower(tag); if (seen.has(lower)) return 'tag appears more than once'; seen.add(lower);
  }
  return '';
}
export function parseObject(body: Uint8Array): Record<string, unknown> {
  const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('invalid object');
  return value as Record<string, unknown>;
}
function text(object: Record<string, unknown>, key: string): string {
  const value = object[key];
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw new Error(`invalid ${key}`);
  return value;
}
function number(object: Record<string, unknown>, key: string): number {
  const value = object[key];
  if (value === undefined || value === null) return 0;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new Error(`invalid ${key}`);
  return value;
}
function boolean(object: Record<string, unknown>, key: string): boolean {
  const value = object[key];
  if (value === undefined || value === null) return false;
  if (typeof value !== 'boolean') throw new Error(`invalid ${key}`);
  return value;
}
function object(object: Record<string, unknown>, key: string): Record<string, unknown> {
  const value = object[key];
  if (value === undefined || value === null) return {};
  if (typeof value !== 'object' || Array.isArray(value)) throw new Error(`invalid ${key}`);
  return value as Record<string, unknown>;
}
function timestamp(value: string): string | undefined { return value ? goRFC3339UTC(value) : undefined; }
function decodeMember(body: Uint8Array, hash: string): Member { return decodeMemberObject(parseObject(body), hash); }
export function decodeMemberObject(value: unknown, expectedHash: string): Member {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('invalid contact');
  const wire = value as Record<string, unknown>; const id = text(wire, 'id');
  if (!/^[0-9a-f]{32}$/.test(id) || expectedHash && id !== expectedHash || !text(wire, 'email_address') || !text(wire, 'status')) throw new Error('invalid contact');
  if (wire.tags !== undefined && wire.tags !== null && !Array.isArray(wire.tags)) throw new Error('invalid tags');
  const tags = Array.isArray(wire.tags) ? wire.tags.map(value => text(object({ tag: value }, 'tag'), 'name')) : [];
  const mergeFields = wire.merge_fields === undefined || wire.merge_fields === null ? undefined : object(wire, 'merge_fields');
  return { subscriberHash: id, emailAddress: text(wire, 'email_address'), status: text(wire, 'status'), tagCount: number(wire, 'tags_count'), listId: text(wire, 'list_id'),
    ...(text(wire, 'contact_id') ? { contactId: text(wire, 'contact_id') } : {}), ...(text(wire, 'full_name') ? { fullName: text(wire, 'full_name') } : {}),
    ...(text(wire, 'email_type') ? { emailType: text(wire, 'email_type') } : {}), ...(mergeFields && Object.keys(mergeFields).length ? { mergeFields } : {}),
    ...(tags.length ? { tags } : {}), ...(boolean(wire, 'vip') ? { isVip: true } : {}), ...(text(wire, 'language') ? { language: text(wire, 'language') } : {}),
    ...(number(wire, 'web_id') ? { webId: number(wire, 'web_id') } : {}),
    ...(timestamp(text(wire, 'timestamp_signup')) ? { signedUpAt: timestamp(text(wire, 'timestamp_signup')) } : {}),
    ...(timestamp(text(wire, 'timestamp_opt')) ? { optedInAt: timestamp(text(wire, 'timestamp_opt')) } : {}),
    ...(timestamp(text(wire, 'last_changed')) ? { lastChangedAt: timestamp(text(wire, 'last_changed')) } : {}) };
}
function decodeCampaign(body: Uint8Array, expectedID: string): Campaign {
  const wire = parseObject(body); const settings = object(wire, 'settings');
  const recipients = object(wire, 'recipients');
  if (text(wire, 'id') !== expectedID || !text(wire, 'status') || !text(wire, 'type')) throw new Error('invalid campaign');
  return { id: expectedID, type: text(wire, 'type'), status: text(wire, 'status'), recipientCount: number(recipients, 'recipient_count'), emailsSent: number(wire, 'emails_sent'),
    ...(number(wire, 'web_id') ? { webId: number(wire, 'web_id') } : {}), ...(text(settings, 'title') ? { title: text(settings, 'title') } : {}),
    ...(text(settings, 'subject_line') ? { subjectLine: text(settings, 'subject_line') } : {}), ...(text(recipients, 'list_id') ? { listId: text(recipients, 'list_id') } : {}),
    ...(text(recipients, 'list_name') ? { listName: text(recipients, 'list_name') } : {}),
    ...(timestamp(text(wire, 'create_time')) ? { createdAt: timestamp(text(wire, 'create_time')) } : {}),
    ...(timestamp(text(wire, 'send_time')) ? { sentAt: timestamp(text(wire, 'send_time')) } : {}) };
}
