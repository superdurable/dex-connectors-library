// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/** AsanaResource is one workspace, project, or section from an Asana list, identified by gid. */
export interface AsanaResource {
  id: string;
  name: string;
}

/** AsanaResourcePage is one page of an Asana list and the offset of the next page, or "" on the last page. */
export interface AsanaResourcePage {
  resources: AsanaResource[];
  nextOffset: string;
}

const gidPattern = /^[0-9]{1,64}$/;
const maximumOffsetLength = 4096;

/** isAsanaGID accepts an Asana gid, a decimal string, the same rule the Go connector enforces. */
export function isAsanaGID(value: string): boolean { return gidPattern.test(value.trim()); }

/**
 * parseResourcePage reads an Asana {data, next_page} list response. Entries without a valid gid are dropped,
 * a missing name falls back to the gid, and an unusable next_page offset ends the list.
 */
export function parseResourcePage(value: Record<string, unknown>): AsanaResourcePage {
  const resources = array(value.data).flatMap((item): AsanaResource[] => {
    if (!record(item) || !text(item.gid) || !isAsanaGID(item.gid)) return [];
    return [{id: item.gid, name: text(item.name) ? item.name : item.gid}];
  });
  const nextPage = record(value.next_page) ? value.next_page : undefined;
  const offset = nextPage && text(nextPage.offset) && nextPage.offset.length <= maximumOffsetLength ? nextPage.offset : "";
  return {resources, nextOffset: offset};
}

/** offsetParameters adds the offset only after the first page, so the first request sends no empty offset. */
export function offsetParameters(offset: string, parameters: Record<string, string> = {}): Record<string, string> {
  return offset === "" ? parameters : {...parameters, offset};
}

function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
