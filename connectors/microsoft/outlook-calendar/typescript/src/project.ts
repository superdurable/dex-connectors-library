// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
import type { LoadedProject } from "@superdurable/connectors-sdk/projectconfig";
import { ConnectionRefSchema } from "@superdurable/connectors-sdk";
import {
  newProjectCredentialProvider,
  newConnection,
  type Connection,
} from "./generated/connection.js";
import { ConnectorID, ProviderID } from "./generated/definitions.js";
import {
  decodeCredentials,
  decodeSharedCredentials,
  type Config,
  type Credentials,
} from "./generated/models.js";
import { Client, type ClientOptions } from "./client.js";
/** Open a statically named Outlook Calendar connection from a loaded Dex project. */
export function newProjectOutlookCalendarConnection(
  project: LoadedProject,
  connectionName: string,
  options: ClientOptions = {},
): Connection {
  if (!project)
    throw new TypeError("Outlook Calendar project configuration is required");
  const reference = ConnectionRefSchema.parse({
    provider: ProviderID,
    name: connectionName,
  });
  const entry = project.configuration.connections.find(
    (item) =>
      item.connectorId === ConnectorID &&
      item.connectionName === connectionName,
  );
  if (!entry || entry.provider !== ProviderID)
    throw new TypeError(
      "Outlook Calendar project connection is not configured",
    );
  const raw = entry.configuration as Record<string, unknown>;
  const config: Config = {
    ...(typeof raw.maxResponseBytes === "number"
      ? { maxResponseBytes: raw.maxResponseBytes }
      : {}),
    ...(typeof raw.tenantId === "string" ? { tenantId: raw.tenantId } : {}),
    ...(typeof raw.mailbox === "string" ? { mailbox: raw.mailbox } : {}),
  };
  const provider = newProjectCredentialProvider(
    project,
    connectionName,
    decodeCredentials,
    encodeCredentials,
    decodeSharedCredentials,
  );
  return newConnection(new Client(config, provider, options), reference);
}
function encodeCredentials(value: Credentials): Record<string, string> {
  return {
    auth_method: value.authMethodId ?? "microsoft-oauth",
    client_id: value.clientId,
    client_secret: value.clientSecret.reveal(),
    ...(value.accessToken ? { access_token: value.accessToken.reveal() } : {}),
    ...(value.refreshToken
      ? { refresh_token: value.refreshToken.reveal() }
      : {}),
  };
}
