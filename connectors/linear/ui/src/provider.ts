// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { LinearTeam } from "./units.js";

export interface LinearTeamList { teams: LinearTeam[]; isTruncated: boolean; }

const linearUUIDPattern = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;

/** parseLinearTeamList keeps teams with a UUID, a key, and a name from the listTeams GraphQL answer. */
export function parseLinearTeamList(value: Record<string, unknown>): LinearTeamList {
  // Linear's error message text never reaches the frame.
  if (array(value.errors).length > 0) throw new Error("Linear returned an error instead of the team list");
  const data = record(value.data) ? value.data : {};
  if (!record(data.teams)) throw new Error("Linear did not return a team list");
  const teams = array(data.teams.nodes).flatMap((node) => {
    if (!record(node) || !text(node.id) || !isLinearTeamUUID(node.id) || !text(node.key) || !text(node.name)) return [];
    return [{id: node.id.toLowerCase(), key: node.key, name: node.name}];
  });
  const pageInfo = record(data.teams.pageInfo) ? data.teams.pageInfo : {};
  return {teams, isTruncated: pageInfo.hasNextPage === true};
}

/** isLinearTeamUUID reports whether a manually entered value has the shape of a Linear team ID. */
export function isLinearTeamUUID(value: string): boolean {
  return linearUUIDPattern.test(value);
}

function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
