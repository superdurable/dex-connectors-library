// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  mustTriggerBindingDefinition, newTrigger,
  type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerContext, type TriggerDefinition, type TriggerRunner, type TriggerSource, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';
import { GmailClient, type GmailClientOptions } from './client.js';
import type { GmailConnection } from './connection.js';
import { parseMailbox } from './message.js';
import { ConnectorID } from './models.js';
import { newProjectConnection } from './project.js';
import { MessagePollingTriggerSource, pollMessageTriggerRoutes, replayMessageTriggerRoutes, runMessageTriggerRoutes, type MessageTriggerRoute } from './trigger_source.js';

/** Filters received Gmail messages by sender and text. */
export const MessageMatcherSchema = z.strictObject({
  /** Case-insensitive substring of the subject or snippet, required when non-empty. */ messageContains: z.string().optional(),
  /** Sender addresses allowed to match when non-empty; compared case-insensitively. */ senderEmails: z.array(z.string()).nullable().optional(),
});
/** Sender and text filter of one Gmail message Trigger binding. */
export type MessageMatcher = z.infer<typeof MessageMatcherSchema>;

/** Configuration of messageReceived: an optional Gmail search query and the root-message matcher. */
export const MessageReceivedTriggerConfigurationSchema = z.strictObject({ searchQuery: z.string().optional(), messageMatcher: MessageMatcherSchema.optional() })
  .superRefine((configuration, context) => validateMessageMatcher(configuration.messageMatcher, context));
/** Search query and root-message matcher of one messageReceived binding. */
export type MessageReceivedTriggerConfiguration = z.infer<typeof MessageReceivedTriggerConfigurationSchema>;

/** Configuration of replyReceived: an optional Gmail search query and the reply matcher. */
export const ReplyReceivedTriggerConfigurationSchema = z.strictObject({ searchQuery: z.string().optional(), replyMatcher: MessageMatcherSchema.optional() })
  .superRefine((configuration, context) => validateMessageMatcher(configuration.replyMatcher, context));
/** Search query and reply matcher of one replyReceived binding. */
export type ReplyReceivedTriggerConfiguration = z.infer<typeof ReplyReceivedTriggerConfigurationSchema>;

/** The metadata-only payload of both Gmail Triggers; field names and order match the Go connector. */
export const MessageEventSchema = z.object({
  /** Mailbox that received the message. */ primaryEmail: z.string(),
  /** Gmail message ID. */ messageId: z.string(),
  /** Gmail thread ID. */ threadId: z.string(),
  /** Sender header. */ from: z.string(),
  /** Decoded subject. */ subject: z.string(),
  /** Gmail snippet, omitted when empty. */ snippet: z.string().optional(),
  /** Receive time as RFC 3339 UTC. */ receivedAt: z.string(),
  /** Whether the message has In-Reply-To or References headers. */ isReply: z.boolean(),
});
/** One received Gmail message. */
export type MessageEvent = z.infer<typeof MessageEventSchema>;

/** Manifest definition of messageReceived. */
export const MessageReceivedTriggerDefinition: TriggerDefinition = { trigger: { connectorId: ConnectorID, triggerName: 'messageReceived' }, description: 'Receive a matching message that begins a Gmail thread.' };
/** Manifest definition of replyReceived. */
export const ReplyReceivedTriggerDefinition: TriggerDefinition = { trigger: { connectorId: ConnectorID, triggerName: 'replyReceived' }, description: 'Receive a matching reply in an existing Gmail thread.' };

/** Gmail search query input UI unit. */ export const UIUnitSearchQueryInput = 'searchQueryInput';
/** Search query input output port. */ export const UISearchQueryInputPortQuery = 'query';
/** Email address list input UI unit. */ export const UIUnitEmailListInput = 'emailListInput';
/** Email list input output port. */ export const UIEmailListInputPortEmails = 'emails';
/** Plain text input UI unit. */ export const UIUnitTextInput = 'textInput';
/** Text input output port. */ export const UITextInputPortText = 'text';

/** One static application binding of a Gmail message Trigger. */
export interface GmailTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units for the binding. */ configurationUI: ConnectorConfigurationUI;
}

/** Declare a static messageReceived binding for Flow Definition tooling and Dex Web. */
export function defineMessageReceivedTriggerBinding(config: GmailTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: MessageReceivedTriggerDefinition, ...config });
}

/** Declare a static replyReceived binding for Flow Definition tooling and Dex Web. */
export function defineReplyReceivedTriggerBinding(config: GmailTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: ReplyReceivedTriggerDefinition, ...config });
}

/** Configuration of one standalone Gmail message Trigger. */
export interface GmailMessageTriggerConfig<Configuration> {
  /** Gmail connection bound to a GmailClient. */ connection: GmailConnection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Search query and matcher. */ configuration: Configuration;
  /** Routes accepted events into the application. */ target: TriggerTarget<MessageEvent>;
}

/**
 * Create one messageReceived Trigger that polls on its own. Prefer one MessageTriggerRunner per
 * connection, which delivers every root before any reply.
 */
export function newMessageReceivedTrigger(config: GmailMessageTriggerConfig<MessageReceivedTriggerConfiguration>): TriggerRunner {
  const configuration = MessageReceivedTriggerConfigurationSchema.parse(config.configuration);
  return newStandaloneTrigger(config, MessageReceivedTriggerDefinition, configuration.searchQuery ?? '', configuration.messageMatcher ?? {}, false);
}

/**
 * Create one replyReceived Trigger that polls on its own. Prefer one MessageTriggerRunner per
 * connection, which delivers every root before any reply.
 */
export function newReplyReceivedTrigger(config: GmailMessageTriggerConfig<ReplyReceivedTriggerConfiguration>): TriggerRunner {
  const configuration = ReplyReceivedTriggerConfigurationSchema.parse(config.configuration);
  return newStandaloneTrigger(config, ReplyReceivedTriggerDefinition, configuration.searchQuery ?? '', configuration.replyMatcher ?? {}, true);
}

function newStandaloneTrigger(config: GmailMessageTriggerConfig<unknown>, definition: TriggerDefinition, searchQuery: string, matcher: MessageMatcher, requiresReply: boolean): TriggerRunner {
  const client = gmailClientOf(config.connection);
  if (config.connectionName !== config.connection.reference.name) throw new TypeError(`gmail connector trigger connectionName "${config.connectionName}" must equal its connection's name "${config.connection.reference.name}"`);
  // Like a Go source from a generated per-Trigger factory, the source has no binding name; the runner adds it.
  const source = new MessagePollingTriggerSource(client, config.connection.reference, definition.trigger.triggerName, '', searchQuery, matcher, requiresReply);
  const triggerSource: TriggerSource<MessageEvent> = { run: (context, target) => source.run(context, target) };
  return newTrigger({ definition, binding: { connection: config.connection.reference, trigger: definition.trigger, name: config.bindingName }, source: triggerSource, target: config.target, logger: client.triggerLogger });
}

/** One root-message binding and its application target. */
export interface MessageReceivedTriggerRoute {
  /** Configured binding name. */ bindingName: string;
  /** Search query and root-message matcher. */ configuration: MessageReceivedTriggerConfiguration;
  /** Routes accepted events into the application. */ target: TriggerTarget<MessageEvent>;
}

/** One reply binding and its application target. */
export interface ReplyReceivedTriggerRoute {
  /** Configured binding name. */ bindingName: string;
  /** Search query and reply matcher. */ configuration: ReplyReceivedTriggerConfiguration;
  /** Routes accepted events into the application. */ target: TriggerTarget<MessageEvent>;
}

/** Gmail message Triggers that share one ordered poller. */
export interface MessageTriggerRunnerConfig {
  /** Gmail connection bound to a GmailClient. */ connection: GmailConnection;
  /** Root-message bindings. */ messageReceivedRoutes?: readonly MessageReceivedTriggerRoute[];
  /** Reply bindings. */ replyReceivedRoutes?: readonly ReplyReceivedTriggerRoute[];
}

/** One root-message binding saved in the project configuration and its application target. */
export interface ProjectMessageReceivedTriggerRoute { /** Configured binding name. */ bindingName: string; /** Application target. */ target: TriggerTarget<MessageEvent> }
/** One reply binding saved in the project configuration and its application target. */
export interface ProjectReplyReceivedTriggerRoute { /** Configured binding name. */ bindingName: string; /** Application target. */ target: TriggerTarget<MessageEvent> }

/** Gmail message Trigger bindings selected from the project configuration. */
export interface ProjectMessageTriggerRunnerConfig {
  /** Root-message bindings. */ messageReceivedRoutes?: readonly ProjectMessageReceivedTriggerRoute[];
  /** Reply bindings. */ replyReceivedRoutes?: readonly ProjectReplyReceivedTriggerRoute[];
}

/** Polls every configured Gmail route of one connection and delivers root messages before replies. */
export class MessageTriggerRunner {
  readonly #rootRoutes: readonly MessageTriggerRoute[];
  readonly #replyRoutes: readonly MessageTriggerRoute[];
  readonly #pollIntervalMs: number;

  /** @internal Use newMessageTriggerRunner or newProjectMessageTriggerRunner. */
  constructor(rootRoutes: readonly MessageTriggerRoute[], replyRoutes: readonly MessageTriggerRoute[], pollIntervalMs: number) {
    this.#rootRoutes = rootRoutes;
    this.#replyRoutes = replyRoutes;
    this.#pollIntervalMs = pollIntervalMs;
  }

  /**
   * Replay each route's durable inbox in order, roots first, then poll until the context ends. Each poll
   * lists every reply route before any root route, then delivers every root before any reply, and ends
   * at the first failure. Every later poll retries the failed event before that route's listed messages,
   * even after Gmail stops listing it. Gmail receives a thread's root before any reply to it, so the
   * root of every listed reply is in the later root listing and reaches Dex first.
   *
   * It logs through the client's logger: WARN "gmail poll failed; retrying" with the consecutive failed
   * polls as attempt and the poll interval as delay_ms when a list or read fails, WARN "trigger delivery
   * failed; retrying" with the attempt number when a target fails, INFO "trigger delivered after retry"
   * when a later poll delivers that message, DEBUG "trigger event ignored" with its reason, and the
   * durable inbox records. It rejects with TriggerDeliveryAbortedError once the context ends, and when
   * an inbox replay fails.
   */
  async run(context: TriggerContext): Promise<void> {
    await replayMessageTriggerRoutes(context, [...this.#rootRoutes, ...this.#replyRoutes]);
    await runMessageTriggerRoutes(context, () => pollMessageTriggerRoutes(context, this.#rootRoutes, this.#replyRoutes), this.#pollIntervalMs);
  }
}

/** Create one ordered runner for every configured Gmail message Trigger route of a connection; throws on invalid wiring. */
export function newMessageTriggerRunner(config: MessageTriggerRunnerConfig): MessageTriggerRunner {
  const client = gmailClientOf(config.connection);
  const bindingKeys = new Set<string>();
  const route = (triggerName: string, bindingName: string, target: TriggerTarget<MessageEvent>, searchQuery: string | undefined, matcher: MessageMatcher | undefined, requiresReply: boolean): MessageTriggerRoute => {
    const trimmedBindingName = bindingName?.trim();
    if (!trimmedBindingName || !target) throw new TypeError('Gmail message Trigger binding name and target are required');
    const bindingKey = `${triggerName}\0${trimmedBindingName}`;
    if (bindingKeys.has(bindingKey)) throw new TypeError(`Gmail message Trigger binding name "${trimmedBindingName}" is duplicated`);
    bindingKeys.add(bindingKey);
    return { source: new MessagePollingTriggerSource(client, config.connection.reference, triggerName, bindingName, searchQuery ?? '', matcher ?? {}, requiresReply), target };
  };
  const rootRoutes = (config.messageReceivedRoutes ?? []).map(root => {
    const configuration = MessageReceivedTriggerConfigurationSchema.parse(root.configuration);
    return route('messageReceived', root.bindingName, root.target, configuration.searchQuery, configuration.messageMatcher, false);
  });
  const replyRoutes = (config.replyReceivedRoutes ?? []).map(reply => {
    const configuration = ReplyReceivedTriggerConfigurationSchema.parse(reply.configuration);
    return route('replyReceived', reply.bindingName, reply.target, configuration.searchQuery, configuration.replyMatcher, true);
  });
  if (rootRoutes.length + replyRoutes.length === 0) throw new TypeError('Gmail message Trigger routes are required');
  return new MessageTriggerRunner(rootRoutes, replyRoutes, client.triggerTransport().http.settings().pollIntervalMs);
}

/**
 * Open connectionName from the loaded project, load the selected bindings' saved configuration, and
 * create one durable, ordered runner. Each route keeps acknowledged events in its binding's project
 * Trigger inbox until its target consumes them, so a restarted or replaced replica replays them. A
 * route whose binding is not configured throws. The options' logger also applies to those inboxes.
 */
export function newProjectMessageTriggerRunner(project: LoadedProject, connectionName: string, config: ProjectMessageTriggerRunnerConfig, options: GmailClientOptions = {}): MessageTriggerRunner {
  const connection = newProjectConnection(project, connectionName, options);
  const client = gmailClientOf(connection);
  const durable = <Configuration>(triggerName: string, bindingName: string, target: TriggerTarget<MessageEvent>, schema: z.ZodType<Configuration>) => {
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName };
    let configuration: Configuration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, schema); }
    catch (error) { throw new Error(`gmail ${triggerName} binding "${bindingName}" configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
    const durableTarget = newDurableTriggerTarget(project.triggerInbox(key), key, target, { logger: client.triggerLogger, decodePayload: (_payloadJSON, payloadBytes) => MessageEventSchema.parse(parseTriggerPayloadJSON(payloadBytes)) });
    return { bindingName, configuration, target: durableTarget };
  };
  return newMessageTriggerRunner({
    connection,
    messageReceivedRoutes: (config.messageReceivedRoutes ?? []).map(route => durable('messageReceived', route.bindingName, route.target, MessageReceivedTriggerConfigurationSchema)),
    replyReceivedRoutes: (config.replyReceivedRoutes ?? []).map(route => durable('replyReceived', route.bindingName, route.target, ReplyReceivedTriggerConfigurationSchema)),
  });
}

function gmailClientOf(connection: GmailConnection): GmailClient {
  if (!(connection?.operations instanceof GmailClient)) throw new TypeError('Gmail Triggers require a connection bound to a GmailClient');
  return connection.operations;
}

function validateMessageMatcher(matcher: MessageMatcher | undefined, context: z.RefinementCtx): void {
  const seen = new Set<string>();
  for (const sender of matcher?.senderEmails ?? []) {
    const address = parseMailbox(sender)?.address;
    if (!address) { context.addIssue({ code: 'custom', message: 'Gmail sender email is invalid' }); return; }
    const canonical = address.toLowerCase();
    if (seen.has(canonical)) { context.addIssue({ code: 'custom', message: 'Gmail sender emails must be unique' }); return; }
    seen.add(canonical);
  }
}
