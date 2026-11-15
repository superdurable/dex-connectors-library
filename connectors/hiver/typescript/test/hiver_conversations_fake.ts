// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString } from '@superdurable/connectors-sdk';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { HiverClient } from '../src/client.js';
import { newConnection, type Config, type Connection } from '../src/index.js';

/** The bearer token every request must carry. */
export const APIKey = 'hiver-fake-api-key';
/** The apiBaseURL a fake client uses; the fake answers through HiverClientOptions.send. */
export const APIBaseURL = 'https://hiver.example/v1';

/**
 * A Hiver API stand-in for the conversationReceived Trigger, mirroring the Go connector's internal/hiverfake:
 * bearer authentication, limit and opaque next_page tokens, newest-first or oldest-first order, page tokens
 * that expire with a 400, scripted 429 answers with Retry-After, and one conversation read for getConversation.
 */
export class HiverConversationsFake {
  /** Conversation-list requests, including rejected ones. */ conversationListRequests = 0;
  /** The next_page value of every conversation-list request, "" for a first page. */ readonly pageTokensUsed: string[] = [];
  private readonly inboxes = new Map<string, { email: string; conversations: string[] }>();
  private isOldestFirst = false;
  private tokenEpoch = 0;
  private readonly rateLimits: number[] = [];

  /** Serve one shared inbox with no conversations. */
  constructor(inboxId: string, email: string) { this.inboxes.set(inboxId, { email, conversations: [] }); }

  /** Add another shared inbox with no conversations. */ addInbox(inboxId: string, email: string): void { this.inboxes.set(inboxId, { email, conversations: [] }); }
  /** Append conversations, which newest-first order lists before older ones. */
  addConversations(inboxId: string, ...conversationIds: string[]): void { this.inboxes.get(inboxId)!.conversations.push(...conversationIds); }
  /** Remove a conversation from the list. */
  deleteConversation(inboxId: string, conversationId: string): void {
    const inbox = this.inboxes.get(inboxId)!;
    inbox.conversations = inbox.conversations.filter(id => id !== conversationId);
  }
  /** List the oldest conversation first, so new ones land on the last page. */ listOldestFirst(): void { this.isOldestFirst = true; }
  /** Make every next_page token issued so far answer 400. */ expirePageTokens(): void { this.tokenEpoch++; }
  /** Answer the next list request with 429 and, when positive, Retry-After seconds. */ rateLimitNext(retryAfterSeconds: number): void { this.rateLimits.push(retryAfterSeconds); }

  /** Answer one provider request; pass it as HiverClientOptions.send. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const url = new URL(request.url), headers: Record<string, string> = { 'content-type': 'application/json' };
    const [status, body] = this.answer(request, url, headers);
    return { status, headers, body: Buffer.from(JSON.stringify(body)) };
  };

  private answer(request: ProviderRequest, url: URL, headers: Record<string, string>): [number, unknown] {
    const authorization = (request.headers as Record<string, string> | undefined)?.Authorization;
    if (request.method !== 'GET' || authorization !== `Bearer ${APIKey}`) return [401, { errors: [{ message: 'unauthorized' }] }];
    const parts = url.pathname.replace(/^\/v1\//, '').split('/').filter(Boolean);
    if (parts.length === 1 && parts[0] === 'inboxes') {
      const results = [...this.inboxes].map(([id, inbox]) => ({ id, display_name: 'Support', email: inbox.email, channel_type: 'email', is_authorised: true }));
      return [200, { data: { results, pagination: { next_page: null } } }];
    }
    if (parts.length === 3 && parts[0] === 'inboxes' && parts[2] === 'conversations') return this.answerConversations(url.searchParams, parts[1]!, headers);
    if (parts.length === 4 && parts[0] === 'inboxes' && parts[2] === 'conversations') return this.answerConversation(parts[1]!, parts[3]!);
    return [404, { Message: 'not found' }];
  }

  private answerConversations(query: URLSearchParams, inboxId: string, headers: Record<string, string>): [number, unknown] {
    this.conversationListRequests++;
    const pageToken = query.get('next_page') ?? '';
    this.pageTokensUsed.push(pageToken);
    const retryAfter = this.rateLimits.shift();
    if (retryAfter !== undefined) {
      if (retryAfter > 0) headers['retry-after'] = String(retryAfter);
      return [429, { Message: 'Too Many Requests' }];
    }
    const inbox = this.inboxes.get(inboxId);
    if (!inbox) return [404, { Message: 'inbox not found' }];
    const limit = Number(query.get('limit'));
    if (!Number.isInteger(limit) || limit < 10 || limit > 100) return [400, { Message: 'limit must be 10 to 100' }];
    let offset = 0;
    if (pageToken) {
      const match = /^e(\d+)\.o(\d+)$/.exec(pageToken);
      if (!match || Number(match[1]) !== this.tokenEpoch) return [400, { errors: [{ message: 'invalid page token' }] }];
      offset = Number(match[2]);
    }
    const ordered = this.isOldestFirst ? [...inbox.conversations] : [...inbox.conversations].reverse();
    const end = Math.min(offset + limit, ordered.length);
    const results = ordered.slice(Math.min(offset, ordered.length), end).map(conversationJSON);
    const nextPage = end < ordered.length ? `e${this.tokenEpoch}.o${end}` : null;
    return [200, { data: { results, pagination: { next_page: nextPage } } }];
  }

  private answerConversation(inboxId: string, conversationId: string): [number, unknown] {
    if (!this.inboxes.get(inboxId)?.conversations.includes(conversationId)) return [404, { Message: 'conversation not found' }];
    return [200, { data: [{ ...conversationJSON(conversationId), message_ids: [{ hiver_message_id: `8${conversationId}`, gmail_message_id: `19cfm${conversationId}` }] }] }];
  }
}

function conversationJSON(conversationId: string): Record<string, unknown> {
  return { id: conversationId, status: 'open', tag_ids: [], gmail_thread_id: `19cf${conversationId}`, private_permalink: `https://app.hiverhq.com/conversation/${conversationId}` };
}

/** A HiverClient whose requests the fake answers, spacing requests 1 ms apart. */
export function newFakeClient(fake: HiverConversationsFake, config: Config = {}): HiverClient {
  return new HiverClient({ requestIntervalMilliseconds: 1, ...config }, { resolve: async () => ({ apiKey: new SecretString(APIKey) }) }, { apiBaseURL: APIBaseURL, send: fake.send });
}

/** A Hiver connection named connectionName whose requests the fake answers. */
export function newFakeConnection(fake: HiverConversationsFake, connectionName = 'hiver-support', config: Config = {}): Connection {
  return newConnection(newFakeClient(fake, config), { provider: 'hiver', name: connectionName });
}

/**
 * A loaded project with one Hiver API-key connection and its conversationReceived bindings over in-memory
 * storage; pass the same objects again to model a restarted process that reopens the storage.
 */
export async function newTestProject(connectionName: string, triggerBindings: Array<{ bindingName: string; configuration: Record<string, unknown> }>, restartedObjects?: InMemoryObjectStore, connectionConfiguration: Record<string, unknown> = { requestIntervalMilliseconds: 1, pollInterval: 1_000_000_000 }): Promise<{ project: LoadedProject; objects: InMemoryObjectStore }> {
  const scope = { projectId: 'hiver-trigger-tests', kind: 'live' } as const;
  const objects = restartedObjects ?? new InMemoryObjectStore();
  if (!restartedObjects) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'hiver', connectionName }, 0, new CredentialMaterial({ api_key: APIKey }, undefined, 'apiKey'));
  const project = new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    // Dex Web stores a duration as Go nanoseconds.
    connections: [{ connectorId: 'hiver', connectionName, modulePath: 'hiver', provider: 'hiver', configuration: connectionConfiguration }],
    triggerBindings: triggerBindings.map(binding => ({ connectorId: 'hiver', connectionName, triggerName: 'conversationReceived', ...binding })),
  }, undefined, objects, undefined, undefined);
  return { project, objects };
}
