// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newQueryBranch, type Call, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { TypeformClient } from './client.js';
import { GetFormDefinition, ListFormsDefinition, ListResponsesDefinition, UpsertWebhookDefinition } from './generated/definitions.js';
import { emptyForm, emptyFormPage, emptyResponsePage, emptyWebhook } from './generated/mock.js';
import type { Form, FormAnswer, FormField, FormPage, FormResponse, GetFormInput, ListFormsInput, ListResponsesInput, ResponsePage, UpsertWebhookInput, Webhook } from './generated/models.js';

const idPattern = /^[A-Za-z0-9_-]{1,128}$/;
const tagPattern = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$/;
/** List one page of forms. */
export class ListFormsOperation implements Query<ListFormsInput, FormPage> {
  /** Bind the client. */ constructor(private readonly client: TypeformClient) {}
  /** Manifest definition. */ definition() { return ListFormsDefinition; }
  /** Validate, send, and decode one listing attempt. */
  async invoke(call: Call, input: ListFormsInput): Promise<QueryAttempt<FormPage>> {
    const operation = 'listForms';
    if (Buffer.byteLength(input.search ?? '') > 256 || (input.workspaceId && !idPattern.test(input.workspaceId)) || !validRange(input.page ?? 0, 0, 10000) || !validRange(input.pageSize ?? 0, 0, 200)) return validation(this.client, operation, emptyFormPage, 'invalid form list input');
    const resolved = await this.client.resolve(call, operation);
    if ('outcome' in resolved) return this.client.queryOutcome(call, resolved.outcome, emptyFormPage, '', false);
    const page = input.page || 1;
    const query = new URLSearchParams(); query.set('page', String(page));
    if (input.pageSize) query.set('page_size', String(input.pageSize));
    if (input.search) query.set('search', input.search);
    if (input.workspaceId) query.set('workspace_id', input.workspaceId);
    const result = await this.client.exchange(call, operation, resolved.credentials, 'GET', `/forms?${goQuery(query)}`);
    if ('outcome' in result) return this.client.queryOutcome(call, result.outcome, emptyFormPage, '', false);
    try { return newQueryBranch('listed', decodeFormPage(result.response.body, page), this.client.receipt(call, '')); }
    catch { return this.client.queryOutcome(call, this.client.malformed(operation), emptyFormPage, '', false); }
  }
}
/** Read one form with flattened group fields. */
export class GetFormOperation implements Query<GetFormInput, Form> {
  /** Bind the client. */ constructor(private readonly client: TypeformClient) {}
  /** Manifest definition. */ definition() { return GetFormDefinition; }
  /** Validate, send, and decode one form attempt. */
  async invoke(call: Call, input: GetFormInput): Promise<QueryAttempt<Form>> {
    const operation = 'getForm';
    if (!idPattern.test(input.formId)) return validation(this.client, operation, emptyForm, 'formId must be a Typeform form ID such as u6nXL7, the part after /to/ in the form link');
    const resolved = await this.client.resolve(call, operation);
    if ('outcome' in resolved) return this.client.queryOutcome(call, resolved.outcome, emptyForm, '', true);
    const result = await this.client.exchange(call, operation, resolved.credentials, 'GET', `/forms/${input.formId}`);
    if ('outcome' in result) return this.client.queryOutcome(call, result.outcome, emptyForm, input.formId, true);
    try { const form = decodeForm(result.response.body); if (form.id !== input.formId) throw new TypeError('different form'); return newQueryBranch('found', form, this.client.receipt(call, form.id)); }
    catch { return this.client.queryOutcome(call, this.client.malformed(operation), emptyForm, input.formId, true); }
  }
}
/** List one page of completed responses. */
export class ListResponsesOperation implements Query<ListResponsesInput, ResponsePage> {
  /** Bind the client. */ constructor(private readonly client: TypeformClient) {}
  /** Manifest definition. */ definition() { return ListResponsesDefinition; }
  /** Validate, send, and decode one response page. */
  async invoke(call: Call, input: ListResponsesInput): Promise<QueryAttempt<ResponsePage>> {
    const operation = 'listResponses';
    if (!idPattern.test(input.formId) || !validRange(input.pageSize ?? 0, 0, 1000) || !!(input.before && input.after) || !!(input.before && !idPattern.test(input.before)) || !!(input.after && !idPattern.test(input.after))) return validation(this.client, operation, emptyResponsePage, 'invalid response list input');
    let since: string | undefined;
    let until: string | undefined;
    try {
      since = input.since ? formatFilterTime(input.since) : undefined;
      until = input.until ? formatFilterTime(input.until) : undefined;
    } catch { return validation(this.client, operation, emptyResponsePage, 'invalid response list input'); }
    if (since && until && since > until) return validation(this.client, operation, emptyResponsePage, 'since must not be later than until');
    const resolved = await this.client.resolve(call, operation);
    if ('outcome' in resolved) return this.client.queryOutcome(call, resolved.outcome, emptyResponsePage, '', true);
    const query = new URLSearchParams();
    if (input.pageSize) query.set('page_size', String(input.pageSize));
    if (since) query.set('since', since);
    if (until) query.set('until', until);
    if (input.before) query.set('before', input.before);
    if (input.after) query.set('after', input.after);
    const path = `/forms/${input.formId}/responses${query.size ? `?${goQuery(query)}` : ''}`;
    const result = await this.client.exchange(call, operation, resolved.credentials, 'GET', path);
    if ('outcome' in result) return this.client.queryOutcome(call, result.outcome, emptyResponsePage, input.formId, true);
    try { return newQueryBranch('listed', decodeResponsePage(result.response.body, input), this.client.receipt(call, input.formId)); }
    catch { return this.client.queryOutcome(call, this.client.malformed(operation), emptyResponsePage, input.formId, true); }
  }
}
/** Replace a webhook by stable form and tag. */
export class UpsertWebhookOperation implements Mutation<UpsertWebhookInput, Webhook> {
  /** Bind the client. */ constructor(private readonly client: TypeformClient) {}
  /** Manifest definition. */ definition() { return UpsertWebhookDefinition; }
  /** Stable key; Typeform's PUT by tag supplies idempotency. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, send, and decode one webhook upsert. */
  async invoke(call: Call, input: UpsertWebhookInput): Promise<MutationAttempt<Webhook>> {
    const operation = 'upsertWebhook';
    if (!idPattern.test(input.formId) || !tagPattern.test(input.tag) || !validWebhookURL(input.url)) return newMutationBranch('defect', emptyWebhook, {}, this.client.failure(operation, 'VALIDATION', 'invalid webhook input'));
    const resolved = await this.client.resolve(call, operation);
    if ('outcome' in resolved) return this.client.mutationOutcome(call, resolved.outcome, emptyWebhook, '');
    const secret = resolved.credentials.webhookSecret;
    if (!(secret instanceof Object) || typeof secret.reveal !== 'function' || !secret.reveal()) return newMutationBranch('defect', emptyWebhook, {}, this.client.failure(operation, 'VALIDATION', 'set webhook_secret on the Typeform connection before registering a webhook, so deliveries can be verified'));
    const body = { url: input.url, enabled: !input.disabled, secret: secret.reveal(), event_types: { form_response: true } };
    const result = await this.client.exchange(call, operation, resolved.credentials, 'PUT', `/forms/${input.formId}/webhooks/${goPathEscape(input.tag)}`, body);
    if ('outcome' in result) return this.client.mutationOutcome(call, result.outcome, emptyWebhook, input.formId);
    try { const webhook = decodeWebhook(result.response.body); if (webhook.formId !== input.formId || webhook.tag !== input.tag || webhook.isEnabled === !!input.disabled) throw new TypeError('webhook mismatch'); return newMutationBranch('upserted', webhook, this.client.receipt(call, webhook.id)); }
    catch { return this.client.mutationOutcome(call, this.client.malformed(operation), emptyWebhook, input.formId); }
  }
}
function validation<Output>(client: TypeformClient, operation: string, empty: Output, message: string): QueryAttempt<Output> { return newQueryBranch('defect', empty, {}, client.failure(operation, 'VALIDATION', message)); }
function validRange(value: number, min: number, max: number): boolean { return Number.isSafeInteger(value) && value >= min && value <= max; }
function goQuery(query: URLSearchParams): string { const ordered = new URLSearchParams(); for (const [key, value] of [...query.entries()].sort(([left], [right]) => left < right ? -1 : left > right ? 1 : 0)) ordered.append(key, value); return ordered.toString().replace(/\*/g, '%2A'); }
function goPathEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`); }
function formatFilterTime(value: string): string { const parsed = timestamp(value); if (!parsed) throw new TypeError('invalid filter time'); return parsed.slice(0, 19); }
function validWebhookURL(value: string): boolean { try { const parsed = new URL(value); return Buffer.byteLength(value) <= 2048 && parsed.protocol === 'https:' && !!parsed.hostname && !parsed.username && !parsed.password && !parsed.hash; } catch { return false; } }
function object(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('expected object'); return value as Record<string, unknown>; }
function parsed(body: Uint8Array): Record<string, unknown> { return object(JSON.parse(Buffer.from(body).toString('utf8')) as unknown); }
function string(value: unknown): string { if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new TypeError('expected string'); return value; }
function integer(value: unknown): number { if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('expected integer'); return value; }
function boolean(value: unknown, fallback = false): boolean { if (value === undefined || value === null) return fallback; if (typeof value !== 'boolean') throw new TypeError('expected boolean'); return value; }
function array(value: unknown): unknown[] { if (value === undefined || value === null) return []; if (!Array.isArray(value)) throw new TypeError('expected array'); return value; }
function timestamp(value: unknown): string | undefined {
  const text = string(value);
  if (!text) return undefined;
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(text);
  if (!match) throw new TypeError('invalid timestamp');
  const [, yearText, monthText, dayText, hourText, minuteText, secondText, fractionText, zone] = match;
  const year = Number(yearText), month = Number(monthText), day = Number(dayText), hour = Number(hourText), minute = Number(minuteText), second = Number(secondText);
  const daysInMonth = new Date(Date.UTC(year === 0 ? 400 : year, month, 0)).getUTCDate();
  if (month < 1 || month > 12 || day < 1 || day > daysInMonth || hour > 23 || minute > 59 || second > 59) throw new TypeError('invalid timestamp');
  if (year <= 1) return undefined;
  const offsetHours = zone === 'Z' ? 0 : Number(zone!.slice(1, 3));
  const offsetMinutes = zone === 'Z' ? 0 : Number(zone!.slice(4, 6));
  if (offsetMinutes > 59) throw new TypeError('invalid timestamp');
  const offset = (zone?.startsWith('-') ? -1 : 1) * (offsetHours * 60 + offsetMinutes);
  const base = new Date(0); base.setUTCFullYear(year, month - 1, day); base.setUTCHours(hour, minute, second, 0);
  const utc = new Date(base.getTime() - offset * 60_000);
  const fraction = fractionText?.slice(0, 9).replace(/0+$/, '') ?? '';
  return utc.toISOString().slice(0, 19) + (fraction ? `.${fraction}` : '') + 'Z';
}
function decodeFormPage(body: Uint8Array, page: number): FormPage {
  const source = parsed(body); const totalItems = integer(source.total_items); const pageCount = integer(source.page_count);
  if (totalItems < 0 || pageCount < 0) throw new TypeError('invalid page');
  const forms = array(source.items).map(item => { const sourceForm = object(item); const id = string(sourceForm.id); if (!idPattern.test(id)) throw new TypeError('invalid ID'); const settings = sourceForm.settings ? object(sourceForm.settings) : {}; const links = sourceForm._links ? object(sourceForm._links) : {}; const createdAt = timestamp(sourceForm.created_at); const lastUpdatedAt = timestamp(sourceForm.last_updated_at); return { id, title: string(sourceForm.title), isPublic: boolean(settings.is_public, true), ...(string(links.display) ? { displayUrl: string(links.display) } : {}), ...(createdAt ? { createdAt } : {}), ...(lastUpdatedAt ? { lastUpdatedAt } : {}) }; });
  return { forms, page, pageCount, totalItems, ...(page < pageCount ? { nextPage: page + 1 } : {}) };
}
function decodeForm(body: Uint8Array): Form {
  const source = parsed(body); const id = string(source.id); if (!idPattern.test(id)) throw new TypeError('invalid form ID');
  const fields: FormField[] = [];
  function append(sourceFields: unknown, groupFieldId: string, depth: number): void {
    if (depth > 4) throw new TypeError('field nesting too deep');
    for (const raw of array(sourceFields)) { const field = object(raw); const fieldID = string(field.id); const type = string(field.type); if (!fieldID || !type) throw new TypeError('invalid field'); const properties = field.properties ? object(field.properties) : {}; const validations = field.validations ? object(field.validations) : {}; const choices = array(properties.choices).map(rawChoice => { const choice = object(rawChoice); return { ...(string(choice.id) ? { id: string(choice.id) } : {}), ...(string(choice.ref) ? { ref: string(choice.ref) } : {}), label: string(choice.label) }; }); fields.push({ id: fieldID, title: string(field.title), type, isRequired: boolean(validations.required), ...(string(field.ref) ? { ref: string(field.ref) } : {}), ...(groupFieldId ? { groupFieldId } : {}), ...(choices.length ? { choices } : {}) }); append(properties.fields, fieldID, depth + 1); }
  }
  append(source.fields, '', 1); const links = source._links ? object(source._links) : {};
  return { id, title: string(source.title), fields, ...(string(source.language) ? { language: string(source.language) } : {}), ...(array(source.hidden).length ? { hiddenFields: array(source.hidden).map(string) } : {}), ...(string(links.display) ? { displayUrl: string(links.display) } : {}) };
}
function decodeResponsePage(body: Uint8Array, input: ListResponsesInput): ResponsePage {
  const source = parsed(body); const totalItems = integer(source.total_items); const pageCount = integer(source.page_count); if (totalItems < 0 || pageCount < 0) throw new TypeError('invalid page'); const items = array(source.items); const pageSize = input.pageSize || 25; if (items.length > pageSize) throw new TypeError('page too large'); const responses = items.map(item => decodeResponse(object(item))); const nextBeforeToken = !input.after && responses.length === pageSize && pageCount > 1 ? responses.at(-1)?.token : undefined; return { formId: input.formId, responses, totalItems, pageCount, ...(nextBeforeToken ? { nextBeforeToken } : {}) };
}
function decodeResponse(source: Record<string, unknown>): FormResponse {
  const token = string(source.token) || string(source.response_id); if (!idPattern.test(token)) throw new TypeError('invalid token'); const answers = array(source.answers).map(raw => decodeAnswer(object(raw))); const landedAt = timestamp(source.landed_at); const submittedAt = timestamp(source.submitted_at); const hidden = source.hidden ? object(source.hidden) : {}; const hiddenFields: Record<string, string> = {}; for (const [key, value] of Object.entries(hidden)) hiddenFields[key] = typeof value === 'string' ? value : value === null ? '' : JSON.stringify(value); const variables = array(source.variables).map(raw => { const value = object(raw); const key = string(value.key); if (!key) throw new TypeError('invalid variable'); const type = string(value.type); const number = value.number; if (number !== undefined && typeof number !== 'number') throw new TypeError('invalid number'); return { key, type, ...(string(value.text) ? { text: string(value.text) } : {}), ...(number !== undefined && number !== null ? { number } : {}) }; }); const calculated = source.calculated ? object(source.calculated) : {}; const score = calculated.score; if (score !== undefined && score !== null && typeof score !== 'number') throw new TypeError('invalid score'); const ending = source.ending ? object(source.ending) : {}; return { token, answers, ...(landedAt ? { landedAt } : {}), ...(submittedAt ? { submittedAt } : {}), ...(Object.keys(hiddenFields).length ? { hiddenFields } : {}), ...(variables.length ? { variables } : {}), ...(score !== undefined && score !== null ? { score } : {}), ...(string(ending.ref) ? { endingRef: string(ending.ref) } : {}) };
}
function decodeAnswer(source: Record<string, unknown>): FormAnswer {
  const field = object(source.field); const fieldId = string(field.id); const fieldType = string(field.type); const type = string(source.type); if (!fieldId || !fieldType || !type) throw new TypeError('invalid answer'); const answer: FormAnswer = { fieldId, fieldType, type, ...(string(field.ref) ? { fieldRef: string(field.ref) } : {}) }; const fields: Record<string, keyof FormAnswer> = { text: 'text', email: 'email', url: 'url', file_url: 'fileUrl', date: 'date', phone_number: 'phoneNumber', number: 'number', boolean: 'boolean' }; if (type in fields) { const raw = source[type]; if (raw === undefined || raw === null) throw new TypeError('missing answer'); const key = fields[type]!; if (type === 'boolean' ? typeof raw !== 'boolean' : type === 'number' ? typeof raw !== 'number' : typeof raw !== 'string') throw new TypeError('wrong answer type'); Object.assign(answer, { [key]: raw }); } else if (type === 'choice') { const choice = object(source.choice); answer.choice = { ...(string(choice.id) ? { id: string(choice.id) } : {}), ...(string(choice.ref) ? { ref: string(choice.ref) } : {}), ...(string(choice.label) ? { label: string(choice.label) } : {}), ...(string(choice.other) ? { other: string(choice.other) } : {}) }; } else if (type === 'choices') { const choices = source.choices; if (Array.isArray(choices)) { answer.choices = choices.length ? { ids: [], labels: [], refs: [] } : {}; for (const raw of choices) { const choice = object(raw); answer.choices.ids!.push(string(choice.id)); answer.choices.labels!.push(string(choice.label)); answer.choices.refs!.push(string(choice.ref)); if (string(choice.other)) answer.choices.other = string(choice.other); } } else { const choice = object(choices); answer.choices = { ...(choice.ids ? { ids: array(choice.ids).map(string) } : {}), ...(choice.labels ? { labels: array(choice.labels).map(string) } : {}), ...(choice.refs ? { refs: array(choice.refs).map(string) } : {}), ...(string(choice.other) ? { other: string(choice.other) } : {}) }; } } else if (type === 'payment') { const payment = object(source.payment); answer.payment = { amount: string(payment.amount), isSuccessful: boolean(payment.success) }; } else if (type === 'signature') { const signature = object(source.signature); answer.signature = { url: string(signature.url), ...(string(signature.type) ? { method: string(signature.type) } : {}) }; } else if (type === 'multi_format') { const format = object(source.multi_format); answer.multiFormat = { ...(string(format.text) ? { text: string(format.text) } : {}), ...(string(format.video_id) ? { videoId: string(format.video_id) } : {}), ...(string(format.audio_id) ? { audioId: string(format.audio_id) } : {}), ...(string(format.transcript) ? { transcript: string(format.transcript) } : {}) }; } return answer;
}
function decodeWebhook(body: Uint8Array): Webhook { const source = parsed(body); const id = string(source.id), formId = string(source.form_id), tag = string(source.tag), url = string(source.url); if (!id || !formId || !tag || !url || source.enabled === undefined || source.enabled === null) throw new TypeError('invalid webhook'); const events = source.event_types ? object(source.event_types) : {}; const eventTypes = Object.entries(events).filter(([, subscribed]) => boolean(subscribed)).map(([name]) => name).sort(); const createdAt = timestamp(source.created_at); const updatedAt = timestamp(source.updated_at); return { id, formId, tag, url, isEnabled: boolean(source.enabled), eventTypes, ...(createdAt ? { createdAt } : {}), ...(updatedAt ? { updatedAt } : {}) }; }
