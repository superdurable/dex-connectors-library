// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { useEffect, useMemo, useState } from "react";

import {
  connectorConnectionWriteCapability,
  connectorStudioHostAPIVersion,
  isConnectorStudioMessage,
  type ConnectorConnectionView,
  type ConnectorStudioConnectionSave,
  type ConnectorStudioCommand,
  type ConnectorStudioCommandResult,
  type ConnectorStudioHostReady,
} from "./host-api.js";

/**
 * ConnectorStudioConnection is the ready message's connection as
 * ConnectorStudioClient reports it: authMethodIds and configuration are always
 * present, and empty when the host omits them, as hosts that predate them do.
 */
export interface ConnectorStudioConnection extends ConnectorConnectionView {
  /**
   * authMethodIds holds the auth method ID the connection selected, or is []
   * before one is chosen and when the host omits it.
   */
  authMethodIds: string[];
  /** configuration is the connection's stored non-secret configuration, or {} when the host omits it. */
  configuration: Record<string, unknown>;
  /** storedCredentialFields names the credential fields with a saved value, or is [] when the host omits it. */
  storedCredentialFields: string[];
  /**
   * isConfigurationReported reports whether the host sent configuration. It is
   * false on hosts that predate it, where configuration is empty even when the
   * connection has saved values.
   */
  isConfigurationReported: boolean;
}

/** ConnectorStudioClientReady is the host's ready message with its connection read as ConnectorStudioConnection. */
export interface ConnectorStudioClientReady extends ConnectorStudioHostReady {
  connection: ConnectorStudioConnection;
}

/** ConnectorStudioCommandError is a command the host rejected, with its coded reason. */
export class ConnectorStudioCommandError extends Error {
  /** code is the host or provider error code, such as COMMAND_UNSUPPORTED. */
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "ConnectorStudioCommandError";
    this.code = code;
  }
}

/** ConnectorStudioClient is a bundle's connection to the Dex Web host for one session. */
export interface ConnectorStudioClient {
  /**
   * ready is the host's latest ready message, or undefined until the host sends
   * it. Its connection always carries authMethodIds and configuration, empty
   * when the host omits them.
   */
  ready: ConnectorStudioClientReady | undefined;
  /** busy reports whether any command is waiting for a result. */
  busy: boolean;
  /**
   * send posts one command and resolves with its result value. It rejects with
   * ConnectorStudioCommandError when the host lacks the capability or rejects the command.
   */
  send(command: ConnectorStudioCommand["command"], capability: string, input?: Record<string, unknown>): Promise<Record<string, unknown>>;
  /**
   * executeProviderCommand runs one manifest-declared provider command through
   * the host broker. The host injects the connection's stored credential. A
   * setup surface granted connection.write may pass draftCredentials, values
   * the user typed but has not saved yet; the host sends the command's own
   * credential field from them instead, so a model list can load before Save.
   */
  executeProviderCommand(
    commandId: string, capability: string, parameters?: Record<string, string>, draftCredentials?: Record<string, string>,
  ): Promise<Record<string, unknown>>;
}

interface PendingCommand {
  resolve(value: Record<string, unknown>): void;
  reject(error: Error): void;
}

/**
 * useConnectorStudioClient listens for the host's ready message and command
 * results for connectorId. Messages from any other window, connector, or
 * session are ignored. The bundle never receives credential values.
 */
export function useConnectorStudioClient(connectorId: string): ConnectorStudioClient {
  const [ready, setReady] = useState<ConnectorStudioClientReady>();
  const [pendingCount, setPendingCount] = useState(0);
  const pending = useMemo(() => new Map<string, PendingCommand>(), []);

  useEffect(() => {
    const receive = (event: MessageEvent<unknown>) => {
      if (event.source !== window.parent || !isConnectorStudioMessage(event.data) || event.data.connectorId !== connectorId) return;
      if (event.data.type === "connector.host.ready") {
        setReady(fillOmittedConnectionContext(event.data));
        return;
      }
      if (event.data.type !== "connector.command.result" || !ready || event.data.sessionNonce !== ready.sessionNonce) return;
      const result = event.data as ConnectorStudioCommandResult;
      const request = pending.get(result.requestId);
      if (!request) return;
      pending.delete(result.requestId);
      setPendingCount(pending.size);
      if (result.ok) request.resolve(result.value ?? {});
      else request.reject(new ConnectorStudioCommandError(result.error?.code ?? "COMMAND_FAILED", result.error?.message ?? "Connector command failed"));
    };
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, [connectorId, pending, ready]);

  const send: ConnectorStudioClient["send"] = (command, capability, input) => new Promise((resolve, reject) => {
    if (!ready || !ready.capabilities.includes(capability)) {
      reject(new ConnectorStudioCommandError("CAPABILITY_UNAVAILABLE", `This Dex Web does not grant ${capability}.`));
      return;
    }
    const requestId = crypto.randomUUID();
    pending.set(requestId, {resolve, reject});
    setPendingCount(pending.size);
    window.parent.postMessage({
      type: "connector.command", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: ready.sessionNonce,
      connectorId, requestId, command, input,
    } satisfies ConnectorStudioCommand, "*");
  });

  return {
    ready,
    busy: pendingCount > 0,
    send,
    executeProviderCommand: (commandId, capability, parameters = {}, draftCredentials) =>
      send("provider.command.execute", capability, {
        commandId, parameters, ...(draftCredentials === undefined ? {} : {credentials: draftCredentials}),
      }),
  };
}

/**
 * saveConnectorConnection stores the whole connection in one host write: its
 * configuration and new or kept credentials. It requires connection.write and
 * rejects with ConnectorStudioCommandError carrying the host's message when the
 * write fails, for example when a required field is missing.
 */
export function saveConnectorConnection(client: ConnectorStudioClient, save: ConnectorStudioConnectionSave): Promise<Record<string, unknown>> {
  return client.send("connection.save", connectorConnectionWriteCapability, {...save});
}

/**
 * withDraftCredentials returns client with draftCredentials attached to every
 * provider command, so existing model-list loaders run against a key the user
 * typed but has not saved. An empty record leaves the stored credential in use.
 */
export function withDraftCredentials(client: ConnectorStudioClient, draftCredentials: Record<string, string>): ConnectorStudioClient {
  if (Object.keys(draftCredentials).length === 0) return client;
  return {
    ...client,
    executeProviderCommand: (commandId, capability, parameters = {}) =>
      client.executeProviderCommand(commandId, capability, parameters, draftCredentials),
  };
}

function fillOmittedConnectionContext(ready: ConnectorStudioHostReady): ConnectorStudioClientReady {
  const {connection} = ready;
  return {
    ...ready,
    connection: {
      ...connection,
      authMethodIds: connection.authMethodIds ?? [],
      configuration: connection.configuration ?? {},
      storedCredentialFields: connection.storedCredentialFields ?? [],
      isConfigurationReported: connection.configuration !== undefined,
    },
  };
}

/** ProviderPage is one page of provider items and the cursor for the next page, empty at the end. */
export interface ProviderPage<T> {
  items: T[];
  nextCursor: string;
}

/**
 * collectProviderPages fetches pages until nextCursor is empty, a cursor
 * repeats, or maxPages pages have been read, and returns every item in order.
 * isTruncated reports that maxPages stopped the loop before the last page.
 */
export async function collectProviderPages<T>(
  fetchPage: (cursor: string) => Promise<ProviderPage<T>>,
  maxPages = 20,
): Promise<{items: T[]; isTruncated: boolean}> {
  const items: T[] = [];
  const seenCursors = new Set<string>();
  let cursor = "";
  for (let page = 0; page < maxPages; page++) {
    const result = await fetchPage(cursor);
    items.push(...result.items);
    if (result.nextCursor === "" || seenCursors.has(result.nextCursor)) return {items, isTruncated: false};
    seenCursors.add(result.nextCursor);
    cursor = result.nextCursor;
  }
  return {items, isTruncated: true};
}
