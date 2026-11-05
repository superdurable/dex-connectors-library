// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { CustomFieldValueInput, Task, UserReference, TaskReference, ProjectReference, SectionReference, WorkspaceReference, CustomFieldValue, EnumOptionReference } from './generated/models.js';

/** Fields returned by Asana's bounded task list endpoint. */
export const taskSummaryFields = ['name', 'resource_subtype', 'completed', 'completed_at', 'assignee.name', 'due_on', 'due_at', 'start_on', 'memberships.project.name', 'memberships.section.name', 'parent.name', 'permalink_url', 'created_at', 'modified_at'].join(',');
/** Extra task detail fields shared by getTask and updateTask. */
export const taskDetailFields = `${taskSummaryFields},workspace.name,notes,custom_fields.name,custom_fields.resource_subtype,custom_fields.display_value,custom_fields.text_value,custom_fields.number_value,custom_fields.enum_value.name,custom_fields.multi_enum_values.name`;
/** Compact fields returned after task creation. */
export const createdTaskFields = 'gid,name,permalink_url,created_at';
/** Compact fields returned after comment creation. */
export const createdStoryFields = 'gid,created_at,created_by.gid,created_by.name,resource_subtype';
const gidPattern = /^[0-9]{1,64}$/;

/** Validate one Asana decimal GID before using it in a path or body. */
export function gid(value: string | undefined, field: string): string {
  const trimmed = value?.trim() ?? '';
  if (!gidPattern.test(trimmed)) throw new TypeError(`${field} must be an Asana gid, a decimal string such as 1204567890123456`);
  return trimmed;
}

/** Validate a present optional GID. */
export function optionalGid(value: string | undefined, field: string): string { return value?.trim() ? gid(value, field) : ''; }

/** Validate a YYYY-MM-DD calendar date. */
export function dueDate(value: string, field: string): string {
  const trimmed = value.trim();
  const date = new Date(`${trimmed}T00:00:00Z`);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(trimmed) || Number.isNaN(date.getTime()) || date.toISOString().slice(0, 10) !== trimmed) throw new TypeError(`${field} must be a YYYY-MM-DD date such as 2026-10-15`);
  return trimmed;
}

/** Validate an Asana assignee selector. */
export function assignee(value: string, field: string): string {
  const trimmed = value.trim();
  if (trimmed === 'me' || gidPattern.test(trimmed) || (Buffer.byteLength(trimmed) <= 254 && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmed))) return trimmed;
  throw new TypeError(`${field} must be me, a user gid, or an email address`);
}

/** Validate plain text without sending markup or control characters. */
export function plainText(value: string, field: string, required: boolean): string {
  if (required && !value.trim()) throw new TypeError(`${field} is required`);
  if ([...value].length > 65536) throw new TypeError(`${field} cannot exceed 65536 characters`);
  if ([...value].some(character => { const code = character.codePointAt(0)!; return (code < 32 && code !== 9 && code !== 10 && code !== 13) || code === 127; })) throw new TypeError(`${field} cannot contain control characters`);
  return value;
}

/** Validate a one-line task name. */
export function taskName(value: string): string {
  const name = value.trim();
  if (!name) throw new TypeError('name is required');
  if ([...name].length > 1024) throw new TypeError('name cannot exceed 1024 characters');
  if ([...name].some(character => { const code = character.codePointAt(0)!; return code < 32 || code === 127; })) throw new TypeError('name must be one line without control characters');
  return name;
}

/** Encode custom field replacements, preserving false, zero, and empty selections. */
export function customFieldValues(inputs?: readonly CustomFieldValueInput[]): Record<string, unknown> | undefined {
  if (!inputs?.length) return undefined;
  if (inputs.length > 20) throw new TypeError('customFields accepts at most 20 values');
  const result: Record<string, unknown> = {};
  for (const input of inputs) {
    const fieldId = gid(input.customFieldId, 'customFieldId');
    if (fieldId in result) throw new TypeError(`custom field ${fieldId} is set twice`);
    const selectors = [input.text !== undefined, input.number !== undefined, !!input.enumOptionId?.trim(), input.multiEnumOptionIds !== undefined];
    if (selectors.filter(Boolean).length !== 1) throw new TypeError(`custom field ${fieldId}: set exactly one of text, number, enumOptionId, and multiEnumOptionIds`);
    if (input.text !== undefined) result[fieldId] = plainText(input.text, 'text', false);
    else if (input.number !== undefined) {
      if (!Number.isFinite(input.number)) throw new TypeError(`custom field ${fieldId}: number must be finite`);
      result[fieldId] = input.number;
    } else if (input.multiEnumOptionIds !== undefined) {
      if (input.multiEnumOptionIds.length > 50) throw new TypeError(`custom field ${fieldId}: multiEnumOptionIds accepts at most 50 options`);
      result[fieldId] = input.multiEnumOptionIds.map(value => gid(value, 'multiEnumOptionIds'));
    } else result[fieldId] = gid(input.enumOptionId, 'enumOptionId');
  }
  return result;
}

function record(value: unknown): Record<string, unknown> { return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}; }
function string(value: unknown): string { return typeof value === 'string' ? value : ''; }
function named<T extends UserReference | TaskReference | ProjectReference | SectionReference | WorkspaceReference | EnumOptionReference>(value: unknown): T | undefined {
  const resource = record(value);
  if (!gidPattern.test(string(resource.gid))) return undefined;
  return { id: string(resource.gid), ...(string(resource.name) ? { name: string(resource.name) } : {}) } as T;
}
function time(value: unknown): string {
  const text = string(value);
  if (!text) return '0001-01-01T00:00:00Z';
  const match = /^(\d{4})-(\d{2})-(\d{2})T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.exec(text);
  if (!match) throw new TypeError('timestamp is not an ISO 8601 date-time');
  const calendarDate = new Date(0);
  calendarDate.setUTCFullYear(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
  if (calendarDate.getUTCFullYear() !== Number(match[1]) || calendarDate.getUTCMonth() + 1 !== Number(match[2]) || calendarDate.getUTCDate() !== Number(match[3])) throw new TypeError('timestamp is not an ISO 8601 date-time');
  const parsed = new Date(text);
  if (Number.isNaN(parsed.getTime())) throw new TypeError('timestamp is not an ISO 8601 date-time');
  return parsed.toISOString().replace(/\.000Z$/, 'Z');
}
function optionalTime(value: unknown): string | undefined { return value ? time(value) : undefined; }
function optionalDate(value: unknown): string | undefined { return value ? dueDate(string(value), 'date') : undefined; }

/** Decode one untrusted Asana task into the generated public model. */
export function decodeTask(value: unknown, detail: boolean): Task {
  const resource = record(value);
  const id = gid(string(resource.gid), 'task gid');
  const task: Task = {
    id, name: string(resource.name), isCompleted: resource.completed === true,
    createdAt: time(resource.created_at), modifiedAt: time(resource.modified_at),
  };
  if (string(resource.resource_subtype)) task.resourceSubtype = string(resource.resource_subtype);
  if (optionalTime(resource.completed_at)) task.completedAt = optionalTime(resource.completed_at);
  if (named<UserReference>(resource.assignee)) task.assignee = named<UserReference>(resource.assignee);
  if (optionalDate(resource.due_on)) task.dueOn = optionalDate(resource.due_on);
  if (optionalTime(resource.due_at)) task.dueAt = optionalTime(resource.due_at);
  if (optionalDate(resource.start_on)) task.startOn = optionalDate(resource.start_on);
  if (named<TaskReference>(resource.parent)) task.parent = named<TaskReference>(resource.parent);
  if (string(resource.permalink_url)) task.permalinkUrl = string(resource.permalink_url);
  const memberships: Task['memberships'] = [];
  for (const raw of Array.isArray(resource.memberships) ? resource.memberships : []) {
    const item = record(raw);
    const project = named<ProjectReference>(item.project);
    if (!project || memberships.length === 20) continue;
    const section = named<SectionReference>(item.section);
    memberships.push({ project, ...(section ? { section } : {}) });
  }
  if (memberships.length) task.memberships = memberships;
  if (detail) {
    if (named<WorkspaceReference>(resource.workspace)) task.workspace = named<WorkspaceReference>(resource.workspace);
    const notes = string(resource.notes);
    if (notes) {
      task.notes = [...notes].slice(0, 65536).join('');
      if ([...notes].length > 65536) task.isNotesTruncated = true;
    }
    const fields: CustomFieldValue[] = [];
    for (const raw of Array.isArray(resource.custom_fields) ? resource.custom_fields : []) {
      const field = record(raw);
      if (!gidPattern.test(string(field.gid)) || fields.length === 50) continue;
      const view: CustomFieldValue = { id: string(field.gid) };
      if (string(field.name)) view.name = string(field.name);
      if (string(field.resource_subtype)) view.type = string(field.resource_subtype);
      if (typeof field.display_value === 'string') view.displayValue = field.display_value;
      if (typeof field.text_value === 'string') view.textValue = field.text_value;
      if (typeof field.number_value === 'number') view.numberValue = field.number_value;
      if (named<EnumOptionReference>(field.enum_value)) view.enumOption = named<EnumOptionReference>(field.enum_value);
      const options = (Array.isArray(field.multi_enum_values) ? field.multi_enum_values : []).map(value => named<EnumOptionReference>(value)).filter((value): value is EnumOptionReference => value !== undefined);
      if (options.length) view.multiEnumOptions = options;
      fields.push(view);
    }
    if (fields.length) task.customFields = fields;
  }
  return task;
}
