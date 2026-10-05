// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export interface PipedriveUser { id: string; name: string; email?: string; }
export interface PipedriveStage { id: string; name: string; }
export interface PipedrivePipeline { id: string; name: string; stages: PipedriveStage[]; }
export interface PipedriveCustomField { key: string; name: string; fieldType: string; isText: boolean; }
export interface PipedrivePage<T> { items: T[]; nextCursor: string; }

export const pipedriveObjectTypes = [
  {id: "persons", label: "Persons", command: "listPersonFields"},
  {id: "organizations", label: "Organizations", command: "listOrganizationFields"},
  {id: "deals", label: "Deals", command: "listDealFields"},
] as const;

export const apiTokenAuthMethodID = "api-token";
export const oauthAuthMethodID = "pipedrive-oauth";

const numericIDPattern = /^[1-9][0-9]{0,18}$/;
const customFieldKeyPattern = /^[0-9a-f]{40}$/;
// textFieldTypes are the custom field types whose value is one JSON string.
const textFieldTypes = new Set(["varchar", "text", "varchar_auto", "phone", "address"]);

/** parsePipedriveUsers keeps active users with numeric IDs from GET /v1/users. */
export function parsePipedriveUsers(value: Record<string, unknown>): PipedriveUser[] {
  const data = successfulData(value, "Pipedrive returned no user list");
  if (!Array.isArray(data)) throw new Error("Pipedrive returned no user list");
  return data.flatMap((user) => {
    const id = numericID(record(user) ? user.id : undefined);
    if (!record(user) || id === "" || user.active_flag === false) return [];
    const email = text(user.email) ? user.email : undefined;
    return [{id, name: text(user.name) ? user.name : email ?? id, email}];
  });
}

/** parsePipedrivePipelinePage keeps pipelines that are not deleted from one GET /api/v2/pipelines page. */
export function parsePipedrivePipelinePage(value: Record<string, unknown>): PipedrivePage<{id: string; name: string; order: number}> {
  return parsePage(value, "Pipedrive returned no pipelines", (pipeline) => {
    const id = numericID(pipeline.id);
    if (id === "" || pipeline.is_deleted === true) return [];
    return [{id, name: text(pipeline.name) ? pipeline.name : id, order: order(pipeline)}];
  });
}

/** parsePipedriveStagePage keeps stages that are not deleted from one GET /api/v2/stages page. */
export function parsePipedriveStagePage(value: Record<string, unknown>): PipedrivePage<{id: string; name: string; pipelineId: string; order: number}> {
  return parsePage(value, "Pipedrive returned no stages", (stage) => {
    const id = numericID(stage.id);
    const pipelineId = numericID(stage.pipeline_id);
    if (id === "" || pipelineId === "" || stage.is_deleted === true) return [];
    return [{id, name: text(stage.name) ? stage.name : id, pipelineId, order: order(stage)}];
  });
}

/** joinPipelineStages groups stages under their pipelines, both in Pipedrive's order_nr order. */
export function joinPipelineStages(
  pipelines: {id: string; name: string; order: number}[],
  stages: {id: string; name: string; pipelineId: string; order: number}[],
): PipedrivePipeline[] {
  return [...pipelines].sort(byOrder).map((pipeline) => ({
    id: pipeline.id, name: pipeline.name,
    stages: stages.filter((stage) => stage.pipelineId === pipeline.id).sort(byOrder).map(({id, name}) => ({id, name})),
  }));
}

/** parsePipedriveFieldPage keeps custom fields, identified by their 40-character keys, from one Fields API v2 page. */
export function parsePipedriveFieldPage(value: Record<string, unknown>): PipedrivePage<PipedriveCustomField> {
  return parsePage(value, "Pipedrive returned no fields", (field) => {
    if (field.is_custom_field !== true || !text(field.field_code) || !customFieldKeyPattern.test(field.field_code)) return [];
    const fieldType = text(field.field_type) ? field.field_type : "unknown";
    return [{key: field.field_code, name: text(field.field_name) ? field.field_name : field.field_code, fieldType, isText: textFieldTypes.has(fieldType)}];
  });
}

/** isPipedriveID reports whether value is blank or a positive decimal Pipedrive ID. */
export function isPipedriveID(value: string): boolean {
  return value === "" || numericIDPattern.test(value);
}

/** isPipedriveCustomFieldKey reports whether value is blank or a 40-character custom field key. */
export function isPipedriveCustomFieldKey(value: string): boolean {
  return value === "" || customFieldKeyPattern.test(value);
}

function parsePage<T>(value: Record<string, unknown>, fallback: string, convert: (item: Record<string, unknown>) => T[]): PipedrivePage<T> {
  const data = successfulData(value, fallback);
  if (!Array.isArray(data)) throw new Error(fallback);
  const additional = record(value.additional_data) ? value.additional_data : {};
  return {items: data.filter(record).flatMap(convert), nextCursor: text(additional.next_cursor) ? additional.next_cursor : ""};
}

// successfulData returns data, naming only Pipedrive's numeric errorCode, never its message text.
function successfulData(value: Record<string, unknown>, fallback: string): unknown {
  if (value.success === true) return value.data;
  const code = typeof value.errorCode === "number" && Number.isInteger(value.errorCode) ? ` (HTTP ${value.errorCode})` : "";
  throw new Error(fallback + code);
}

function numericID(value: unknown): string {
  const id = typeof value === "number" && Number.isInteger(value) ? String(value) : typeof value === "string" ? value : "";
  return numericIDPattern.test(id) ? id : "";
}

function order(value: Record<string, unknown>): number {
  return typeof value.order_nr === "number" ? value.order_nr : Number.MAX_SAFE_INTEGER;
}

function byOrder(left: {order: number}, right: {order: number}): number { return left.order - right.order; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.trim().length > 0; }
