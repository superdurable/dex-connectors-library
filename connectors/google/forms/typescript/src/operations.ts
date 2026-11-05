// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { GetFormDefinition, GetResponseDefinition, ListResponsesDefinition } from './generated/definitions.js';
import type { Form, FormAnswer, FormChoice, FormItem, FormQuestion, FormResponse, GetFormInput, GetResponseInput, ListResponsesInput, ResponsePage } from './generated/models.js';
import { goQueryEncode, type GoogleFormsClient, type ReadFailure } from './client.js';

const idPattern = /^[A-Za-z0-9_-]{1,256}$/;
const emptyForm: Form = { formId: '', title: '', items: [] };
const emptyResponse: FormResponse = { responseId: '', formId: '', lastSubmittedAt: '0001-01-01T00:00:00Z', answers: [] };
const emptyPage: ResponsePage = { formId: '', responses: [] };
function fail(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'google-forms', operation, message }; }
function defect<T>(operation: string, output: T, message: string): QueryAttempt<T> { return newQueryBranch('defect', output, zeroReceipt(), fail(operation, 'VALIDATION', message)); }
function outcome<T>(failure: ReadFailure, output: T): QueryAttempt<T> {
  if (failure.disposition === 'retry') return newQueryRetry(failure.failure, failure.retryAfterMs);
  return newQueryBranch(failure.disposition, output, failure.receipt, failure.failure);
}
function invalid<T>(operation: string, output: T, receipt: ReceiptDraft, message: string): QueryAttempt<T> {
  return newQueryBranch('invalidResponse', output, receipt, fail(operation, 'PROTOCOL', message));
}
function object(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid provider object');
  return value as Record<string, unknown>;
}
function optionalObject(value: unknown): Record<string, unknown> { return value == null ? {} : object(value); }
function string(value: unknown): string { if (value == null) return ''; if (typeof value !== 'string') throw new TypeError('invalid provider string'); return value; }
function bool(value: unknown): boolean { if (value == null) return false; if (typeof value !== 'boolean') throw new TypeError('invalid provider boolean'); return value; }
function number(value: unknown): number { if (value == null) return 0; if (typeof value !== 'number' || !Number.isFinite(value)) throw new TypeError('invalid provider number'); return value; }
function integer(value: unknown): number { const result = number(value); if (!Number.isSafeInteger(result)) throw new TypeError('invalid provider integer'); return result; }
function array(value: unknown): unknown[] { if (value == null) return []; if (!Array.isArray(value)) throw new TypeError('invalid provider array'); return value; }
function parseBody(body: Uint8Array): Record<string, unknown> { return object(JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body))); }
function optionalString(value: unknown): string | undefined { const converted = string(value); return converted || undefined; }
function choices(value: unknown): FormChoice[] {
  return array(optionalObject(value).options).map(entry => { const choice = object(entry); const result: FormChoice = { value: string(choice.value) }; if (bool(choice.isOther)) result.isOther = true; return result; });
}
function question(value: unknown, itemId: string, title: string, grid?: Record<string, unknown>): FormQuestion {
  const source = object(value); const questionId = string(source.questionId); if (!questionId) throw new TypeError('missing question ID');
  const result: FormQuestion = { questionId, itemId, title, type: 'unknown', isRequired: bool(source.required) };
  let kinds = 0;
  if (source.choiceQuestion != null) { kinds++; const choice = object(source.choiceQuestion); result.type = ({ RADIO: 'radio', CHECKBOX: 'checkbox', DROP_DOWN: 'dropDown' } as Record<string, string>)[string(choice.type)] ?? 'unknown'; const options = choices(choice); if (options.length) result.choices = options; }
  if (source.textQuestion != null) { kinds++; result.type = bool(object(source.textQuestion).paragraph) ? 'paragraph' : 'shortText'; }
  if (source.scaleQuestion != null) { kinds++; const scale = object(source.scaleQuestion); result.type = 'scale'; result.scale = { low: integer(scale.low), high: integer(scale.high) }; const lowLabel = optionalString(scale.lowLabel); const highLabel = optionalString(scale.highLabel); if (lowLabel) result.scale.lowLabel = lowLabel; if (highLabel) result.scale.highLabel = highLabel; }
  if (source.dateQuestion != null) { kinds++; const date = object(source.dateQuestion); result.type = 'date'; if (bool(date.includeYear)) result.includesYear = true; if (bool(date.includeTime)) result.includesTime = true; }
  if (source.timeQuestion != null) { kinds++; result.type = 'time'; if (bool(object(source.timeQuestion).duration)) result.isDuration = true; }
  if (source.fileUploadQuestion != null) { kinds++; result.type = 'fileUpload'; }
  if (source.ratingQuestion != null) { kinds++; result.type = 'rating'; const level = integer(object(source.ratingQuestion).ratingScaleLevel); if (level) result.ratingScaleLevel = level; }
  if (source.rowQuestion != null) { kinds++; if (grid) { result.type = ({ RADIO: 'radioGrid', CHECKBOX: 'checkboxGrid' } as Record<string, string>)[string(grid.type)] ?? 'unknown'; const options = choices(grid); if (options.length) result.choices = options; } }
  if (kinds > 1) throw new TypeError('multiple question kinds');
  const pointValue = integer(optionalObject(source.grading).pointValue); if (pointValue) result.pointValue = pointValue;
  return result;
}
function item(value: unknown): FormItem {
  const source = object(value); const itemId = string(source.itemId); if (!itemId) throw new TypeError('missing item ID');
  const result: FormItem = { itemId, kind: 'unknown' }; const title = optionalString(source.title); const description = optionalString(source.description); if (title) result.title = title; if (description) result.description = description;
  let kinds = 0;
  if (source.questionItem != null) { kinds++; result.kind = 'question'; result.questions = [question(object(source.questionItem).question, itemId, title ?? '')]; }
  if (source.questionGroupItem != null) { kinds++; result.kind = 'questionGroup'; const group = object(source.questionGroupItem); const grid = group.grid == null ? undefined : object(optionalObject(group.grid).columns); const questions = array(group.questions).map(entry => question(entry, itemId, string(optionalObject(object(entry).rowQuestion).title), grid)); if (questions.length) result.questions = questions; }
  for (const [field, kind] of [['pageBreakItem', 'pageBreak'], ['textItem', 'text'], ['imageItem', 'image'], ['videoItem', 'video']] as const) if (source[field] != null) { kinds++; result.kind = kind; }
  if (kinds > 1) throw new TypeError('multiple item kinds');
  return result;
}
function form(value: Record<string, unknown>): Form {
  const formId = string(value.formId); if (!idPattern.test(formId)) throw new TypeError('invalid form ID');
  const info = optionalObject(value.info), settings = optionalObject(value.settings);
  const result: Form = { formId, title: string(info.title), items: array(value.items).map(item) };
  for (const [key, source] of [['documentTitle', info], ['description', info], ['responderUri', value], ['linkedSheetId', value], ['revisionId', value], ['emailCollectionType', settings]] as const) { const field = optionalString(source[key]); if (field) result[key] = field; }
  if (bool(optionalObject(settings.quizSettings).isQuiz)) result.isQuiz = true;
  if (value.publishSettings != null && optionalObject(value.publishSettings).publishState != null) { const state = object(optionalObject(value.publishSettings).publishState); result.publishState = { isPublished: bool(state.isPublished), isAcceptingResponses: bool(state.isAcceptingResponses) }; }
  const ids = new Set<string>(); for (const entry of result.items ?? []) for (const q of entry.questions ?? []) { if (ids.has(q.questionId)) throw new TypeError('duplicate question'); ids.add(q.questionId); }
  return result;
}
function goTimestamp(value: unknown): string {
  const text = string(value); const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(text);
  if (!match) throw new TypeError('invalid time');
  const [year, month, day, hour, minute, second] = match.slice(1, 7).map(Number);
  const leap = year! % 4 === 0 && (year! % 100 !== 0 || year! % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  const offset = match[8]!;
  if (month! < 1 || month! > 12 || day! < 1 || day! > days[month! - 1]! || hour! > 23 || minute! > 59 || second! > 59 || (offset !== 'Z' && Number(offset.slice(4)) > 59)) throw new TypeError('invalid time');
  const fraction = (match[7] ?? '').slice(0, 9).replace(/0+$/, '');
  const date = new Date(0);
  date.setUTCFullYear(year!, month! - 1, day!);
  date.setUTCHours(hour!, minute!, second!, 0);
  const offsetMinutes = offset === 'Z' ? 0 : (offset[0] === '+' ? 1 : -1) * (Number(offset.slice(1, 3)) * 60 + Number(offset.slice(4, 6)));
  date.setTime(date.getTime() - offsetMinutes * 60_000);
  if (Number.isNaN(date.getTime())) throw new TypeError('invalid time');
  const base = date.toISOString().replace(/\.000Z$/, '');
  return `${base}${fraction ? '.' + fraction : ''}Z`;
}

function response(value: unknown, formId: string): FormResponse {
  const source = object(value), responseId = string(source.responseId), providerFormId = string(source.formId);
  if (!idPattern.test(responseId) || !source.lastSubmittedTime || source.lastSubmittedTime === '0001-01-01T00:00:00Z' || (providerFormId && providerFormId !== formId)) throw new TypeError('invalid response identity');
  const result: FormResponse = { responseId, formId, lastSubmittedAt: goTimestamp(source.lastSubmittedTime), answers: [] };
  if (source.createTime) result.createdAt = goTimestamp(source.createTime);
  const respondentEmail = optionalString(source.respondentEmail); if (respondentEmail) result.respondentEmail = respondentEmail;
  if (source.totalScore != null) result.totalScore = number(source.totalScore);
  const answers = optionalObject(source.answers);
  for (const [questionId, raw] of Object.entries(answers).sort(([a], [b]) => a.localeCompare(b))) {
    const entry = object(raw); if (!questionId || (string(entry.questionId) && string(entry.questionId) !== questionId)) throw new TypeError('invalid answer key');
    const answer: FormAnswer = { questionId };
    if (entry.textAnswers != null) { const values = array(object(entry.textAnswers).answers).map(item => string(object(item).value)); if (values.length) answer.values = values; }
    if (entry.fileUploadAnswers != null) { const files = array(object(entry.fileUploadAnswers).answers).map(file => { const item = object(file), fileId = string(item.fileId); if (!idPattern.test(fileId)) throw new TypeError('invalid file ID'); const output = { fileId } as { fileId: string; fileName?: string; mimeType?: string }; const fileName = optionalString(item.fileName), mimeType = optionalString(item.mimeType); if (fileName) output.fileName = fileName; if (mimeType) output.mimeType = mimeType; return output; }); if (files.length) answer.files = files; }
    if (entry.grade != null) { const grade = object(entry.grade); answer.grade = { score: number(grade.score), isCorrect: bool(grade.correct) }; }
    result.answers!.push(answer);
  }
  return result;
}

/** Read one form and its questions. */
export class GetFormOperation implements Query<GetFormInput, Form> {
  /** Bind the provider client. */ constructor(private readonly client: GoogleFormsClient) {}
  /** Return the manifest definition. */ definition() { return GetFormDefinition; }
  /** Invoke one provider read. */
  async invoke(call: Call, input: GetFormInput): Promise<QueryAttempt<Form>> {
    if (!idPattern.test(input.formId)) return defect('getForm', emptyForm, 'formId must be a Google Forms form ID');
    const result = await this.client.read(call, 'getForm', `/forms/${input.formId}`);
    if ('outcome' in result) return outcome(result.outcome, emptyForm);
    const receipt = this.client.receipt(call, requestId(result.response), input.formId);
    try { const output = form(parseBody(result.response.body)); if (output.formId !== input.formId) throw new TypeError('form ID mismatch'); return newQueryBranch('found', output, receipt); }
    catch { return invalid('getForm', emptyForm, receipt, 'provider returned an invalid form'); }
  }
}
/** List one bounded page of form responses. */
export class ListResponsesOperation implements Query<ListResponsesInput, ResponsePage> {
  /** Bind the provider client. */ constructor(private readonly client: GoogleFormsClient) {}
  /** Return the manifest definition. */ definition() { return ListResponsesDefinition; }
  /** Invoke one provider listing. */
  async invoke(call: Call, input: ListResponsesInput): Promise<QueryAttempt<ResponsePage>> {
    if (!idPattern.test(input.formId)) return defect('listResponses', emptyPage, 'formId must be a Google Forms form ID');
    if (input.submittedAfter && input.submittedAtOrAfter) return defect('listResponses', emptyPage, 'set submittedAfter or submittedAtOrAfter, not both');
    if (Buffer.byteLength(input.pageToken ?? '', 'utf8') > 4096 || Array.from(input.pageToken ?? '').some(character => character.codePointAt(0)! <= 32 || character.codePointAt(0) === 127)) return defect('listResponses', emptyPage, 'pageToken is not a Google Forms page token');
    const pageSize = input.pageSize || this.client.responsePageSize;
    if (!Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 1000) return defect('listResponses', emptyPage, 'pageSize must be from 1 to 1000, or 0 for the connection default');
    const query: Record<string, string> = {};
    try { if (input.submittedAfter || input.submittedAtOrAfter) query.filter = `timestamp ${input.submittedAfter ? '>' : '>='} ${goTimestamp(input.submittedAfter || input.submittedAtOrAfter)}`; }
    catch { return defect('listResponses', emptyPage, 'submission time filters must be between years 1 and 9999'); }
    query.pageSize = String(pageSize);
    if (input.pageToken) query.pageToken = input.pageToken;
    const result = await this.client.read(call, 'listResponses', `/forms/${input.formId}/responses?${goQueryEncode(query)}`);
    if ('outcome' in result) return outcome(result.outcome, emptyPage);
    const receipt = this.client.receipt(call, requestId(result.response), input.formId);
    try { const body = parseBody(result.response.body), values = array(body.responses); if (values.length > pageSize) throw new TypeError('oversized page'); const responses = values.map(value => response(value, input.formId)); const ids = responses.map(value => value.responseId); if (new Set(ids).size !== ids.length) throw new TypeError('duplicate response'); const page: ResponsePage = { formId: input.formId, responses }; const nextPageToken = optionalString(body.nextPageToken); if (nextPageToken) page.nextPageToken = nextPageToken; return newQueryBranch('listed', page, receipt); }
    catch { return invalid('listResponses', emptyPage, receipt, 'provider returned an invalid page of responses'); }
  }
}
/** Read one submitted response. */
export class GetResponseOperation implements Query<GetResponseInput, FormResponse> {
  /** Bind the provider client. */ constructor(private readonly client: GoogleFormsClient) {}
  /** Return the manifest definition. */ definition() { return GetResponseDefinition; }
  /** Invoke one provider read. */
  async invoke(call: Call, input: GetResponseInput): Promise<QueryAttempt<FormResponse>> {
    if (!idPattern.test(input.formId) || !idPattern.test(input.responseId)) return defect('getResponse', emptyResponse, 'formId and responseId must be Google Forms IDs');
    const result = await this.client.read(call, 'getResponse', `/forms/${input.formId}/responses/${input.responseId}`);
    if ('outcome' in result) return outcome(result.outcome, emptyResponse);
    const receipt = this.client.receipt(call, requestId(result.response), input.responseId);
    try { const output = response(parseBody(result.response.body), input.formId); if (output.responseId !== input.responseId) throw new TypeError('response ID mismatch'); return newQueryBranch('found', output, receipt); }
    catch { return invalid('getResponse', emptyResponse, receipt, 'provider returned an invalid response record'); }
  }
}
function requestId(value: { headers: Record<string, string | string[] | undefined> }): string { const goog = value.headers['x-goog-request-id'] ?? value.headers['x-request-id']; return Array.isArray(goog) ? goog[0] ?? '' : goog ?? ''; }
