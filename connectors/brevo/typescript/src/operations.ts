// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, type Call, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { brevoFailure, isPlainEmail, isRetryableStatus, rejectionFailureKind, type BrevoClient } from './client.js';
import { CreateOrUpdateContactDefinition, GetContactDefinition } from './generated/definitions.js';
import type { Contact, ContactWriteResult, CreateOrUpdateContactInput, GetContactInput } from './generated/models.js';

const emptyWrite: ContactWriteResult = { email: '', created: false };
const emptyContact: Contact = { id: 0, email: '' };

/** Repeat-safe Brevo contact creation or absolute-attribute update. */
export class CreateOrUpdateContactOperation implements Mutation<CreateOrUpdateContactInput, ContactWriteResult> {
  /** Bind one authenticated client. */
  constructor(private readonly client: BrevoClient) {}
  /** Return the manifest definition. */
  definition() { return CreateOrUpdateContactDefinition; }
  /** Use the stable Dex Call ID; Brevo has no idempotency header. */
  idempotencyKey(callId: string): string { return callId; }
  /** Send and classify one contact upsert attempt. */
  async invoke(call: Call, input: CreateOrUpdateContactInput): Promise<MutationAttempt<ContactWriteResult>> {
    const operation = 'createOrUpdateContact';
    if (!isPlainEmail(input.email)) return newMutationBranch('defect', emptyWrite, {}, brevoFailure(operation, 'VALIDATION', 'a plain email address is required'));
    if (input.attributes && Object.keys(input.attributes).some(key => !key || key.toUpperCase() !== key)) {
      return newMutationBranch('defect', emptyWrite, {}, brevoFailure(operation, 'VALIDATION', 'attribute names must be uppercase'));
    }
    let body: Uint8Array;
    try { body = Buffer.from(JSON.stringify({ email: input.email, ...(input.attributes && Object.keys(input.attributes).length ? { attributes: input.attributes } : {}), updateEnabled: true })); }
    catch { return newMutationBranch('defect', emptyWrite, {}, brevoFailure(operation, 'VALIDATION', 'contact payload is invalid or too large')); }
    if (body.byteLength > 1 << 20) return newMutationBranch('defect', emptyWrite, {}, brevoFailure(operation, 'VALIDATION', 'contact payload is invalid or too large'));
    const exchange = await this.client.exchange(call, operation, 'POST', '/contacts', body);
    if ('failure' in exchange) {
      if (exchange.classification === 'retry') return newMutationRetry(exchange.failure);
      return newMutationBranch(exchange.classification === 'invalidResponse' ? 'invalidResponse' : 'defect', emptyWrite, {}, exchange.failure);
    }
    const { response } = exchange;
    if (isRetryableStatus(response.status)) return newMutationRetry(brevoFailure(operation, 'AVAILABILITY', 'Brevo contact request is temporarily unavailable'), this.client.retryAfter(response));
    if (response.status !== 201 && response.status !== 204) return newMutationBranch('providerRejected', emptyWrite, {}, brevoFailure(operation, rejectionFailureKind(response.status), `Brevo rejected contact request with HTTP ${response.status}`));
    if (response.status === 204) return newMutationBranch('saved', { email: input.email, created: false });
    try {
      const decoded: unknown = JSON.parse(Buffer.from(response.body).toString('utf8'));
      if (!decoded || typeof decoded !== 'object' || !('id' in decoded) || typeof decoded.id !== 'number' || !Number.isSafeInteger(decoded.id) || decoded.id <= 0) throw new TypeError('invalid ID');
      return newMutationBranch('saved', { id: decoded.id, email: input.email, created: true });
    } catch { return newMutationBranch('invalidResponse', emptyWrite, {}, brevoFailure(operation, 'PROTOCOL', 'Brevo returned an invalid contact ID')); }
  }
}

/** Retrieve one Brevo contact by email. */
export class GetContactOperation implements Query<GetContactInput, Contact> {
  /** Bind one authenticated client. */
  constructor(private readonly client: BrevoClient) {}
  /** Return the manifest definition. */
  definition() { return GetContactDefinition; }
  /** Send and classify one contact lookup attempt. */
  async invoke(call: Call, input: GetContactInput): Promise<QueryAttempt<Contact>> {
    const operation = 'getContact';
    if (!isPlainEmail(input.email)) return newQueryBranch('defect', emptyContact, {}, brevoFailure(operation, 'VALIDATION', 'a plain email address is required'));
    const exchange = await this.client.exchange(call, operation, 'GET', `/contacts/${escapeContactEmailPath(input.email)}`);
    if ('failure' in exchange) {
      if (exchange.classification === 'retry') return newQueryRetry(exchange.failure);
      return newQueryBranch(exchange.classification === 'invalidResponse' ? 'invalidResponse' : 'defect', emptyContact, {}, exchange.failure);
    }
    const { response } = exchange;
    if (isRetryableStatus(response.status)) return newQueryRetry(brevoFailure(operation, 'AVAILABILITY', 'Brevo contact lookup is temporarily unavailable'), this.client.retryAfter(response));
    if (response.status === 404) return newQueryBranch('notFound', emptyContact, {}, brevoFailure(operation, 'NOT_FOUND', 'Brevo contact was not found'));
    if (response.status !== 200) return newQueryBranch('providerRejected', emptyContact, {}, brevoFailure(operation, rejectionFailureKind(response.status), `Brevo rejected contact lookup with HTTP ${response.status}`));
    try {
      const decoded: unknown = JSON.parse(Buffer.from(response.body).toString('utf8'));
      if (!decoded || typeof decoded !== 'object' || !('id' in decoded) || typeof decoded.id !== 'number' || !Number.isSafeInteger(decoded.id) || decoded.id <= 0 || !('email' in decoded) || typeof decoded.email !== 'string' || !isPlainEmail(decoded.email)) throw new TypeError('invalid contact');
      return newQueryBranch('found', { id: decoded.id, email: decoded.email });
    } catch { return newQueryBranch('invalidResponse', emptyContact, {}, brevoFailure(operation, 'PROTOCOL', 'Brevo returned an invalid contact')); }
  }
}

function escapeContactEmailPath(email: string): string {
  return encodeURIComponent(email)
    .replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`)
    .replace(/%(24|26|2B|3A|3D|40)/gi, encoded => String.fromCharCode(Number.parseInt(encoded.slice(1), 16)));
}
