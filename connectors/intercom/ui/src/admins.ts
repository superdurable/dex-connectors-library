// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  executeFirstAcceptedProviderCommand,
  type ConnectorStudioClient,
  type ConnectorStudioConnection,
} from "@superdurable/dex-connectors-react";

/** IntercomAdmin is one teammate from GET /admins, projected for the admin picker. */
export interface IntercomAdmin {
  id: string;
  name: string;
  email: string;
  isAway: boolean;
}

/** adminListCapability is the backend capability every admin list command declares. */
export const adminListCapability = "intercom.admins-list";

/** adminListCommandIDs maps each region to its manifest command, which pins that region's API host. */
export const adminListCommandIDs = Object.freeze({us: "listAdmins", eu: "listAdminsEU", au: "listAdminsAU"});

type Region = keyof typeof adminListCommandIDs;

const adminIDPattern = /^[0-9]{1,24}$/;

/**
 * adminListCommandOrder returns the connection's own regional command when the host reports the
 * connection configuration. Hosts that do not report it get every region, US first, because an
 * access token works only on its own region's host and commands cannot template a host.
 */
export function adminListCommandOrder(connection: ConnectorStudioConnection): string[] {
  const region = connection.configuration.region;
  if (connection.isConfigurationReported) {
    if (region === undefined || region === "") return [adminListCommandIDs.us];
    if (isRegion(region)) return [adminListCommandIDs[region]];
  }
  return [adminListCommandIDs.us, adminListCommandIDs.eu, adminListCommandIDs.au];
}

/** loadIntercomAdmins lists the workspace's admins through the first regional command the host accepts. */
export async function loadIntercomAdmins(client: ConnectorStudioClient, connection: ConnectorStudioConnection): Promise<IntercomAdmin[]> {
  return parseIntercomAdminList(await executeFirstAcceptedProviderCommand(client, adminListCapability, adminListCommandOrder(connection)));
}

/** parseIntercomAdminList keeps admins with a numeric ID, sorted by name, from an admin.list response. */
export function parseIntercomAdminList(value: Record<string, unknown>): IntercomAdmin[] {
  if (value.type !== undefined && value.type !== "admin.list") throw new Error("Intercom did not return an admin list");
  if (!Array.isArray(value.admins)) throw new Error("Intercom did not return an admin list");
  const admins = value.admins.flatMap((item): IntercomAdmin[] => {
    if (!isRecord(item) || typeof item.id !== "string" || !adminIDPattern.test(item.id)) return [];
    return [{
      id: item.id,
      name: typeof item.name === "string" && item.name.trim() !== "" ? item.name.trim() : item.id,
      email: typeof item.email === "string" ? item.email : "",
      isAway: item.away_mode_enabled === true,
    }];
  });
  return admins.sort((left, right) => left.name.localeCompare(right.name) || left.id.localeCompare(right.id));
}

/** isIntercomAdminID reports whether value is an Intercom admin ID, a string of digits. */
export function isIntercomAdminID(value: string): boolean {
  return adminIDPattern.test(value);
}

function isRegion(value: unknown): value is Region {
  return value === "us" || value === "eu" || value === "au";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
