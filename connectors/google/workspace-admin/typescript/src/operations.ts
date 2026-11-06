// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { DirectoryOutcome, WorkspaceAdminClient } from './client.js';
import { goPathEscape } from './client.js';
import { AddUserToGroupDefinition, CreateUserDefinition, GetUserDefinition, ListUsersDefinition, RemoveUserFromGroupDefinition, SuspendUserDefinition, UnsuspendUserDefinition } from './generated/definitions.js';
import type { AddUserToGroupInput, AddUserToGroupOutput, CreateUserInput, CreateUserOutput, GetUserInput, GroupMembership, ListUsersInput, ListUsersOutput, RemoveUserFromGroupInput, RemoveUserFromGroupOutput, SuspendUserInput, UnsuspendUserInput, User } from './generated/models.js';

const provider = 'google-workspace-admin';
const zeroUser: User = { id: '', primaryEmail: '', name: { givenName: '', familyName: '' }, orgUnitPath: '', isSuspended: false, isArchived: false, isAdmin: false, isDelegatedAdmin: false, isEnrolledIn2Sv: false, isEnforcedIn2Sv: false, changePasswordAtNextLogin: false, isMailboxSetup: false };
const zeroCreate: CreateUserOutput = { user: zeroUser, wasAlreadyCreated: false };
const zeroMembership: GroupMembership = { groupKey: '', memberId: '', role: '' };
const zeroAdd: AddUserToGroupOutput = { membership: zeroMembership, wasAlreadyMember: false };
const zeroRemove: RemoveUserFromGroupOutput = { groupKey: '', memberKey: '', wasAlreadyRemoved: false };
const zeroList: ListUsersOutput = { users: [] };

/** Read one account from the Directory API. */
export class GetUserOperation implements Query<GetUserInput, User> {
  /** Bind a client. */ constructor(private readonly client: WorkspaceAdminClient) {}
  /** Return the manifest definition. */ definition() { return GetUserDefinition; }
  /** Validate the key and classify one account read. */
  async invoke(call: Call, input: GetUserInput): Promise<QueryAttempt<User>> {
    let key: string;
    try { key = validateUserKey(input.userKey); } catch (error) { return queryDefect('getUser', zeroUser, error); }
    const exchange = await this.client.exchange(call, 'getUser', 'GET', userPath(key));
    if ('outcome' in exchange) return queryOutcome(exchange.outcome, zeroUser, 'notFound');
    try { const { user } = decodeUser(exchange.response.body); return newQueryBranch('found', user, this.client.receipt(call, exchange.response, user.id)); }
    catch { return queryFailure('getUser', 'invalidResponse', 'PROTOCOL', zeroUser, exchange.receipt, 'provider returned an invalid account: account is invalid'); }
  }
}

/** List one bounded Directory user page. */
export class ListUsersOperation implements Query<ListUsersInput, ListUsersOutput> {
  /** Bind a client. */ constructor(private readonly client: WorkspaceAdminClient) {}
  /** Return the manifest definition. */ definition() { return ListUsersDefinition; }
  /** Validate selectors and decode one page. */
  async invoke(call: Call, input: ListUsersInput): Promise<QueryAttempt<ListUsersOutput>> {
    let query: Record<string, string>;
    try { query = buildListQuery(input, this.client.listUsersPageSize); } catch (error) { return queryDefect('listUsers', zeroList, error); }
    const exchange = await this.client.exchange(call, 'listUsers', 'GET', '/users', query);
    if ('outcome' in exchange) return queryOutcome(exchange.outcome, zeroList);
    try {
      const raw = decodeObject(exchange.response.body);
      const usersValue = raw.users;
      if (usersValue !== undefined && usersValue !== null && !Array.isArray(usersValue)) throw new Error('invalid users');
      const users = (usersValue ?? []).map((item: unknown) => convertUser(requireObject(item)).user);
      const nextPageToken = optionalString(raw, 'nextPageToken');
      return newQueryBranch('listed', { users, ...(nextPageToken ? { nextPageToken } : {}) }, exchange.receipt);
    } catch { return queryFailure('listUsers', 'invalidResponse', 'PROTOCOL', zeroList, exchange.receipt, 'provider returned an invalid page: page is invalid'); }
  }
}

/** Create an account with a stable external creation key. */
export class CreateUserOperation implements Mutation<CreateUserInput, CreateUserOutput> {
  /** Bind a client. */ constructor(private readonly client: WorkspaceAdminClient) {}
  /** Return the manifest definition. */ definition() { return CreateUserDefinition; }
  /** Use a declared provisioning key or the Call ID. */ idempotencyKey(callId: string, input: CreateUserInput) { return input.provisioningKey?.trim() || callId; }
  /** Insert and converge by reading the account after a conflict. */
  async invoke(call: Call, input: CreateUserInput): Promise<MutationAttempt<CreateUserOutput>> {
    let payload: Record<string, unknown>;
    try { payload = buildCreateRequest(input, call.idempotencyKey || this.idempotencyKey(call.id, input), this.client.randomBytes); }
    catch (error) { return mutationDefect('createUser', zeroCreate, error); }
    const email = String(payload.primaryEmail);
    const creationKey = call.idempotencyKey || this.idempotencyKey(call.id, input);
    const exchange = await this.client.exchange(call, 'createUser', 'POST', '/users', undefined, payload);
    if ('response' in exchange) {
      try { const { user } = decodeUser(exchange.response.body); if (user.primaryEmail.toLowerCase() !== email.toLowerCase()) throw new Error('address mismatch'); return newMutationBranch('created', { user, wasAlreadyCreated: false }, this.client.receipt(call, exchange.response, user.id)); }
      catch { return newMutationRetry(failure('createUser', 'PROTOCOL', 'insert response is unusable; the retry reads the account back')); }
    }
    if (exchange.outcome.kind === 'conflict') {
      const read = await this.client.exchange(call, 'createUser', 'GET', userPath(email));
      if ('response' in read) {
        try {
          const { user, resource } = decodeUser(read.response.body);
          const receipt = this.client.receipt(call, read.response, user.id);
          if (hasCreationKey(resource, creationKey)) return newMutationBranch('created', { user, wasAlreadyCreated: true }, receipt);
          return newMutationBranch('alreadyExists', { user, wasAlreadyCreated: false }, receipt, failure('createUser', 'CONFLICT', 'address belongs to an account this Step did not create'));
        } catch { return mutationFailure('createUser', 'invalidResponse', 'PROTOCOL', zeroCreate, read.receipt, 'provider returned an invalid account: account is invalid'); }
      }
      if (read.outcome.kind === 'notFound') {
        const firstAttemptAt = call.context.firstAttemptAt;
        if (firstAttemptAt && this.client.now().getTime() - new Date(firstAttemptAt).getTime() < 60_000) return newMutationRetry(failure('createUser', 'AVAILABILITY', 'Google reports the address as taken but does not return the account yet'));
        return mutationFailure('createUser', 'alreadyExists', 'CONFLICT', zeroCreate, read.outcome.receipt, 'address belongs to a group or an account outside the directory');
      }
      return mutationOutcome(read.outcome, zeroCreate, 'providerRejected');
    }
    if (exchange.outcome.kind === 'invalidResponse') return newMutationRetry(failure('createUser', exchange.outcome.failure.kind, 'insert response is unusable; the retry reads the account back'));
    return mutationOutcome(exchange.outcome, zeroCreate, 'providerRejected');
  }
}

/** Set the suspended flag to true. */
export class SuspendUserOperation implements Mutation<SuspendUserInput, User> {
  /** Bind a client. */ constructor(private readonly client: WorkspaceAdminClient) {}
  /** Return the manifest definition. */ definition() { return SuspendUserDefinition; }
  /** Absolute assignment is safe to repeat. */ idempotencyKey(callId: string) { return callId; }
  /** Apply one suspension. */ invoke(call: Call, input: SuspendUserInput) { return setSuspension(this.client, call, input.userKey, true); }
}

/** Set the suspended flag to false. */
export class UnsuspendUserOperation implements Mutation<UnsuspendUserInput, User> {
  /** Bind a client. */ constructor(private readonly client: WorkspaceAdminClient) {}
  /** Return the manifest definition. */ definition() { return UnsuspendUserDefinition; }
  /** Absolute assignment is safe to repeat. */ idempotencyKey(callId: string) { return callId; }
  /** Restore one account. */ invoke(call: Call, input: UnsuspendUserInput) { return setSuspension(this.client, call, input.userKey, false); }
}

async function setSuspension(client: WorkspaceAdminClient, call: Call, rawKey: string, suspended: boolean): Promise<MutationAttempt<User>> {
  const operation = suspended ? 'suspendUser' : 'unsuspendUser';
  let key: string;
  try { key = validateUserKey(rawKey); } catch (error) { return mutationDefect(operation, zeroUser, error); }
  const exchange = await client.exchange(call, operation, 'PUT', userPath(key), undefined, { suspended });
  if ('outcome' in exchange) return mutationOutcome(exchange.outcome, zeroUser, 'providerRejected', 'notFound');
  try {
    const { user } = decodeUser(exchange.response.body);
    const receipt = client.receipt(call, exchange.response, user.id);
    if (user.isSuspended !== suspended) return newMutationBranch('providerRejected', user, receipt, failure(operation, 'PROVIDER_REJECTION', 'Google returned the account without the requested suspension state'));
    return newMutationBranch(suspended ? 'suspended' : 'unsuspended', user, receipt);
  } catch { return mutationFailure(operation, 'invalidResponse', 'PROTOCOL', zeroUser, exchange.receipt, 'provider returned an invalid account: account is invalid'); }
}

/** Insert one direct group membership. */
export class AddUserToGroupOperation implements Mutation<AddUserToGroupInput, AddUserToGroupOutput> {
  /** Bind a client. */ constructor(private readonly client: WorkspaceAdminClient) {}
  /** Return the manifest definition. */ definition() { return AddUserToGroupDefinition; }
  /** Direct membership is unique per account and group. */ idempotencyKey(callId: string) { return callId; }
  /** Insert or converge on the existing membership. */
  async invoke(call: Call, input: AddUserToGroupInput): Promise<MutationAttempt<AddUserToGroupOutput>> {
    let groupKey: string, email: string, role: string;
    try { groupKey = validateGroupKey(input.groupKey); email = validateBareEmail(input.memberEmail.trim(), 'memberEmail must be one bare email address'); role = input.role || 'MEMBER'; if (!isRole(role)) throw new Error('role must be MEMBER, MANAGER, or OWNER'); }
    catch (error) { return mutationDefect('addUserToGroup', zeroAdd, error); }
    const exchange = await this.client.exchange(call, 'addUserToGroup', 'POST', groupMembersPath(groupKey), undefined, { email, role });
    if ('response' in exchange) {
      try { const membership = decodeMembership(exchange.response.body, groupKey); return newMutationBranch('added', { membership, wasAlreadyMember: false }, this.client.receipt(call, exchange.response, membership.memberId)); }
      catch { return newMutationRetry(failure('addUserToGroup', 'PROTOCOL', 'insert response is unusable; the retry reads the membership back')); }
    }
    if (exchange.outcome.kind === 'conflict') {
      const read = await this.client.exchange(call, 'addUserToGroup', 'GET', groupMemberPath(groupKey, email));
      if ('response' in read) {
        try { const membership = decodeMembership(read.response.body, groupKey); return newMutationBranch('added', { membership, wasAlreadyMember: true }, this.client.receipt(call, read.response, membership.memberId)); }
        catch { return mutationFailure('addUserToGroup', 'invalidResponse', 'PROTOCOL', zeroAdd, read.receipt, 'provider returned an invalid membership: membership is invalid'); }
      }
      if (read.outcome.kind === 'notFound') return newMutationRetry(failure('addUserToGroup', 'AVAILABILITY', 'Google reports the membership as existing but does not return it yet'));
      return mutationOutcome(read.outcome, zeroAdd, 'providerRejected', 'notFound');
    }
    if (exchange.outcome.kind === 'invalidResponse') return newMutationRetry(failure('addUserToGroup', exchange.outcome.failure.kind, 'insert response is unusable; the retry reads the membership back'));
    return mutationOutcome(exchange.outcome, zeroAdd, 'providerRejected', 'notFound');
  }
}

/** Delete one direct group membership. */
export class RemoveUserFromGroupOperation implements Mutation<RemoveUserFromGroupInput, RemoveUserFromGroupOutput> {
  /** Bind a client. */ constructor(private readonly client: WorkspaceAdminClient) {}
  /** Return the manifest definition. */ definition() { return RemoveUserFromGroupDefinition; }
  /** Deletion is safe to repeat. */ idempotencyKey(callId: string) { return callId; }
  /** Delete and confirm that a 404 refers to an existing group. */
  async invoke(call: Call, input: RemoveUserFromGroupInput): Promise<MutationAttempt<RemoveUserFromGroupOutput>> {
    let groupKey: string, memberKey: string;
    try { groupKey = validateGroupKey(input.groupKey); memberKey = validateUserKey(input.memberKey); }
    catch (error) { return mutationDefect('removeUserFromGroup', zeroRemove, error); }
    const output = { groupKey, memberKey, wasAlreadyRemoved: false };
    const exchange = await this.client.exchange(call, 'removeUserFromGroup', 'DELETE', groupMemberPath(groupKey, memberKey));
    if ('response' in exchange) return newMutationBranch('removed', output, exchange.receipt);
    if (exchange.outcome.kind === 'invalidResponse') return newMutationRetry(failure('removeUserFromGroup', exchange.outcome.failure.kind, 'delete response is unusable; the retry confirms the removal'));
    if (exchange.outcome.kind === 'notFound') {
      const check = await this.client.exchange(call, 'removeUserFromGroup', 'GET', groupMembersPath(groupKey), { maxResults: '1' });
      if ('response' in check) return newMutationBranch('removed', { ...output, wasAlreadyRemoved: true }, check.receipt);
      return mutationOutcome(check.outcome, zeroRemove, 'providerRejected', 'notFound');
    }
    return mutationOutcome(exchange.outcome, zeroRemove, 'providerRejected', 'notFound');
  }
}

function queryOutcome<T>(outcome: DirectoryOutcome, zero: T, notFoundBranch = 'providerRejected'): QueryAttempt<T> {
  if (outcome.kind === 'retry') return newQueryRetry(outcome.failure, outcome.retryAfterMs);
  return newQueryBranch(outcome.kind === 'notFound' ? notFoundBranch : outcome.kind === 'conflict' || outcome.kind === 'rejected' ? 'providerRejected' : outcome.kind, zero, outcome.receipt, outcome.failure);
}
function mutationOutcome<T>(outcome: DirectoryOutcome, zero: T, rejectedBranch: string, notFoundBranch = rejectedBranch): MutationAttempt<T> {
  if (outcome.kind === 'retry') return newMutationRetry(outcome.failure, outcome.retryAfterMs);
  return newMutationBranch(outcome.kind === 'notFound' ? notFoundBranch : outcome.kind === 'conflict' || outcome.kind === 'rejected' ? rejectedBranch : outcome.kind, zero, outcome.receipt, outcome.failure);
}
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider, operation, message }; }
function queryDefect<T>(operation: string, zero: T, error: unknown): QueryAttempt<T> { return queryFailure(operation, 'defect', 'VALIDATION', zero, {}, error instanceof Error ? error.message : 'invalid input'); }
function mutationDefect<T>(operation: string, zero: T, error: unknown): MutationAttempt<T> { return mutationFailure(operation, 'defect', 'VALIDATION', zero, {}, error instanceof Error ? error.message : 'invalid input'); }
function queryFailure<T>(operation: string, branch: string, kind: Failure['kind'], zero: T, receipt: ReceiptDraft, message: string): QueryAttempt<T> { return newQueryBranch(branch, zero, receipt, failure(operation, kind, message)); }
function mutationFailure<T>(operation: string, branch: string, kind: Failure['kind'], zero: T, receipt: ReceiptDraft, message: string): MutationAttempt<T> { return newMutationBranch(branch, zero, receipt, failure(operation, kind, message)); }

function requireObject(value: unknown): Record<string, unknown> { if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new Error('invalid object'); return value as Record<string, unknown>; }
function decodeObject(body: Uint8Array): Record<string, unknown> { return requireObject(JSON.parse(Buffer.from(body).toString('utf8')) as unknown); }
function optionalString(value: Record<string, unknown>, key: string): string { const item = value[key]; if (item === null || item === undefined) return ''; if (typeof item !== 'string') throw new Error('invalid string'); return item; }
function optionalBool(value: Record<string, unknown>, key: string): boolean { const item = value[key]; if (item === null || item === undefined) return false; if (typeof item !== 'boolean') throw new Error('invalid boolean'); return item; }
function convertUser(resource: Record<string, unknown>): { user: User; resource: Record<string, unknown> } {
  const id = optionalString(resource, 'id'), primaryEmail = optionalString(resource, 'primaryEmail');
  if (!/^[0-9]{1,64}$/.test(id)) throw new Error('account lacks a valid unique ID');
  validateBareEmail(primaryEmail, 'account lacks a valid primary address');
  const rawName = resource.name == null ? {} : requireObject(resource.name);
  const fullName = optionalString(rawName, 'fullName'), suspensionReason = optionalString(resource, 'suspensionReason');
  const aliases = resource.aliases == null ? [] : resource.aliases;
  if (!Array.isArray(aliases) || !aliases.every(item => typeof item === 'string')) throw new Error('invalid aliases');
  const user: User = { id, primaryEmail, name: { givenName: optionalString(rawName, 'givenName'), familyName: optionalString(rawName, 'familyName'), ...(fullName ? { fullName } : {}) },
    orgUnitPath: optionalString(resource, 'orgUnitPath'), isSuspended: optionalBool(resource, 'suspended'), ...(suspensionReason ? { suspensionReason } : {}),
    isArchived: optionalBool(resource, 'archived'), isAdmin: optionalBool(resource, 'isAdmin'), isDelegatedAdmin: optionalBool(resource, 'isDelegatedAdmin'), isEnrolledIn2Sv: optionalBool(resource, 'isEnrolledIn2Sv'), isEnforcedIn2Sv: optionalBool(resource, 'isEnforcedIn2Sv'), changePasswordAtNextLogin: optionalBool(resource, 'changePasswordAtNextLogin'), isMailboxSetup: optionalBool(resource, 'isMailboxSetup'),
    ...(aliases.length ? { aliases } : {}), ...optionalNonempty(resource, 'customerId'), ...optionalNonempty(resource, 'creationTime'), ...optionalNonempty(resource, 'lastLoginTime') };
  return { user, resource };
}
function optionalNonempty(resource: Record<string, unknown>, key: 'customerId' | 'creationTime' | 'lastLoginTime'): Record<string, string> { const item = optionalString(resource, key); return item ? { [key]: item } : {}; }
function decodeUser(body: Uint8Array) { return convertUser(decodeObject(body)); }
function isRole(role: string): boolean { return role === 'MEMBER' || role === 'MANAGER' || role === 'OWNER'; }
function decodeMembership(body: Uint8Array, groupKey: string): GroupMembership {
  const resource = decodeObject(body), id = optionalString(resource, 'id'), role = optionalString(resource, 'role');
  if (!id || Buffer.byteLength(id) > 64 || !isRole(role)) throw new Error('membership lacks a valid member ID or role');
  return { groupKey, memberId: id, role, ...optionalNonemptyMembership(resource, 'email'), ...optionalNonemptyMembership(resource, 'type'), ...optionalNonemptyMembership(resource, 'status') };
}
function optionalNonemptyMembership(resource: Record<string, unknown>, key: 'email' | 'type' | 'status'): Record<string, string> { const item = optionalString(resource, key); return item ? { [key]: item } : {}; }
function hasCreationKey(resource: Record<string, unknown>, key: string): boolean { return Array.isArray(resource.externalIds) && resource.externalIds.some(item => { try { const external = requireObject(item); return external.type === 'custom' && external.customType === 'dexIdempotencyKey' && external.value === key; } catch { return false; } }); }

function validateBareEmail(value: string, message: string): string {
  if (!value || Buffer.byteLength(value) > 254 || [...'<>"()[]\\,; \t\r\n/?#%'].some(character => value.includes(character)) || (value.match(/@/g) ?? []).length !== 1) throw new Error(message);
  const [local, domain] = value.split('@');
  const atom = /^[A-Za-z0-9!$&'*+\-=^_`{|}~\u0080-\u{10ffff}]+$/u;
  if (!local || !domain || !local.split('.').every(part => part && atom.test(part)) || !domain.split('.').every(part => part && atom.test(part))) throw new Error(message);
  return value;
}
function validateUserKey(value: string): string { const key = value.trim(); if (/^[0-9]{1,64}$/.test(key)) return key; return validateBareEmail(key, 'userKey must be one bare email address or a numeric user ID'); }
function validateGroupKey(value: string): string { const key = value.trim(); if (/^[0-9a-z]{1,64}$/.test(key)) return key; return validateBareEmail(key, 'groupKey must be one bare group email address or a group ID'); }
function userPath(key: string): string { return '/users/' + goPathEscape(key); }
function groupMembersPath(key: string): string { return '/groups/' + goPathEscape(key) + '/members'; }
function groupMemberPath(group: string, member: string): string { return groupMembersPath(group) + '/' + goPathEscape(member); }

function buildListQuery(input: ListUsersInput, defaultSize: number): Record<string, string> {
  const domain = input.domain?.trim() ?? '', customer = input.customer?.trim() ?? '';
  if (domain && customer) throw new Error('set domain or customer, not both');
  if (domain && (Buffer.byteLength(domain) > 253 || !/^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$/.test(domain))) throw new Error('domain must be a domain name such as example.com');
  if (customer && customer !== 'my_customer' && !/^C[0-9A-Za-z]{1,32}$/.test(customer)) throw new Error('customer must be a customer ID such as C03az79cb or my_customer');
  const query: Record<string, string> = domain ? { domain } : { customer: customer || 'my_customer' };
  const search = input.query?.trim() ?? '';
  if (search) { if (Buffer.byteLength(search) > 2048 || hasControlCharacter(search)) throw new Error('query must be at most 2048 bytes without control characters'); query.query = search; }
  const orderBy = input.orderBy?.trim() || 'email';
  if (!['email', 'givenName', 'familyName'].includes(orderBy)) throw new Error('orderBy must be email, givenName, or familyName');
  query.orderBy = orderBy;
  const pageSize = input.pageSize || defaultSize;
  if (!Number.isInteger(pageSize) || pageSize < 1 || pageSize > 500) throw new Error('pageSize must be from 1 to 500');
  query.maxResults = String(pageSize);
  const pageToken = input.pageToken?.trim() ?? '';
  if (pageToken) { if (Buffer.byteLength(pageToken) > 4096 || !/^[A-Za-z0-9._~+/=-]+$/.test(pageToken)) throw new Error('pageToken must be the NextPageToken of a previous page'); query.pageToken = pageToken; }
  return query;
}
function buildCreateRequest(input: CreateUserInput, creationKey: string, randomSource: (length: number) => Uint8Array): Record<string, unknown> {
  const primaryEmail = validateBareEmail(input.primaryEmail.trim(), 'primaryEmail must be one bare email address');
  if (!/^[A-Za-z0-9._:-]{1,128}$/.test(creationKey)) throw new Error('provisioningKey must be 1 to 128 letters, digits, or . _ : - characters');
  const givenName = validateName(input.givenName, 'givenName'), familyName = validateName(input.familyName, 'familyName');
  const orgUnitPath = input.orgUnitPath?.trim() || '/';
  if (!orgUnitPath.startsWith('/') || Buffer.byteLength(orgUnitPath) > 1024 || hasControlCharacter(orgUnitPath)) throw new Error('orgUnitPath must be a full path such as /Engineering');
  const recoveryEmail = input.recoveryEmail?.trim() ?? '';
  if (recoveryEmail) validateBareEmail(recoveryEmail, 'recoveryEmail must be one bare email address');
  const recoveryPhone = input.recoveryPhone?.trim() ?? '';
  if (recoveryPhone && !/^\+[1-9][0-9]{1,14}$/.test(recoveryPhone)) throw new Error('recoveryPhone must be an E.164 number such as +16506661212');
  const passwordBytes = randomSource(32);
  if (passwordBytes.length !== 32) throw new Error('initial password could not be generated');
  const password = Buffer.from(passwordBytes).toString('base64url');
  return { primaryEmail, name: { givenName, familyName }, password, changePasswordAtNextLogin: true, orgUnitPath, externalIds: [{ type: 'custom', customType: 'dexIdempotencyKey', value: creationKey }], ...(recoveryEmail ? { recoveryEmail } : {}), ...(recoveryPhone ? { recoveryPhone } : {}) };
}
function validateName(value: string, field: string): string { const name = value.trim(); if (!name) throw new Error(`${field} is required`); if ([...name].length > 60) throw new Error(`${field} must be valid text of at most 60 characters`); if (/\p{Cc}/u.test(name)) throw new Error(`${field} cannot contain control characters`); return name; }
function hasControlCharacter(value: string): boolean { return [...value].some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127); }
