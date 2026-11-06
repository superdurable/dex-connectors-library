// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomInt } from 'node:crypto';
import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type MutationAttempt, type QueryAttempt } from '@superdurable/connectors-sdk';
import { readErrorTokens } from '@superdurable/connectors-sdk/providerhttp';
import { AddUserToGroupBranch, AddUserToGroupDefinition, CreateUserBranch, CreateUserDefinition, DisableUserBranch, DisableUserDefinition, EnableUserBranch, EnableUserDefinition, GetUserBranch, GetUserDefinition, ListUsersBranch, ListUsersDefinition, RemoveUserFromGroupBranch, RemoveUserFromGroupDefinition, RevokeSignInSessionsBranch, RevokeSignInSessionsDefinition } from './generated/definitions.js';
import type { AddUserToGroupInput, AddUserToGroupOutput, CreateUserInput, CreateUserOutput, Credentials, DisableUserInput, EnableUserInput, GetUserInput, ListUsersInput, ListUsersOutput, RemoveUserFromGroupInput, RemoveUserFromGroupOutput, RevokeSignInSessionsInput, RevokeSignInSessionsOutput, User } from './generated/models.js';
import { EntraClient, userPath, type GraphExchange, type GraphRequest } from './client.js';

const userSelect = 'id,userPrincipalName,displayName,givenName,surname,mail,mailNickname,accountEnabled,userType,jobTitle,department,usageLocation,onPremisesSyncEnabled,createdDateTime,signInSessionsValidFromDateTime';
const creationSelect = `${userSelect},onPremisesExtensionAttributes`;
const guid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const lookupUPN = /^[A-Za-z0-9'.\-_!#^~$&+=]+@[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$/;
const zeroUser: User = { id: '', userPrincipalName: '', isAccountEnabled: false, isOnPremisesSyncEnabled: false };
const zeroCreate: CreateUserOutput = { user: zeroUser, wasAlreadyCreated: false };
function hasControl(value: string): boolean { return Array.from(value).some(character => { const point = character.codePointAt(0)!; return point < 32 || point === 127; }); }

/** Provider-safe failed attempt classification. */
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'microsoft-entra-id', operation, message }; }
function goTrimSpace(value: string): string {
  const isSpace = (character: string): boolean => {
    const code = character.codePointAt(0)!;
    return code >= 9 && code <= 13 || code === 32 || code === 133 || code === 160 || code === 5760 ||
      code >= 8192 && code <= 8202 || code === 8232 || code === 8233 || code === 8239 || code === 8287 || code === 12288;
  };
  let start = 0, end = value.length;
  while (start < end && isSpace(value[start]!)) start++;
  while (end > start && isSpace(value[end - 1]!)) end--;
  return value.slice(start, end);
}
function checkUserKey(value: string): string {
  const key = goTrimSpace(value);
  if (guid.test(key)) return key.toLowerCase();
  if (Buffer.byteLength(key) <= 256 && lookupUPN.test(key) && key.split('@').length === 2) return key;
  throw new TypeError('userKey must be an object ID or one user principal name such as ada@contoso.com');
}
function checkObjectID(field: string, value: string): string {
  const id = goTrimSpace(value);
  if (!guid.test(id)) throw new TypeError(`${field} must be an object ID, a GUID such as 00000000-0000-0000-0000-000000000000`);
  return id.toLowerCase();
}
function parseObject(body: Uint8Array): Record<string, unknown> {
  let value: unknown;
  try { value = JSON.parse(Buffer.from(body).toString()); }
  catch { throw new TypeError('provider response is not valid JSON'); }
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid JSON object');
  return value as Record<string, unknown>;
}
function decodeUser(value: Record<string, unknown>, requireEnabled: boolean): User {
  const id = value.id;
  const upn = value.userPrincipalName;
  if (typeof id !== 'string' || !guid.test(id)) throw new TypeError('account lacks a valid object ID');
  if (typeof upn !== 'string' || !lookupUPN.test(upn) || Buffer.byteLength(upn) > 256) throw new TypeError('account lacks a valid user principal name');
  if (value.accountEnabled != null && typeof value.accountEnabled !== 'boolean') throw new TypeError('account has invalid accountEnabled');
  if (value.onPremisesSyncEnabled != null && typeof value.onPremisesSyncEnabled !== 'boolean') throw new TypeError('account has invalid onPremisesSyncEnabled');
  if (requireEnabled && typeof value.accountEnabled !== 'boolean') throw new TypeError('account lacks accountEnabled; the token may lack User.Read.All');
  const user: User = { id: id.toLowerCase(), userPrincipalName: upn, isAccountEnabled: value.accountEnabled === true, isOnPremisesSyncEnabled: value.onPremisesSyncEnabled === true };
  for (const field of ['displayName', 'givenName', 'surname', 'mail', 'mailNickname', 'userType', 'jobTitle', 'department', 'usageLocation', 'createdDateTime', 'signInSessionsValidFromDateTime'] as const) {
    const fieldValue = value[field];
    if (fieldValue != null && typeof fieldValue !== 'string') throw new TypeError(`account has invalid ${field}`);
    if (typeof fieldValue === 'string' && fieldValue !== '') user[field] = fieldValue;
  }
  const attributes = value.onPremisesExtensionAttributes;
  if (attributes != null) {
    if (typeof attributes !== 'object' || Array.isArray(attributes) || Object.values(attributes).some(attribute => attribute != null && typeof attribute !== 'string')) throw new TypeError('account has invalid extension attributes');
  }
  return user;
}
function branchFor(exchange: GraphExchange, hasNotFound = true): string {
  if (exchange.outcome === 'invalidResponse') return 'invalidResponse';
  if (exchange.outcome === 'defect') return 'defect';
  if (exchange.outcome === 'notFound' && hasNotFound) return 'notFound';
  return 'providerRejected';
}
function failedQuery<T>(client: EntraClient, call: Call, exchange: GraphExchange, empty: T, hasNotFound = true): QueryAttempt<T> {
  if (exchange.outcome === 'retry') return newQueryRetry(exchange.failure!, exchange.retryAfterMs);
  return newQueryBranch(branchFor(exchange, hasNotFound), empty, exchange.outcome === 'defect' ? zeroReceipt() : client.receipt(call, exchange.response), exchange.failure);
}
function failedMutation<T>(client: EntraClient, call: Call, exchange: GraphExchange, empty: T, hasNotFound = true): MutationAttempt<T> {
  if (exchange.outcome === 'retry') return newMutationRetry(exchange.failure!, exchange.retryAfterMs);
  return newMutationBranch(branchFor(exchange, hasNotFound), empty, exchange.outcome === 'defect' ? zeroReceipt() : client.receipt(call, exchange.response), exchange.failure);
}
async function credentialsOrDefect(client: EntraClient, call: Call): Promise<Credentials | undefined> { return client.resolve(call); }
function credentialDefect<T>(operation: string, value: T): QueryAttempt<T> {
  const problem = failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable');
  return newQueryBranch('defect', value, zeroReceipt(), problem);
}

/** Read one user from a bounded Microsoft Graph projection. */
export class GetUserOperation {
  constructor(private readonly client: EntraClient) {}
  definition() { return GetUserDefinition; }
  async invoke(call: Call, input: GetUserInput): Promise<QueryAttempt<User>> {
    let key: string;
    try { key = checkUserKey(input.userKey); } catch (error) { return newQueryBranch(GetUserBranch.Defect, zeroUser, zeroReceipt(), failure('getUser', 'VALIDATION', (error as Error).message)); }
    const credentials = await credentialsOrDefect(this.client, call);
    if (!credentials) return credentialDefect('getUser', zeroUser);
    const exchange = await this.client.readUser(call, credentials, 'getUser', key, userSelect);
    if (exchange.outcome !== 'succeeded') return failedQuery(this.client, call, exchange, zeroUser);
    const receipt = this.client.receipt(call, exchange.response);
    try {
      const user = decodeUser(parseObject(exchange.response!.body), true);
      return newQueryBranch(GetUserBranch.Found, user, this.client.receipt(call, exchange.response, user.id));
    } catch (error) { return newQueryBranch(GetUserBranch.InvalidResponse, zeroUser, receipt, failure('getUser', 'PROTOCOL', `provider returned an invalid account: ${(error as Error).message}`)); }
  }
}

function validateNextPageURL(value: string): string {
  if (Buffer.byteLength(value) > 8192 || hasControl(value) || !value.startsWith('https://graph.microsoft.com/v1.0/users?')) throw new TypeError('pageToken must be the NextPageToken of a previous listUsers page');
  let parsed: URL;
  try { parsed = new URL(value); } catch { throw new TypeError('pageToken must be the NextPageToken of a previous listUsers page'); }
  if (parsed.protocol !== 'https:' || parsed.host !== 'graph.microsoft.com' || parsed.username || parsed.password || parsed.hash || parsed.pathname !== '/v1.0/users' || !parsed.search) throw new TypeError('pageToken must be the NextPageToken of a previous listUsers page');
  return value;
}
function listRequest(input: ListUsersInput, pageSizeDefault: number): GraphRequest {
  const filter = goTrimSpace(input.filter ?? '');
  const search = goTrimSpace(input.search ?? '');
  const pageToken = goTrimSpace(input.pageToken ?? '');
  if (pageToken) {
    if (filter || search || (input.pageSize ?? 0) !== 0) throw new TypeError('leave filter, search, and pageSize blank with pageToken, which carries the original query');
    const nextPageURL = validateNextPageURL(pageToken);
    const query = new URL(nextPageURL).searchParams;
    return { method: 'GET', nextPageURL, eventual: !!input.usesAdvancedQuery || query.has('$search') || query.has('$count') };
  }
  if (Buffer.byteLength(filter) > 2048 || hasControl(filter)) throw new TypeError('filter must be at most 2048 bytes without control characters');
  if (search && (Buffer.byteLength(search) > 1024 || hasControl(search) || !search.startsWith('"'))) throw new TypeError('search must be a quoted expression such as "displayName:Ada", at most 1024 bytes');
  const pageSize = input.pageSize || pageSizeDefault;
  if (!Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 999) throw new TypeError('pageSize must be from 1 to 999');
  return { method: 'GET', path: '/users', query: { '$select': userSelect, ...(filter ? { '$filter': filter } : {}), ...(search ? { '$search': search } : {}), ...(input.usesAdvancedQuery ? { '$count': 'true' } : {}), '$top': String(pageSize) }, eventual: !!input.usesAdvancedQuery || !!search };
}

/** List one bounded users page, preserving the opaque nextLink. */
export class ListUsersOperation {
  constructor(private readonly client: EntraClient) {}
  definition() { return ListUsersDefinition; }
  async invoke(call: Call, input: ListUsersInput): Promise<QueryAttempt<ListUsersOutput>> {
    let request: GraphRequest;
    try { request = listRequest(input, this.client.listUsersPageSize); } catch (error) { return newQueryBranch(ListUsersBranch.Defect, { users: [] }, zeroReceipt(), failure('listUsers', 'VALIDATION', (error as Error).message)); }
    const credentials = await credentialsOrDefect(this.client, call);
    if (!credentials) return credentialDefect('listUsers', { users: [] });
    const exchange = await this.client.exchange(call, credentials, 'listUsers', request);
    if (exchange.outcome !== 'succeeded') return failedQuery(this.client, call, exchange, { users: [] }, false);
    const receipt = this.client.receipt(call, exchange.response);
    try {
      const page = parseObject(exchange.response!.body);
      if (page.value != null && !Array.isArray(page.value)) throw new TypeError('page has invalid users');
      const users = Array.isArray(page.value) ? page.value.map((value: unknown, index: number) => { try { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('account is not valid JSON'); return decodeUser(value as Record<string, unknown>, true); } catch (error) { throw new TypeError(`account ${index}: ${(error as Error).message}`); } }) : [];
      const nextPageToken = page['@odata.nextLink'];
      if (nextPageToken && typeof nextPageToken !== 'string') throw new TypeError('next page link is not a Microsoft Graph users URL');
      const output: ListUsersOutput = { users, ...(typeof nextPageToken === 'string' && nextPageToken ? { nextPageToken: validateNextPageURL(nextPageToken) } : {}) };
      return newQueryBranch(ListUsersBranch.Listed, output, receipt);
    } catch (error) { return newQueryBranch(ListUsersBranch.InvalidResponse, { users: [] }, receipt, failure('listUsers', 'PROTOCOL', `provider returned an invalid page: ${(error as Error).message}`)); }
  }
}

/** Change an accountEnabled value and confirm the observable account state. */
async function setAccountEnabled(client: EntraClient, call: Call, rawKey: string, enabled: boolean): Promise<MutationAttempt<User>> {
  const operation = enabled ? 'enableUser' : 'disableUser';
  let key: string;
  try { key = checkUserKey(rawKey); } catch (error) { return newMutationBranch('defect', zeroUser, zeroReceipt(), failure(operation, 'VALIDATION', (error as Error).message)); }
  const credentials = await credentialsOrDefect(client, call);
  if (!credentials) return newMutationBranch('defect', zeroUser, zeroReceipt(), failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable'));
  const written = await client.exchange(call, credentials, operation, { method: 'PATCH', path: userPath(key), payload: { accountEnabled: enabled } });
  if (written.outcome !== 'succeeded') return failedMutation(client, call, written, zeroUser);
  const read = await client.readUser(call, credentials, operation, key, userSelect);
  if (read.outcome === 'notFound' && client.withinPropagationWindow(call)) return newMutationRetry(failure(operation, 'AVAILABILITY', 'Microsoft Graph accepted the change but does not return the account yet'));
  if (read.outcome !== 'succeeded') return failedMutation(client, call, read, zeroUser);
  const receipt = client.receipt(call, read.response);
  let user: User;
  try { user = decodeUser(parseObject(read.response!.body), true); }
  catch (error) { return newMutationBranch('invalidResponse', zeroUser, receipt, failure(operation, 'PROTOCOL', `provider returned an invalid account: ${(error as Error).message}`)); }
  const userReceipt = client.receipt(call, read.response, user.id);
  if (user.isAccountEnabled !== enabled) {
    if (client.withinPropagationWindow(call)) return newMutationRetry(failure(operation, 'AVAILABILITY', 'Microsoft Graph does not report the change yet'));
    return newMutationBranch('providerRejected', user, userReceipt, failure(operation, 'PROVIDER_REJECTION', 'Microsoft Graph returned the account without the requested accountEnabled value'));
  }
  return newMutationBranch(enabled ? EnableUserBranch.Enabled : DisableUserBranch.Disabled, user, userReceipt);
}

/** Disable sign-in with a repeatable absolute update. */
export class DisableUserOperation {
  constructor(private readonly client: EntraClient) {}
  definition() { return DisableUserDefinition; }
  idempotencyKey(callId: string, input: DisableUserInput): string { void input; return callId; }
  invoke(call: Call, input: DisableUserInput): Promise<MutationAttempt<User>> { return setAccountEnabled(this.client, call, input.userKey, false); }
}
/** Enable sign-in with a repeatable absolute update. */
export class EnableUserOperation {
  constructor(private readonly client: EntraClient) {}
  definition() { return EnableUserDefinition; }
  idempotencyKey(callId: string, input: EnableUserInput): string { void input; return callId; }
  invoke(call: Call, input: EnableUserInput): Promise<MutationAttempt<User>> { return setAccountEnabled(this.client, call, input.userKey, true); }
}

/** Revoke a user's sign-in sessions, safely repeatable after a lost reply. */
export class RevokeSignInSessionsOperation {
  constructor(private readonly client: EntraClient) {}
  definition() { return RevokeSignInSessionsDefinition; }
  idempotencyKey(callId: string, input: RevokeSignInSessionsInput): string { void input; return callId; }
  async invoke(call: Call, input: RevokeSignInSessionsInput): Promise<MutationAttempt<RevokeSignInSessionsOutput>> {
    let key: string;
    const zero: RevokeSignInSessionsOutput = { userKey: '' };
    try { key = checkUserKey(input.userKey); } catch (error) { return newMutationBranch('defect', zero, zeroReceipt(), failure('revokeSignInSessions', 'VALIDATION', (error as Error).message)); }
    const credentials = await credentialsOrDefect(this.client, call);
    if (!credentials) return newMutationBranch('defect', zero, zeroReceipt(), failure('revokeSignInSessions', 'AUTHENTICATION', 'connection credentials are unavailable'));
    const exchange = await this.client.exchange(call, credentials, 'revokeSignInSessions', { method: 'POST', path: `${userPath(key)}/revokeSignInSessions` });
    if (exchange.outcome !== 'succeeded') return failedMutation(this.client, call, exchange, zero);
    const output = { userKey: key };
    const receipt = this.client.receipt(call, exchange.response, guid.test(key) ? key : '');
    if (!goTrimSpace(Buffer.from(exchange.response!.body).toString())) return newMutationBranch(RevokeSignInSessionsBranch.Revoked, output, receipt);
    try {
      const response = JSON.parse(Buffer.from(exchange.response!.body).toString()) as unknown;
      if (response === null) return newMutationBranch(RevokeSignInSessionsBranch.Revoked, output, receipt);
      if (!response || typeof response !== 'object' || Array.isArray(response)) throw new TypeError('invalid response');
      const parsed = response as { value?: unknown };
      if (parsed.value != null && typeof parsed.value !== 'boolean') throw new TypeError('invalid value');
      if (parsed.value === false) return newMutationBranch(RevokeSignInSessionsBranch.ProviderRejected, output, receipt, failure('revokeSignInSessions', 'PROVIDER_REJECTION', 'Microsoft Graph reported that it did not revoke the sessions'));
    } catch { return newMutationBranch(RevokeSignInSessionsBranch.InvalidResponse, output, receipt, failure('revokeSignInSessions', 'PROTOCOL', 'provider returned an invalid revocation response')); }
    return newMutationBranch(RevokeSignInSessionsBranch.Revoked, output, receipt);
  }
}

function textField(name: string, value: string | undefined, limit: number): string {
  const trimmed = goTrimSpace(value ?? '');
  if (Array.from(trimmed).length > limit) throw new TypeError(`${name} must be valid text of at most ${limit} characters`);
  if (/\p{Cc}/u.test(trimmed)) throw new TypeError(`${name} cannot contain control characters`);
  return trimmed;
}
function createPayload(input: CreateUserInput, attribute: string, key: string): Record<string, unknown> {
  const upn = goTrimSpace(input.userPrincipalName);
  const [local, domain, extra] = upn.split('@');
  if (extra !== undefined || !local || !domain || Buffer.byteLength(upn) > 113 || Buffer.byteLength(local) > 64 || Buffer.byteLength(domain) > 48 || !/^[A-Za-z0-9'.\-_!#^~]+$/.test(local) || local.startsWith('.') || local.endsWith('.') || !/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$/i.test(domain)) throw new TypeError("userPrincipalName must be alias@domain with at most 64 letters, digits, or ' . - _ ! # ^ ~ before the @ and a verified domain after it");
  if (!/^[A-Za-z0-9._:-]{1,128}$/.test(key)) throw new TypeError('provisioningKey must be 1 to 128 letters, digits, or . _ : - characters');
  const givenName = textField('givenName', input.givenName, 64);
  const surname = textField('surname', input.surname, 64);
  const displayName = textField('displayName', input.displayName, 256) || goTrimSpace(`${givenName} ${surname}`);
  if (!displayName) throw new TypeError('displayName, givenName, or surname is required');
  const jobTitle = textField('jobTitle', input.jobTitle, 128);
  const department = textField('department', input.department, 64);
  const mailNickname = goTrimSpace(input.mailNickname ?? '') || local;
  if (mailNickname.length > 64 || !/^[A-Za-z0-9!#$%&'*+\-/=?^_`{|}~.]+$/.test(mailNickname)) throw new TypeError('mailNickname must be at most 64 letters, digits, or alias punctuation without spaces or @');
  const usageLocation = goTrimSpace(input.usageLocation ?? '');
  if (usageLocation && !/^[A-Z]{2}$/.test(usageLocation)) throw new TypeError('usageLocation must be a two-letter uppercase ISO 3166 country code such as US');
  return { accountEnabled: true, displayName, ...(givenName ? { givenName } : {}), ...(surname ? { surname } : {}), mailNickname, userPrincipalName: upn, ...(jobTitle ? { jobTitle } : {}), ...(department ? { department } : {}), ...(usageLocation ? { usageLocation } : {}), passwordProfile: { forceChangePasswordNextSignIn: true, password: '' }, onPremisesExtensionAttributes: { [attribute]: `dex-creation-key:${key}` } };
}
function initialPassword(draw: (upper: number) => number): string {
  const classes = ['abcdefghijklmnopqrstuvwxyz', 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', '0123456789', '!#%*+-=?@^_~'];
  const alphabet = classes.join('');
  const characters = classes.map(characterClass => characterClass[draw(characterClass.length)]!);
  while (characters.length < 44) characters.push(alphabet[draw(alphabet.length)]!);
  for (let index = characters.length - 1; index > 0; index--) {
    const swap = draw(index + 1);
    [characters[index], characters[swap]] = [characters[swap]!, characters[index]!];
  }
  return characters.join('');
}
/** Create a tagged account and converge on a repeated principal name. */
export class CreateUserOperation {
  constructor(private readonly client: EntraClient) {}
  definition() { return CreateUserDefinition; }
  idempotencyKey(callId: string, input: CreateUserInput): string { return goTrimSpace(input.provisioningKey ?? '') || callId; }
  async invoke(call: Call, input: CreateUserInput): Promise<MutationAttempt<CreateUserOutput>> {
    const operation = 'createUser';
    const key = call.idempotencyKey ?? this.idempotencyKey(call.id, input);
    let payload: Record<string, unknown>;
    try { payload = createPayload(input, this.client.creationKeyAttribute, key); }
    catch (error) { return newMutationBranch(CreateUserBranch.Defect, zeroCreate, zeroReceipt(), failure(operation, 'VALIDATION', (error as Error).message)); }
    const credentials = await this.client.resolve(call);
    if (!credentials) return newMutationBranch(CreateUserBranch.Defect, zeroCreate, zeroReceipt(), failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable'));
    try { (payload.passwordProfile as Record<string, unknown>).password = initialPassword(this.client.randomInt ?? randomInt); }
    catch { return newMutationBranch(CreateUserBranch.Defect, zeroCreate, zeroReceipt(), failure(operation, 'LOCAL_DEFECT', 'initial password could not be generated')); }
    const written = await this.client.exchange(call, credentials, operation, { method: 'POST', path: '/users', payload });
    if (written.outcome === 'succeeded') {
      try {
        const user = decodeUser(parseObject(written.response!.body), false);
        if (user.userPrincipalName.toLowerCase() !== String(payload.userPrincipalName).toLowerCase()) throw new TypeError();
        user.isAccountEnabled = true;
        for (const field of ['mailNickname', 'department', 'usageLocation'] as const) if (!user[field] && payload[field]) user[field] = String(payload[field]);
        return newMutationBranch(CreateUserBranch.Created, { user, wasAlreadyCreated: false }, this.client.receipt(call, written.response, user.id));
      } catch { return newMutationRetry(failure(operation, 'PROTOCOL', 'create response is unusable; the retry reads the account back')); }
    }
    if (written.outcome === 'invalidResponse') return newMutationRetry(failure(operation, written.failure!.kind, 'create response is unusable; the retry reads the account back'));
    if (written.outcome === 'badRequest') {
      const read = await this.client.readUser(call, credentials, operation, String(payload.userPrincipalName), creationSelect);
      if (read.outcome === 'succeeded') {
        const receipt = this.client.receipt(call, read.response);
        try {
          const resource = parseObject(read.response!.body);
          const user = decodeUser(resource, true);
          const userReceipt = this.client.receipt(call, read.response, user.id);
          const attributes = resource.onPremisesExtensionAttributes;
          if (attributes && typeof attributes === 'object' && !Array.isArray(attributes) && (attributes as Record<string, unknown>)[this.client.creationKeyAttribute] === `dex-creation-key:${key}`) return newMutationBranch(CreateUserBranch.Created, { user, wasAlreadyCreated: true }, userReceipt);
          return newMutationBranch(CreateUserBranch.AlreadyExists, { user, wasAlreadyCreated: false }, userReceipt, failure(operation, 'CONFLICT', 'user principal name belongs to an account this Step did not create'));
        } catch (error) { return newMutationBranch(CreateUserBranch.InvalidResponse, zeroCreate, receipt, failure(operation, 'PROTOCOL', `provider returned an invalid account: ${(error as Error).message}`)); }
      }
      if (read.outcome === 'notFound') {
        if (this.client.withinPropagationWindow(call)) return newMutationRetry(failure(operation, 'AVAILABILITY', 'Microsoft Graph rejected the account but does not return the user principal name yet'));
        const details = readErrorTokens(written.response!.body, ['/error/details/0/code', '/error/details/1/code', '/error/details/2/code']);
        if (details.includes('ObjectConflict')) return newMutationBranch(CreateUserBranch.AlreadyExists, zeroCreate, this.client.receipt(call, read.response), failure(operation, 'CONFLICT', 'user principal name or mail alias belongs to an object outside the user directory, such as a deleted account'));
        return newMutationBranch(CreateUserBranch.ProviderRejected, zeroCreate, this.client.receipt(call, written.response), written.failure);
      }
      return failedMutation(this.client, call, read, zeroCreate, false);
    }
    if (written.outcome === 'notFound' || written.outcome === 'rejected') return newMutationBranch(CreateUserBranch.ProviderRejected, zeroCreate, this.client.receipt(call, written.response), written.failure);
    return failedMutation(this.client, call, written, zeroCreate, false);
  }
}

function memberIds(input: AddUserToGroupInput | RemoveUserFromGroupInput): [string, string] { return [checkObjectID('groupId', input.groupId), checkObjectID('userId', input.userId)]; }
function memberPath(groupId: string): string { return `/groups/${groupId}/members`; }
function memberCheckRequest(groupId: string, userId: string): GraphRequest { return { method: 'GET', path: memberPath(groupId), eventual: true, query: { '$count': 'true', '$filter': `id eq '${userId}'`, '$select': 'id' } }; }

/** Add one direct membership, converging on a preexisting one. */
export class AddUserToGroupOperation {
  constructor(private readonly client: EntraClient) {}
  definition() { return AddUserToGroupDefinition; }
  idempotencyKey(callId: string, input: AddUserToGroupInput): string { void input; return callId; }
  async invoke(call: Call, input: AddUserToGroupInput): Promise<MutationAttempt<AddUserToGroupOutput>> {
    const operation = 'addUserToGroup';
    const zero: AddUserToGroupOutput = { groupId: '', userId: '', wasAlreadyMember: false };
    let groupId: string, userId: string;
    try { [groupId, userId] = memberIds(input); } catch (error) { return newMutationBranch(AddUserToGroupBranch.Defect, zero, zeroReceipt(), failure(operation, 'VALIDATION', (error as Error).message)); }
    const credentials = await this.client.resolve(call);
    if (!credentials) return newMutationBranch(AddUserToGroupBranch.Defect, zero, zeroReceipt(), failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable'));
    const output = { groupId, userId, wasAlreadyMember: false };
    const written = await this.client.exchange(call, credentials, operation, { method: 'POST', path: `${memberPath(groupId)}/$ref`, payload: { '@odata.id': `https://graph.microsoft.com/v1.0/directoryObjects/${userId}` } });
    if (written.outcome === 'succeeded') return newMutationBranch(AddUserToGroupBranch.Added, output, this.client.receipt(call, written.response, userId));
    if (written.outcome === 'badRequest') {
      const check = await this.client.exchange(call, credentials, operation, memberCheckRequest(groupId, userId));
      if (check.outcome !== 'succeeded') return failedMutation(this.client, call, check, zero);
      const receipt = this.client.receipt(call, check.response, userId);
      let isMember = false;
      try { const page = parseObject(check.response!.body); if (page.value != null && !Array.isArray(page.value)) throw new TypeError('invalid members'); isMember = Array.isArray(page.value) && page.value.some((item: unknown) => { if (!item || typeof item !== 'object' || Array.isArray(item)) throw new TypeError('invalid member'); const id = (item as Record<string, unknown>).id; if (id != null && typeof id !== 'string') throw new TypeError('invalid member ID'); return typeof id === 'string' && id.toLowerCase() === userId; }); }
      catch { return newMutationBranch(AddUserToGroupBranch.InvalidResponse, zero, receipt, failure(operation, 'PROTOCOL', 'provider returned an invalid membership check')); }
      if (isMember) return newMutationBranch(AddUserToGroupBranch.Added, { ...output, wasAlreadyMember: true }, receipt);
      if (this.client.withinPropagationWindow(call)) return newMutationRetry(failure(operation, 'AVAILABILITY', 'Microsoft Graph rejected the membership but does not list it yet'));
      return newMutationBranch(AddUserToGroupBranch.ProviderRejected, zero, this.client.receipt(call, written.response), written.failure);
    }
    if (written.outcome === 'notFound') {
      const group = await this.client.readGroup(call, credentials, operation, groupId);
      if (group.outcome !== 'succeeded') return failedMutation(this.client, call, group, zero);
      if (this.client.withinPropagationWindow(call)) return newMutationRetry(failure(operation, 'AVAILABILITY', 'Microsoft Graph does not return the account yet'));
      return newMutationBranch(AddUserToGroupBranch.NotFound, zero, this.client.receipt(call, group.response), failure(operation, 'NOT_FOUND', 'account was not found'));
    }
    return failedMutation(this.client, call, written, zero);
  }
}

/** Remove only a direct membership reference, never the user object. */
export class RemoveUserFromGroupOperation {
  constructor(private readonly client: EntraClient) {}
  definition() { return RemoveUserFromGroupDefinition; }
  idempotencyKey(callId: string, input: RemoveUserFromGroupInput): string { void input; return callId; }
  async invoke(call: Call, input: RemoveUserFromGroupInput): Promise<MutationAttempt<RemoveUserFromGroupOutput>> {
    const operation = 'removeUserFromGroup';
    const zero: RemoveUserFromGroupOutput = { groupId: '', userId: '', wasAlreadyRemoved: false };
    let groupId: string, userId: string;
    try { [groupId, userId] = memberIds(input); } catch (error) { return newMutationBranch(RemoveUserFromGroupBranch.Defect, zero, zeroReceipt(), failure(operation, 'VALIDATION', (error as Error).message)); }
    const credentials = await this.client.resolve(call);
    if (!credentials) return newMutationBranch(RemoveUserFromGroupBranch.Defect, zero, zeroReceipt(), failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable'));
    const output = { groupId, userId, wasAlreadyRemoved: false };
    const removed = await this.client.exchange(call, credentials, operation, { method: 'DELETE', path: `${memberPath(groupId)}/${userId}/$ref` });
    if (removed.outcome === 'succeeded') return newMutationBranch(RemoveUserFromGroupBranch.Removed, output, this.client.receipt(call, removed.response, userId));
    if (removed.outcome === 'notFound') {
      const group = await this.client.readGroup(call, credentials, operation, groupId);
      if (group.outcome !== 'succeeded') return failedMutation(this.client, call, group, zero);
      return newMutationBranch(RemoveUserFromGroupBranch.Removed, { ...output, wasAlreadyRemoved: true }, this.client.receipt(call, group.response, userId));
    }
    return failedMutation(this.client, call, removed, zero);
  }
}
