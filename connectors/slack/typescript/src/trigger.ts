// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import type { IncomingMessage, ServerResponse } from 'node:http';
import {
  StaticCredentialProvider, TriggerDeliveryAbortedError, createTriggerCall, deliverTrigger, hasDurableTriggerInbox, logTriggerRecord, mustTriggerBindingDefinition, newTrigger, prepareTriggerDelivery,
  throwIfTriggerAborted, waitForTriggerDelay, withTriggerLogAttributes, withTriggerLoggerAttributes,
  type CallID, type ConnectionRef, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerContext, type TriggerDefinition, type TriggerEvent,
  type TriggerLogAttributes, type TriggerLogLevel, type TriggerLogger, type TriggerRunner, type TriggerSource, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import {
  ConnectionNotSelectedError, CredentialBrokerUnavailableError, RelayTokenRenewalDeferredError, RelayedTriggerNotRelayedError, decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type TriggerInboxKey,
} from '@superdurable/connectors-sdk/projectconfig';
import { WebhookEndpoint, WebhookVerificationUnavailableError, type WebhookRequest } from '@superdurable/connectors-sdk/webhooktrigger';
import { z } from 'zod';
import { SlackClient, slackCode, type SlackClientOptions } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import type { Connection } from './generated/connection.js';
import { newProjectConnection } from './project.js';
import { BlockActionsPayloadSchema, ButtonClickEventSchema, ButtonClickedTriggerConfigurationSchema, ButtonClickedTriggerDefinition, blockActionsChannelId, matchButtonClickPayload, type ButtonClickEvent, type ButtonClickedTriggerConfiguration } from './button_trigger.js';
import { redactSocketURL, type SlackSocketConnection } from './socket_mode.js';

/** Filters Slack messages by text and posting user. */
export const MessageMatcherSchema = z.strictObject({
  /** Case-insensitive substring the message must contain when non-empty. */ messageContains: z.string().optional(),
  /** Slack user IDs allowed to post a match when non-empty. */ posterUserIds: z.array(z.string()).nullable().optional(),
});
/** Message text and poster filter. */
export type MessageMatcher = z.infer<typeof MessageMatcherSchema>;

/** Configuration of channelThreadCreated: the channel and the root-message matcher. */
export const ChannelThreadCreatedTriggerConfigurationSchema = z.strictObject({ channelId: z.string(), threadTriggerMatcher: MessageMatcherSchema.optional() })
  .superRefine((configuration, context) => validateTriggerConfiguration(configuration.channelId, configuration.threadTriggerMatcher, false, context));
/** Channel and root-message matcher of one channelThreadCreated binding. */
export type ChannelThreadCreatedTriggerConfiguration = z.infer<typeof ChannelThreadCreatedTriggerConfigurationSchema>;

/** Configuration of threadReplyCreated: the channel and the reply matcher, which requires at least one poster. */
export const ThreadReplyCreatedTriggerConfigurationSchema = z.strictObject({ channelId: z.string(), threadReplyMatcher: MessageMatcherSchema.optional() })
  .superRefine((configuration, context) => validateTriggerConfiguration(configuration.channelId, configuration.threadReplyMatcher, true, context));
/** Channel and reply matcher of one threadReplyCreated binding. */
export type ThreadReplyCreatedTriggerConfiguration = z.infer<typeof ThreadReplyCreatedTriggerConfigurationSchema>;

/** One normalized Slack message event; field names and order match the Go connector. */
export const MessageEventSchema = z.object({
  /** Slack team ID. */ teamId: z.string(), /** Slack channel ID. */ channelId: z.string(), /** Message timestamp. */ timestamp: z.string(),
  /** Parent thread timestamp; equals timestamp for a root message. */ threadTimestamp: z.string(), /** Posting user ID. */ userId: z.string(), /** Message text. */ text: z.string(),
});
/** One normalized Slack message event. */
export type MessageEvent = z.infer<typeof MessageEventSchema>;

/** Manifest definition of channelThreadCreated. */
export const ChannelThreadCreatedTriggerDefinition: TriggerDefinition = { trigger: { connectorId: ConnectorID, triggerName: 'channelThreadCreated' }, description: 'Receive a matching top-level channel message.' };
/** Manifest definition of threadReplyCreated. */
export const ThreadReplyCreatedTriggerDefinition: TriggerDefinition = { trigger: { connectorId: ConnectorID, triggerName: 'threadReplyCreated' }, description: 'Receive a matching thread reply.' };

/** Slack channel picker UI unit, which stores a stable channel ID. */ export const UIUnitChannelPicker = 'channelPicker';
/** Channel picker output port. */ export const UIChannelPickerPortChannelID = 'channelId';
/** Slack member picker UI unit, which stores user IDs. */ export const UIUnitMemberPicker = 'memberPicker';
/** Member picker output port. */ export const UIMemberPickerPortMemberIDs = 'memberIds';
/** Plain text input UI unit. */ export const UIUnitTextInput = 'textInput';
/** Text input output port. */ export const UITextInputPortText = 'text';

/** One static application binding of a Slack message Trigger. */
export interface SlackTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units for the binding. */ configurationUI: ConnectorConfigurationUI;
}

/** Declare a static channelThreadCreated binding for Flow Definition tooling and Dex Web. */
export function defineChannelThreadCreatedTriggerBinding(config: SlackTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: ChannelThreadCreatedTriggerDefinition, ...config });
}

/** Declare a static threadReplyCreated binding for Flow Definition tooling and Dex Web. */
export function defineThreadReplyCreatedTriggerBinding(config: SlackTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: ThreadReplyCreatedTriggerDefinition, ...config });
}

/** Declare a static buttonClicked binding for Flow Definition tooling and Dex Web. */
export function defineButtonClickedTriggerBinding(config: SlackTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: ButtonClickedTriggerDefinition, ...config });
}

/** Path of a connection's relayed Slack events: "/triggers/slack/" and the unpadded base64url connection name. */
export function relayedTriggerPath(connectionName: string): string {
  return relayedTriggerPathPrefix + Buffer.from(connectionName).toString('base64url');
}

/** One root-message binding and its application target. */
export interface ChannelThreadCreatedTriggerRoute {
  /** Configured binding name. */ bindingName: string;
  /** Channel and root-message matcher. */ configuration: ChannelThreadCreatedTriggerConfiguration;
  /** Routes accepted events into the application. */ target: TriggerTarget<MessageEvent>;
}

/** One reply-message binding and its application target. */
export interface ThreadReplyCreatedTriggerRoute {
  /** Configured binding name. */ bindingName: string;
  /** Channel and reply matcher. */ configuration: ThreadReplyCreatedTriggerConfiguration;
  /** Routes accepted events into the application. */ target: TriggerTarget<MessageEvent>;
}

/** One button click binding and its application target. */
export interface ButtonClickedTriggerRoute {
  /** Configured binding name. */ bindingName: string;
  /** Channel, action ID, and clicker filters. */ configuration: ButtonClickedTriggerConfiguration;
  /** Routes accepted clicks into the application. */ target: TriggerTarget<ButtonClickEvent>;
}

/**
 * Slack message and button click Triggers that share one Socket Mode connection. Slack sends each event to
 * only one of an app's open Socket Mode connections, so give every Trigger of a connection to one runner.
 */
export interface MessageTriggerRunnerConfig {
  /** Slack connection from newProjectConnection or newConnection with a SlackClient. */ connection: Connection;
  /** Root-message bindings. */ channelThreadCreatedRoutes?: readonly ChannelThreadCreatedTriggerRoute[];
  /** Reply-message bindings. */ threadReplyCreatedRoutes?: readonly ThreadReplyCreatedTriggerRoute[];
  /**
   * Button click bindings. Clicks arrive only through Socket Mode, so the credential needs an app_token and the
   * Slack app needs Interactivity turned on. With button routes, every route's target, message routes included,
   * must be a durable inbox from newDurableTriggerTarget, so a click can be acknowledged within three seconds
   * without risking its loss; newMessageTriggerRunner throws otherwise. newProjectMessageTriggerRunner always
   * supplies durable inboxes.
   */
  buttonClickedRoutes?: readonly ButtonClickedTriggerRoute[];
}

/** One root-message binding from the project configuration and its application target. */
export interface ProjectChannelThreadCreatedTriggerRoute { /** Configured binding name. */ bindingName: string; /** Application target. */ target: TriggerTarget<MessageEvent> }
/** One reply-message binding from the project configuration and its application target. */
export interface ProjectThreadReplyCreatedTriggerRoute { /** Configured binding name. */ bindingName: string; /** Application target. */ target: TriggerTarget<MessageEvent> }

/** One button click binding from the project configuration and its application target. */
export interface ProjectButtonClickedTriggerRoute { /** Configured binding name. */ bindingName: string; /** Application target. */ target: TriggerTarget<ButtonClickEvent> }

/** Slack message and button click Trigger bindings selected from the project configuration. */
export interface ProjectMessageTriggerRunnerConfig {
  /** Root-message bindings. */ channelThreadCreatedRoutes?: readonly ProjectChannelThreadCreatedTriggerRoute[];
  /** Reply-message bindings. */ threadReplyCreatedRoutes?: readonly ProjectThreadReplyCreatedTriggerRoute[];
  /** Button click bindings, received through Socket Mode only; see MessageTriggerRunnerConfig.buttonClickedRoutes. */ buttonClickedRoutes?: readonly ProjectButtonClickedTriggerRoute[];
}

/** Configuration of one standalone Slack message Trigger. */
export interface SlackMessageTriggerConfig<Configuration> {
  /** Slack connection. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Channel and matcher. */ configuration: Configuration;
  /** Routes accepted events into the application. */ target: TriggerTarget<MessageEvent>;
}

/** Create one channelThreadCreated Trigger with its own Socket Mode connection; prefer one MessageTriggerRunner per connection. */
export function newChannelThreadCreatedTrigger(config: SlackMessageTriggerConfig<ChannelThreadCreatedTriggerConfiguration>): TriggerRunner {
  return newStandaloneTrigger(config, ChannelThreadCreatedTriggerDefinition, ChannelThreadCreatedTriggerConfigurationSchema.parse(config.configuration), false);
}

/** Create one threadReplyCreated Trigger with its own Socket Mode connection; prefer one MessageTriggerRunner per connection. */
export function newThreadReplyCreatedTrigger(config: SlackMessageTriggerConfig<ThreadReplyCreatedTriggerConfiguration>): TriggerRunner {
  return newStandaloneTrigger(config, ThreadReplyCreatedTriggerDefinition, ThreadReplyCreatedTriggerConfigurationSchema.parse(config.configuration), true);
}

/** Configuration of one standalone buttonClicked Trigger. */
export interface SlackButtonClickedTriggerConfig {
  /** Slack connection. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Channel, action ID, and clicker filters. */ configuration: ButtonClickedTriggerConfiguration;
  /** Routes accepted clicks into the application. */ target: TriggerTarget<ButtonClickEvent>;
}

/**
 * Create one buttonClicked Trigger, at parity with Go's generated NewButtonClickedTrigger. A click is
 * acknowledged before delivery, so it needs a durable inbox, which newTrigger cannot pass to its source:
 * run always rejects. Use newProjectMessageTriggerRunner with buttonClickedRoutes instead.
 */
export function newButtonClickedTrigger(config: SlackButtonClickedTriggerConfig): TriggerRunner {
  if (config.connectionName !== config.connection.reference.name) throw new TypeError(`slack connector trigger connectionName "${config.connectionName}" must equal its connection's name "${config.connection.reference.name}"`);
  const client = slackClientOf(config.connection);
  const configuration = ButtonClickedTriggerConfigurationSchema.parse(config.configuration);
  const source = newButtonClickedTriggerSource(client, config.connection.reference, config.bindingName, configuration);
  const triggerSource: TriggerSource<ButtonClickEvent> = {
    run: async (context, target) => {
      if (!hasDurableTriggerInbox(target)) throw new TypeError(buttonClicksRequireDurableInboxesMessage);
      await runSocketModeRoutes(context, { messages: [], buttons: [{ source, target }] });
    },
  };
  const definition = ButtonClickedTriggerDefinition;
  return newTrigger({ definition, binding: { connection: config.connection.reference, trigger: definition.trigger, name: config.bindingName }, source: triggerSource, target: config.target, logger: client.triggerLogger });
}

/**
 * Receives Slack messages for one connection and dispatches matching routes. It uses Socket Mode when the
 * connection's credential has an app_token. A runner from newProjectMessageTriggerRunner whose shared
 * connection has none receives the events its host relays instead, through serveHTTP.
 */
export class MessageTriggerRunner {
  readonly #routes: SocketModeRoutes;
  readonly #relay: RelayedMessageReceiver | undefined;

  /** @internal Use newMessageTriggerRunner or newProjectMessageTriggerRunner. */
  constructor(routes: SocketModeRoutes, relay: RelayedMessageReceiver | undefined) {
    this.#routes = routes;
    this.#relay = relay;
  }

  /**
   * Replay each route's durable inbox in order, roots first, then replies, then button clicks, then receive and dispatch Slack events until
   * the context ends or the runner fails. Before each reconnect it replays the inboxes again, so an event
   * persisted on a failed connection is delivered before any envelope read on the next one.
   *
   * When every route's target is a durable inbox, run acknowledges each envelope once the matching
   * inboxes recorded it and delivers acknowledged events in arrival order separately, so a retrying target
   * does not delay later acknowledgements. Otherwise it delivers each event before reading the next.
   *
   * A runner from newProjectMessageTriggerRunner first reads the connection's credential, retrying with
   * the reconnect backoff while it is unavailable. With an app_token it uses Socket Mode. Without one, on a
   * shared connection, it replays the inboxes, serves relayed events through serveHTTP, and registers the
   * connection name with the host, retrying while the host is unavailable; it rejects when the host
   * refuses the registration, when the scope no longer selects the connection, and for a connection that
   * is not shared. A host relays only Events API messages, so a relayed runner with button routes rejects.
   */
  async run(context: TriggerContext): Promise<void> {
    if (this.#relay && await this.#relay.selectRelayedMode(context)) {
      if (this.#routes.buttons.length > 0) throw new Error(`Slack connection "${this.#relay.connectionName}" has no app_token for Socket Mode, and button clicks reach only Socket Mode; add an app_token or remove the buttonClicked routes`);
      await this.#relay.run(context);
      return;
    }
    await replaySocketModeRoutes(context, this.#routes);
    await runSocketModeRoutes(context, this.#routes);
  }

  /**
   * Receive the Slack events a host relays for the runner's connection, at relayedTriggerPath. It answers
   * 200 once every matching binding's inbox recorded the event, 400 for a request without a valid host
   * signature or with an invalid body, 413 for a body over 1 MiB, 405 for another method, and 503 while
   * the runner does not receive relayed events, such as before run starts, while it replays the inboxes,
   * or when the connection uses Socket Mode. A runner built without a project configuration answers 404.
   */
  async serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void> {
    if (!this.#relay) { request.resume(); response.writeHead(404).end(); return; }
    await this.#relay.endpoint.serveHTTP(request, response);
  }
}

/**
 * Create one runner for every configured Slack message and button click Trigger route of a connection. It
 * throws for button routes unless every route's target is a durable inbox.
 */
export function newMessageTriggerRunner(config: MessageTriggerRunnerConfig): MessageTriggerRunner {
  return new MessageTriggerRunner(socketModeRoutesOf(config), undefined);
}

/**
 * Open connectionName and its configured message and button bindings from the loaded project and create one durable runner,
 * which uses Socket Mode or the host's relay as MessageTriggerRunner.run describes. Mount the runner's
 * serveHTTP at relayedTriggerPath(connectionName). Each route's target receives its events through the
 * binding's durable project inbox, which every replica shares. A route whose binding is not configured
 * throws. The client options' logger also applies to the durable inboxes.
 */
export function newProjectMessageTriggerRunner(project: LoadedProject, connectionName: string, config: ProjectMessageTriggerRunnerConfig, options: SlackClientOptions = {}): MessageTriggerRunner {
  const connection = newProjectConnection(project, connectionName, options);
  const client = slackClientOf(connection);
  const durable = <Event>(triggerName: string, bindingName: string, target: TriggerTarget<Event>, eventSchema: z.ZodType<Event>) => {
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName };
    return { key, target: newDurableTriggerTarget(project.triggerInbox(key), key, target, { logger: client.triggerLogger, decodePayload: (_payloadJSON, payloadBytes) => eventSchema.parse(parseTriggerPayloadJSON(payloadBytes)) }) };
  };
  const decode = <Configuration>(key: TriggerInboxKey, schema: z.ZodType<Configuration>): Configuration => {
    try { return decodeTriggerConfiguration(project.configuration, key, schema); }
    catch (error) { throw new Error(`slack ${key.triggerName} binding "${key.bindingName}" configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
  };
  const routes = socketModeRoutesOf({
    connection,
    channelThreadCreatedRoutes: (config.channelThreadCreatedRoutes ?? []).map(route => {
      const wrapped = durable(ChannelThreadCreatedTriggerDefinition.trigger.triggerName, route.bindingName, route.target, MessageEventSchema);
      return { bindingName: route.bindingName, target: wrapped.target, configuration: decode(wrapped.key, ChannelThreadCreatedTriggerConfigurationSchema) };
    }),
    threadReplyCreatedRoutes: (config.threadReplyCreatedRoutes ?? []).map(route => {
      const wrapped = durable(ThreadReplyCreatedTriggerDefinition.trigger.triggerName, route.bindingName, route.target, MessageEventSchema);
      return { bindingName: route.bindingName, target: wrapped.target, configuration: decode(wrapped.key, ThreadReplyCreatedTriggerConfigurationSchema) };
    }),
    buttonClickedRoutes: (config.buttonClickedRoutes ?? []).map(route => {
      const wrapped = durable(ButtonClickedTriggerDefinition.trigger.triggerName, route.bindingName, route.target, ButtonClickEventSchema);
      return { bindingName: route.bindingName, target: wrapped.target, configuration: decode(wrapped.key, ButtonClickedTriggerConfigurationSchema) };
    }),
  });
  return new MessageTriggerRunner(routes, new RelayedMessageReceiver(project, connection.reference, client, socketModeRoutesIdentity(routes), routes.messages));
}

const relayedTriggerPathPrefix = '/triggers/slack/';
/** The host's registration endpoint on its credential broker's origin. */
const relayedTriggerRegistrationPath = '/internal/slack-triggers/register';
/** Bound of one relayed Events API callback, as the host bounds what Slack sends it. */
const maxRelayedEventBytes = 1 << 20;
/** Stands in for the Socket Mode envelope ID, which a relayed event lacks. */
const relayedEnvelopeID = 'relayed';
/** How often the relayed receiver checks that its source runs before it registers. */
const relayedSourceStartPollMs = 10;

/** Names a Trigger source's connection and binding in credential reads and log records. */
interface TriggerBindingIdentity {
  client: SlackClient;
  connection: ConnectionRef;
  triggerName: string;
  bindingName: string;
  logger: TriggerLogger;
}

/** One binding's source: its identity, channel, matcher, and whether it matches replies or roots. */
interface MessageTriggerSource extends TriggerBindingIdentity {
  channelId: string;
  matcher: MessageMatcher;
  requiresThread: boolean;
}

/** One button binding's source: its identity and filters. */
interface ButtonClickedTriggerSource extends TriggerBindingIdentity { configuration: ButtonClickedTriggerConfiguration }

interface MessageTriggerRoute { source: MessageTriggerSource; target: TriggerTarget<MessageEvent> }
interface ButtonClickedTriggerSourceRoute { source: ButtonClickedTriggerSource; target: TriggerTarget<ButtonClickEvent> }
interface PendingMessageTriggerDelivery { source: MessageTriggerSource; target: TriggerTarget<MessageEvent>; event: TriggerEvent<MessageEvent> }
interface PendingButtonClickedDelivery { source: ButtonClickedTriggerSource; target: TriggerTarget<ButtonClickEvent>; event: TriggerEvent<ButtonClickEvent> }
interface SocketEnvelope { envelopeId: string; type: string; payload: unknown; reason: string }

/** Routes that share one socket, because Slack sends each envelope to only one app connection. */
interface SocketModeRoutes { messages: MessageTriggerRoute[]; buttons: ButtonClickedTriggerSourceRoute[] }
/** One envelope's matched deliveries; at most one list is non-empty. */
interface SocketModeDeliveries { messages: PendingMessageTriggerDelivery[]; buttons: PendingButtonClickedDelivery[] }

/** Slack's interactive acknowledgement deadline; later, the clicker sees an error. */
const slackInteractiveAcknowledgementLimitMs = 3_000;
const buttonClicksRequireDurableInboxesMessage = 'Slack button click Triggers require durable inboxes for every route on their Socket Mode connection: '
  + 'add the binding to buttonClickedRoutes of newProjectMessageTriggerRunner, or give newMessageTriggerRunner newDurableTriggerTarget targets';

/** Return the first route's identity, which names the shared connection. */
function socketModeRoutesIdentity(routes: SocketModeRoutes): TriggerBindingIdentity {
  return routes.messages[0]?.source ?? routes.buttons[0]!.source;
}

/** Report whether every route's target records and replays events, so queued deliveries lose none. */
function hasDurableInboxes(routes: SocketModeRoutes): boolean {
  return routes.messages.every(route => hasDurableTriggerInbox(route.target)) && routes.buttons.every(route => hasDurableTriggerInbox(route.target));
}

function newStandaloneTrigger(config: SlackMessageTriggerConfig<unknown>, definition: TriggerDefinition, configuration: { channelId: string; threadTriggerMatcher?: MessageMatcher | undefined; threadReplyMatcher?: MessageMatcher | undefined }, requiresThread: boolean): TriggerRunner {
  if (config.connectionName !== config.connection.reference.name) throw new TypeError(`slack connector trigger connectionName "${config.connectionName}" must equal its connection's name "${config.connection.reference.name}"`);
  const client = slackClientOf(config.connection);
  const source = newMessageTriggerSource(client, config.connection.reference, definition.trigger.triggerName, config.bindingName, configuration.channelId, (requiresThread ? configuration.threadReplyMatcher : configuration.threadTriggerMatcher) ?? {}, requiresThread);
  const triggerSource: TriggerSource<MessageEvent> = { run: (context, target) => runSocketModeRoutes(context, { messages: [{ source, target }], buttons: [] }) };
  return newTrigger({ definition, binding: { connection: config.connection.reference, trigger: definition.trigger, name: config.bindingName }, source: triggerSource, target: config.target, logger: client.triggerLogger });
}

function socketModeRoutesOf(config: MessageTriggerRunnerConfig): SocketModeRoutes {
  const client = slackClientOf(config.connection);
  const bindingKeys = new Set<string>();
  const routes: MessageTriggerRoute[] = [];
  const reserveBindingName = (triggerName: string, bindingName: string, target: unknown) => {
    const trimmed = bindingName?.trim();
    if (!trimmed || !target) throw new TypeError('Slack message Trigger binding name and target are required');
    if (bindingKeys.has(`${triggerName}\0${trimmed}`)) throw new TypeError(`Slack message Trigger binding name "${trimmed}" is duplicated`);
    bindingKeys.add(`${triggerName}\0${trimmed}`);
  };
  const add = (triggerName: string, bindingName: string, target: TriggerTarget<MessageEvent>, channelId: string, matcher: MessageMatcher | undefined, requiresThread: boolean) => {
    reserveBindingName(triggerName, bindingName, target);
    routes.push({ source: newMessageTriggerSource(client, config.connection.reference, triggerName, bindingName, channelId, matcher ?? {}, requiresThread), target });
  };
  for (const route of config.channelThreadCreatedRoutes ?? []) {
    const configuration = ChannelThreadCreatedTriggerConfigurationSchema.parse(route.configuration);
    add('channelThreadCreated', route.bindingName, route.target, configuration.channelId, configuration.threadTriggerMatcher, false);
  }
  for (const route of config.threadReplyCreatedRoutes ?? []) {
    const configuration = ThreadReplyCreatedTriggerConfigurationSchema.parse(route.configuration);
    add('threadReplyCreated', route.bindingName, route.target, configuration.channelId, configuration.threadReplyMatcher, true);
  }
  const buttons: ButtonClickedTriggerSourceRoute[] = [];
  for (const route of config.buttonClickedRoutes ?? []) {
    reserveBindingName('buttonClicked', route.bindingName, route.target);
    const configuration = ButtonClickedTriggerConfigurationSchema.parse(route.configuration);
    buttons.push({ source: newButtonClickedTriggerSource(client, config.connection.reference, route.bindingName, configuration), target: route.target });
  }
  if (routes.length === 0 && buttons.length === 0) throw new TypeError('Slack message Trigger routes are required');
  const socketModeRoutes = { messages: routes, buttons };
  if (buttons.length > 0 && !hasDurableInboxes(socketModeRoutes)) throw new TypeError(buttonClicksRequireDurableInboxesMessage);
  return socketModeRoutes;
}

function newButtonClickedTriggerSource(client: SlackClient, connection: ConnectionRef, bindingName: string, configuration: ButtonClickedTriggerConfiguration): ButtonClickedTriggerSource {
  const triggerName = ButtonClickedTriggerDefinition.trigger.triggerName;
  return { client, connection, triggerName, bindingName, configuration, logger: withTriggerLoggerAttributes(client.triggerLogger, bindingAttributes(connection, triggerName, bindingName)) };
}

function newMessageTriggerSource(client: SlackClient, connection: ConnectionRef, triggerName: string, bindingName: string, channelId: string, matcher: MessageMatcher, requiresThread: boolean): MessageTriggerSource {
  return { client, connection, triggerName, bindingName, channelId, matcher, requiresThread, logger: withTriggerLoggerAttributes(client.triggerLogger, bindingAttributes(connection, triggerName, bindingName)) };
}

function slackClientOf(connection: Connection): SlackClient {
  if (!(connection?.operations instanceof SlackClient)) throw new TypeError('Slack Triggers require a connection bound to a SlackClient');
  return connection.operations;
}

function validateTriggerConfiguration(channelId: string, matcher: MessageMatcher | undefined, requiresPoster: boolean, context: z.RefinementCtx): void {
  if (!channelId.trim()) context.addIssue({ code: 'custom', message: 'Slack channel ID is required' });
  const posters = matcher?.posterUserIds ?? [];
  if (posters.some(userId => !userId.trim()) || new Set(posters).size !== posters.length) context.addIssue({ code: 'custom', message: 'Slack poster user IDs must be non-empty and unique' });
  if (requiresPoster && posters.length === 0) context.addIssue({ code: 'custom', message: 'Slack reply matcher requires at least one poster user ID' });
}

function connectionAttributes(connection: ConnectionRef): TriggerLogAttributes { return { connector: ConnectorID, connection: connection.name }; }
function bindingAttributes(connection: ConnectionRef, triggerName: string, bindingName: string): TriggerLogAttributes {
  return { ...connectionAttributes(connection), trigger: triggerName, ...(bindingName ? { binding: bindingName } : {}) };
}

function logConnection(source: TriggerBindingIdentity, context: TriggerContext, level: TriggerLogLevel, message: string, attributes: TriggerLogAttributes = {}): void {
  logTriggerRecord(source.client.triggerLogger, context, level, message, { ...connectionAttributes(source.connection), ...attributes });
}

/** Return the wait after consecutive failure number failures, starting at 1, doubled up to the cap. */
function socketModeReconnectBackoffMs(client: SlackClient, failures: number): number {
  let delay = client.triggerTimings.reconnectDelayMs;
  for (let completed = 1; completed < failures && delay < client.triggerTimings.reconnectMaxDelayMs; completed++) delay *= 2;
  return Math.min(delay, client.triggerTimings.reconnectMaxDelayMs);
}

/** Deliver every route's pending durable inbox events in route order: roots, then replies, then button clicks. */
async function replaySocketModeRoutes(context: TriggerContext, routes: SocketModeRoutes): Promise<void> {
  await replayMessageTriggerRoutes(context, routes.messages);
  await replayMessageTriggerRoutes(context, routes.buttons);
}

/** Deliver every route's pending durable inbox events in route order, roots first. */
async function replayMessageTriggerRoutes(context: TriggerContext, routes: readonly { target: { replayTriggerDeliveries?: (context: TriggerContext) => Promise<void> } }[]): Promise<void> {
  for (const route of routes) {
    if (!route.target.replayTriggerDeliveries) continue;
    try { await route.target.replayTriggerDeliveries(context); }
    catch (error) {
      if (error instanceof TriggerDeliveryAbortedError) throw error;
      throw new Error(`replay Slack Trigger deliveries: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
    }
  }
}

/** Slack asked the runner to close its connection and open a new one. */
class SocketModeDisconnect extends Error {
  constructor(readonly reason: string) { super(`Slack requested a Socket Mode reconnect: ${reason}`); }
}

async function runSocketModeRoutes(context: TriggerContext, routes: SocketModeRoutes): Promise<void> {
  const source = socketModeRoutesIdentity(routes);
  let isReconnecting = false;
  let failures = 0;
  for (;;) {
    throwIfTriggerAborted(context.signal);
    const { isHealthy, error } = await runSocketModeConnection(context, routes, isReconnecting);
    isReconnecting = true;
    throwIfTriggerAborted(context.signal);
    if (error instanceof TriggerDeliveryAbortedError) throw error;
    if (isHealthy) failures = 0;
    let delayMs = source.client.triggerTimings.reconnectDelayMs;
    if (error instanceof SocketModeDisconnect) {
      // Slack refreshes connections routinely, so a requested reconnect is not a failure.
      logConnection(source, context, 'info', 'slack socket mode disconnected; reconnecting', { reason: error.reason, delay_ms: delayMs });
    } else {
      failures++;
      delayMs = socketModeReconnectBackoffMs(source.client, failures);
      logConnection(source, context, 'warn', 'slack socket mode connection failed; reconnecting', { attempt: failures, delay_ms: delayMs, error: error instanceof Error ? error.message : String(error) });
    }
    await waitForTriggerDelay(context.signal, delayMs);
  }
}

/**
 * Serve one Socket Mode connection until it fails or Slack asks for a reconnect. It reports whether the
 * connection received an envelope, so the caller counts consecutive failed attempts. With durable inboxes,
 * a separate loop delivers a bounded queue in arrival order and the connection's end waits for it.
 */
async function runSocketModeConnection(context: TriggerContext, routes: SocketModeRoutes, isReconnecting: boolean): Promise<{ isHealthy: boolean; error: unknown }> {
  const source = socketModeRoutesIdentity(routes);
  const connectionAbort = new AbortController();
  const stopConnection = () => connectionAbort.abort();
  if (context.signal.aborted) stopConnection();
  else context.signal.addEventListener('abort', stopConnection, { once: true });
  let connection: SlackSocketConnection | undefined;
  const state = { isHealthy: false };
  try {
    // A connection can end after an event was persisted but before it was delivered. Deliver it before
    // reading newer envelopes, so a reply cannot overtake its root.
    if (isReconnecting) await replaySocketModeRoutes(context, routes);
    let appToken: string | undefined;
    try { appToken = (await source.client.resolveTriggerCredentials(triggerCredentialCall(source, randomUUID() as CallID, context.signal), context.signal)).appToken?.reveal(); }
    catch (error) {
      if (error instanceof TriggerDeliveryAbortedError) throw error;
      // A credential provider's error may quote what it read, so only its absence is reported.
      return { isHealthy: false, error: new Error('Slack Socket Mode credentials are unavailable') };
    }
    if (!appToken) return { isHealthy: false, error: new Error('Slack Socket Mode credentials are unavailable: credential app_token is required') };
    const socketURL = await source.client.openSocketModeConnection(appToken, context.signal);
    if (connectionAbort.signal.aborted) return { isHealthy: false, error: new TriggerDeliveryAbortedError() };
    try { connection = await source.client.socketDialer(socketURL, connectionAbort.signal); }
    catch (error) { return { isHealthy: false, error: new Error(redactSocketURL(error instanceof Error ? error.message : String(error), socketURL)) }; }
    const opened = connection;
    // A stop during the dial must close the new socket here, or Slack's pings would keep the read waiting forever.
    if (connectionAbort.signal.aborted) { opened.close(); return { isHealthy: false, error: new TriggerDeliveryAbortedError() }; }
    connectionAbort.signal.addEventListener('abort', () => opened.close(), { once: true });
    logConnection(source, context, 'info', 'slack socket mode connected', { reconnect: isReconnecting });
    if (!hasDurableInboxes(routes)) {
      // Only one acknowledged event can then await delivery, so a stopped process loses at most one.
      return { isHealthy: state.isHealthy, error: await readSocketModeEnvelopes(context, opened, routes, undefined, state) };
    }
    const queue = new DeliveryQueue(source.client.socketModeDeliveryQueueCapacity);
    const delivering = deliverQueuedDeliveries(context, queue, stopConnection);
    const readError = await readSocketModeEnvelopes(withAbortSignal(context, connectionAbort.signal), opened, routes, queue, state);
    queue.close();
    const deliveryError = await delivering;
    return { isHealthy: state.isHealthy, error: deliveryError ?? readError };
  } catch (error) {
    return { isHealthy: state.isHealthy, error };
  } finally {
    context.signal.removeEventListener('abort', stopConnection);
    connection?.close();
  }
}

/** Record and acknowledge envelopes until the connection ends; queue deliveries, or deliver inline when queue is undefined. */
async function readSocketModeEnvelopes(context: TriggerContext, connection: SlackSocketConnection, routes: SocketModeRoutes, queue: DeliveryQueue | undefined, state: { isHealthy: boolean }): Promise<unknown> {
  const source = socketModeRoutesIdentity(routes);
  for (;;) {
    let message: unknown;
    try { message = await connection.readMessage(source.client.triggerTimings.readTimeoutMs); }
    catch (error) { return context.signal.aborted ? new TriggerDeliveryAbortedError() : error; }
    const receivedAtMs = Date.now();
    state.isHealthy = true;
    const envelope = socketEnvelope(message);
    if (envelope.type === 'disconnect') return new SocketModeDisconnect(slackCode(envelope.reason));
    let deliveries: SocketModeDeliveries;
    try { deliveries = decodeSocketModeDeliveries(context, routes, envelope); }
    catch (error) {
      logConnection(source, context, 'warn', 'trigger event skipped: undecodable', { envelope_id: envelope.envelopeId, error: error instanceof Error ? error.message : String(error) });
      try { await acknowledgeEnvelope(connection, envelope.envelopeId); } catch (acknowledgeError) { return acknowledgeError; }
      continue;
    }
    try {
      for (const delivery of deliveries.messages) await prepareTriggerDelivery(context, delivery.target, delivery.event);
      for (const delivery of deliveries.buttons) await prepareTriggerDelivery(context, delivery.target, delivery.event);
      await acknowledgeEnvelope(connection, envelope.envelopeId);
    } catch (error) { return error; }
    if (deliveries.messages.length === 0 && deliveries.buttons.length === 0) continue;
    const elapsedMs = Date.now() - receivedAtMs;
    if (envelope.type === 'interactive' && elapsedMs > slackInteractiveAcknowledgementLimitMs) {
      // Slack has already shown the clicker an error, but the recorded click is still delivered.
      logConnection(source, context, 'warn', 'slack interactive envelope acknowledged late', { envelope_id: envelope.envelopeId, elapsed_ms: elapsedMs });
    }
    if (!queue) {
      try { await deliverSocketModeDeliveries(context, deliveries); } catch (error) { return error; }
      continue;
    }
    // A full queue pauses reading; the inboxes hold what was acknowledged, and Slack resends the rest.
    try { await queue.push(deliveries, context.signal); } catch (error) { return error; }
  }
}

/** Deliver queued deliveries in order until the queue closes; after an error, stop the connection and leave the rest in their inboxes. */
async function deliverQueuedDeliveries(context: TriggerContext, queue: DeliveryQueue, stopConnection: () => void): Promise<unknown> {
  let deliveryError: unknown;
  for (;;) {
    const deliveries = await queue.next();
    if (!deliveries) return deliveryError;
    if (deliveryError !== undefined) continue;
    try { await deliverSocketModeDeliveries(context, deliveries); }
    catch (error) { deliveryError = error; stopConnection(); }
  }
}

/** Deliver one envelope's deliveries inline in route order until each is consumed. */
async function deliverSocketModeDeliveries(context: TriggerContext, deliveries: SocketModeDeliveries): Promise<void> {
  await deliverMessageTriggerDeliveries(context, deliveries.messages);
  await deliverButtonClickedDeliveries(context, deliveries.buttons);
}

/** Deliver one envelope's clicks in route order, with channel, thread, and message log attributes. */
async function deliverButtonClickedDeliveries(context: TriggerContext, deliveries: readonly PendingButtonClickedDelivery[]): Promise<void> {
  for (const delivery of deliveries) {
    const click = delivery.event.payload;
    const deliveryContext = withTriggerLogAttributes(context, { channel: click.channelId, thread_ts: click.threadTimestamp, message_ts: click.messageTimestamp });
    await deliverTrigger(deliveryContext, delivery.target, delivery.event, { logger: delivery.source.logger });
  }
}

/** Send interactive envelopes to button routes, when any, and every other envelope to message routes. */
function decodeSocketModeDeliveries(context: TriggerContext, routes: SocketModeRoutes, envelope: SocketEnvelope): SocketModeDeliveries {
  if (envelope.type === 'interactive' && envelope.envelopeId.trim() && routes.buttons.length > 0) return { messages: [], buttons: decodeButtonClickedDeliveries(context, routes.buttons, envelope) };
  if (routes.messages.length > 0) return { messages: decodeMessageTriggerDeliveries(context, routes.messages, envelope), buttons: [] };
  const source = socketModeRoutesIdentity(routes);
  if (source.client.triggerLogger.isEnabled('debug')) logConnection(source, context, 'debug', 'slack socket mode envelope ignored', { type: envelope.type, envelope_id: envelope.envelopeId });
  return { messages: [], buttons: [] };
}

/** Match one interactive envelope against the button routes. */
function decodeButtonClickedDeliveries(context: TriggerContext, routes: readonly ButtonClickedTriggerSourceRoute[], envelope: SocketEnvelope): PendingButtonClickedDelivery[] {
  if (envelope.payload === undefined) throw new Error('decode Slack interactive payload: payload is missing');
  const payload = BlockActionsPayloadSchema.parse(envelope.payload);
  const deliveries: PendingButtonClickedDelivery[] = [];
  for (const route of routes) {
    const matched = matchButtonClickPayload(route.source.configuration, payload);
    // Most interactions belong to other bindings, so build the record only when DEBUG is enabled.
    if (route.source.logger.isEnabled('debug')) {
      for (const reason of matched.ignored) logTriggerRecord(route.source.logger, context, 'debug', 'trigger event ignored', { envelope_id: envelope.envelopeId, channel: blockActionsChannelId(payload), reason });
    }
    for (const event of matched.events) deliveries.push({ source: route.source, target: route.target, event });
  }
  return deliveries;
}

/**
 * Deliver one event's deliveries inline in route order, so a root reaches Dex before any reply received
 * after it. The channel and thread reach the inbox and Dex target records too.
 */
async function deliverMessageTriggerDeliveries(context: TriggerContext, deliveries: readonly PendingMessageTriggerDelivery[]): Promise<void> {
  for (const delivery of deliveries) {
    const deliveryContext = withTriggerLogAttributes(context, { channel: delivery.event.payload.channelId, thread_ts: delivery.event.payload.threadTimestamp });
    await deliverTrigger(deliveryContext, delivery.target, delivery.event, { logger: delivery.source.logger });
  }
}

function decodeMessageTriggerDeliveries(context: TriggerContext, routes: readonly MessageTriggerRoute[], envelope: SocketEnvelope): PendingMessageTriggerDelivery[] {
  if (envelope.type !== 'events_api' || !envelope.envelopeId.trim()) {
    const source = routes[0]!.source;
    if (source.client.triggerLogger.isEnabled('debug')) logConnection(source, context, 'debug', 'slack socket mode envelope ignored', { type: envelope.type, envelope_id: envelope.envelopeId });
    return [];
  }
  const payload = EventsAPIPayloadSchema.parse(envelope.payload);
  const deliveries: PendingMessageTriggerDelivery[] = [];
  for (const route of routes) {
    const matched = matchMessageEvent(route.source, payload);
    if ('reason' in matched) {
      // Most channel traffic is ignored, so build the record only when DEBUG is enabled.
      if (route.source.logger.isEnabled('debug')) logTriggerRecord(route.source.logger, context, 'debug', 'trigger event ignored', matched.attributes);
      continue;
    }
    deliveries.push({ source: route.source, target: route.target, event: matched });
  }
  return deliveries;
}

const nullableString = z.string().nullish().transform(value => value ?? '');
const EventsAPIPayloadSchema = z.object({
  event_id: nullableString, event_time: z.number().int().nullish().transform(value => value ?? 0), team_id: nullableString,
  event: z.object({ type: nullableString, subtype: nullableString, channel: nullableString, user: nullableString, bot_id: nullableString, text: nullableString, ts: nullableString, thread_ts: nullableString }).nullish().transform(value => value ?? { type: '', subtype: '', channel: '', user: '', bot_id: '', text: '', ts: '', thread_ts: '' }),
});
type EventsAPIPayload = z.infer<typeof EventsAPIPayloadSchema>;

/** Return the event when the message matches the source, and otherwise the reason it is ignored, with IDs and Slack enum values only. */
function matchMessageEvent(source: Pick<MessageTriggerSource, 'channelId' | 'matcher' | 'requiresThread'>, payload: EventsAPIPayload): TriggerEvent<MessageEvent> | { reason: string; attributes: TriggerLogAttributes } {
  const message = payload.event;
  const isReply = message.thread_ts !== '' && message.thread_ts !== message.ts;
  const ignored = (reason: string) => ({ reason, attributes: { event_id: payload.event_id, channel: message.channel, reason, ...(reason === 'subtype' ? { subtype: message.subtype } : {}) } });
  if (!payload.event_id) return ignored('missing_event_id');
  if (message.type !== 'message') return ignored('not_a_message');
  if (message.subtype) return ignored('subtype');
  if (message.bot_id) return ignored('bot');
  if (!message.user) return ignored('missing_user');
  if (message.channel !== source.channelId) return ignored('channel_mismatch');
  if (source.requiresThread && !isReply) return ignored('not_a_reply');
  if (!source.requiresThread && isReply) return ignored('not_a_root');
  if (!matchesMessage(source.matcher, message.user, message.text)) return ignored('matcher_mismatch');
  return {
    id: payload.event_id, occurredAt: new Date(payload.event_time * 1000),
    payload: { teamId: payload.team_id, channelId: message.channel, timestamp: message.ts, threadTimestamp: isReply ? message.thread_ts : message.ts, userId: message.user, text: message.text },
  };
}

function matchesMessage(matcher: MessageMatcher, userId: string, text: string): boolean {
  if (matcher.messageContains && !text.toLowerCase().includes(matcher.messageContains.toLowerCase())) return false;
  const posters = matcher.posterUserIds ?? [];
  return posters.length === 0 || posters.includes(userId);
}

function socketEnvelope(message: unknown): SocketEnvelope {
  const record = message !== null && typeof message === 'object' && !Array.isArray(message) ? message as Record<string, unknown> : {};
  const text = (value: unknown) => typeof value === 'string' ? value : '';
  return { envelopeId: text(record.envelope_id), type: text(record.type), payload: record.payload, reason: text(record.reason) };
}

async function acknowledgeEnvelope(connection: SlackSocketConnection, envelopeId: string): Promise<void> {
  if (!envelopeId) return;
  try { await connection.writeMessage({ envelope_id: envelopeId }); }
  catch (error) { throw new Error(`acknowledge Slack Socket Mode envelope: ${error instanceof Error ? error.message : String(error)}`, { cause: error }); }
}

/** Identify a source's credential reads; a project connection's provider refuses a call without the connector ID. */
function triggerCredentialCall(source: TriggerBindingIdentity, id: CallID, signal: AbortSignal) {
  return createTriggerCall(source.connection, { connectorId: ConnectorID, operationId: source.triggerName }, id, signal);
}

function withAbortSignal(context: TriggerContext, signal: AbortSignal): TriggerContext {
  return { signal, logAttributes: context.logAttributes, recordSkip: () => context.recordSkip() };
}

/** Bounded FIFO of one connection's acknowledged deliveries. */
class DeliveryQueue {
  readonly #items: SocketModeDeliveries[] = [];
  #isClosed = false;
  #readerWake: (() => void) | undefined;
  #writerWake: (() => void) | undefined;

  constructor(private readonly capacity: number) {}

  async push(deliveries: SocketModeDeliveries, signal: AbortSignal): Promise<void> {
    while (this.#items.length >= this.capacity) {
      throwIfTriggerAborted(signal);
      await new Promise<void>(resolve => {
        const onAbort = () => resolve();
        signal.addEventListener('abort', onAbort, { once: true });
        this.#writerWake = () => { signal.removeEventListener('abort', onAbort); resolve(); };
      });
      this.#writerWake = undefined;
    }
    throwIfTriggerAborted(signal);
    this.#items.push(deliveries);
    this.#readerWake?.();
  }

  async next(): Promise<SocketModeDeliveries | undefined> {
    while (this.#items.length === 0) {
      if (this.#isClosed) return undefined;
      await new Promise<void>(resolve => { this.#readerWake = resolve; });
      this.#readerWake = undefined;
    }
    const item = this.#items.shift();
    this.#writerWake?.();
    return item;
  }

  close(): void { this.#isClosed = true; this.#readerWake?.(); }
}

/** Receives one connection's relayed Events API callbacks and delivers them to the runner's routes, as Socket Mode does. */
class RelayedMessageReceiver {
  readonly endpoint: WebhookEndpoint<Record<string, never>, PendingMessageTriggerDelivery[]>;
  readonly #source: TriggerSource<PendingMessageTriggerDelivery[]>;
  readonly #callId = randomUUID() as CallID;

  constructor(private readonly project: LoadedProject, private readonly connection: ConnectionRef, private readonly client: SlackClient, private readonly identity: TriggerBindingIdentity, private readonly routes: readonly MessageTriggerRoute[]) {
    this.endpoint = new WebhookEndpoint<Record<string, never>, PendingMessageTriggerDelivery[]>({
      connectorId: ConnectorID, triggerName: 'relayedMessage', connection,
      // The relay's signature uses the scope token, so the public route never reads a Slack credential.
      credentials: new StaticCredentialProvider([[connection, {}]]),
      maxBodyBytes: maxRelayedEventBytes, now: client.now, logger: client.triggerLogger,
      verifyRequest: request => this.#verifyRequest(request),
      decodeEvent: request => this.#decodeEvent(request),
    });
    this.#source = this.endpoint.newSource();
  }

  /** The relayed connection's name. */
  get connectionName(): string { return this.connection.name; }

  /** Read the credential once, retrying while it is unavailable, and report whether the connection lacks an app_token and so receives relayed events. */
  async selectRelayedMode(context: TriggerContext): Promise<boolean> {
    const source = this.identity;
    for (let failures = 1; ; failures++) {
      try {
        const credentials = await this.client.resolveTriggerCredentials(triggerCredentialCall(source, this.#callId, context.signal), context.signal);
        if (credentials.appToken?.reveal()) return false;
        if (!this.project.sharedConnectionId({ connectorId: ConnectorID, connectionName: this.connection.name })) {
          throw new TerminalRelayError(`Slack connection "${this.connection.name}" has no app_token for Socket Mode and is not a shared connection whose host relays its events`);
        }
        return true;
      } catch (error) {
        if (error instanceof TerminalRelayError) throw error;
        throwIfTriggerAborted(context.signal);
        if (error instanceof ConnectionNotSelectedError) throw new Error(`Slack connection "${this.connection.name}": ${error.message}`, { cause: error });
      }
      const delayMs = socketModeReconnectBackoffMs(this.client, failures);
      // A credential provider's error may quote what it read, so only its absence is reported.
      logConnection(source, context, 'warn', 'slack trigger credentials unavailable; retrying', { attempt: failures, delay_ms: delayMs });
      await waitForTriggerDelay(context.signal, delayMs);
    }
  }

  /** Replay the inboxes, receive relayed events, and register the connection name once the source runs. */
  async run(context: TriggerContext): Promise<void> {
    await replayMessageTriggerRoutes(context, this.routes);
    const controller = new AbortController();
    const stop = () => controller.abort();
    context.signal.addEventListener('abort', stop, { once: true });
    const runContext = withAbortSignal(context, controller.signal);
    let sourceResult: { error?: unknown } | undefined;
    const sourceDone = this.#source.run(runContext, new RelayedDeliveryTarget(this.routes)).then(() => { sourceResult = {}; }, (error: unknown) => { sourceResult = { error }; });
    try {
      try { await this.#register(runContext, () => sourceResult); }
      catch (error) {
        controller.abort();
        await sourceDone;
        if (context.signal.aborted) throw new TriggerDeliveryAbortedError();
        throw error;
      }
      await sourceDone;
    } finally { context.signal.removeEventListener('abort', stop); }
    if (sourceResult?.error !== undefined) throw sourceResult.error;
  }

  /** Wait until the source runs, then register, retrying while the host is unavailable; stop early when the source stops. */
  async #register(context: TriggerContext, sourceResult: () => { error?: unknown } | undefined): Promise<void> {
    const source = this.identity;
    const key = { connectorId: ConnectorID, connectionName: this.connection.name };
    while (this.endpoint.runningSourceCount() === 0) {
      const result = sourceResult();
      if (result) throw result.error ?? new TriggerDeliveryAbortedError();
      await waitForTriggerDelay(context.signal, relayedSourceStartPollMs);
    }
    for (let failures = 1; ; failures++) {
      try {
        await this.project.registerRelayedTrigger(key, relayedTriggerRegistrationPath, context.signal);
        logConnection(source, context, 'info', 'slack relayed trigger registered');
        return;
      } catch (error) {
        throwIfTriggerAborted(context.signal);
        if (error instanceof RelayedTriggerNotRelayedError) throw new Error(`Slack connection "${this.connection.name}" has no app_token for Socket Mode and its host does not relay its events`, { cause: error });
        if (!(error instanceof CredentialBrokerUnavailableError)) throw new Error(`register Slack relayed trigger for connection "${this.connection.name}": ${error instanceof Error ? error.message : String(error)}`, { cause: error });
        const delayMs = socketModeReconnectBackoffMs(this.client, failures);
        logConnection(source, context, 'warn', 'slack relayed trigger registration failed; retrying', { attempt: failures, delay_ms: delayMs, error: error.message });
        await waitForTriggerDelay(context.signal, delayMs);
        const result = sourceResult();
        if (result) throw result.error ?? new TriggerDeliveryAbortedError();
      }
    }
  }

  /** Check the host's signature; a token that cannot be read is retryable for the host. */
  async #verifyRequest(request: WebhookRequest): Promise<void> {
    try { await this.project.verifyRelayedRequest(request.headers, request.body, request.receivedAt); }
    catch (error) {
      // The route is public, so anyone can send a bad signature; only the application's own failure is a warning.
      // A mismatch while the token cannot be reread yet is retryable, so a rotated token is accepted on the host's retry.
      const isDeferred = error instanceof RelayTokenRenewalDeferredError;
      const isUnavailable = error instanceof CredentialBrokerUnavailableError;
      const reason = isDeferred ? 'token_renewal_deferred' : isUnavailable ? 'verification_unavailable' : 'invalid_signature';
      logConnection(this.identity, { signal: new AbortController().signal, logAttributes: {}, recordSkip: () => undefined }, isUnavailable && !isDeferred ? 'warn' : 'debug', 'slack relayed request refused', { reason });
      throw isUnavailable ? new WebhookVerificationUnavailableError(error) : error;
    }
  }

  /** Match a relayed Events API callback, which has the Socket Mode payload's shape, against the routes. */
  #decodeEvent(request: WebhookRequest): TriggerEvent<PendingMessageTriggerDelivery[]> | undefined {
    const payload = JSON.parse(Buffer.from(request.body).toString('utf8')) as unknown;
    const context = { signal: new AbortController().signal, logAttributes: {}, recordSkip: () => undefined };
    const deliveries = decodeMessageTriggerDeliveries(context, this.routes, { envelopeId: relayedEnvelopeID, type: 'events_api', payload, reason: '' });
    if (deliveries.length === 0) return undefined;
    return { id: deliveries[0]!.event.id, occurredAt: deliveries[0]!.event.occurredAt, payload: deliveries };
  }
}

/** A refusal that no credential retry can change. */
class TerminalRelayError extends Error {}

/** Records a relayed event in every matching route's inbox, then delivers it to those routes inline in route order. */
class RelayedDeliveryTarget implements TriggerTarget<PendingMessageTriggerDelivery[]> {
  constructor(private readonly routes: readonly MessageTriggerRoute[]) {}

  async prepareTrigger(context: TriggerContext, event: TriggerEvent<PendingMessageTriggerDelivery[]>): Promise<void> {
    for (const delivery of event.payload) await prepareTriggerDelivery(context, delivery.target, delivery.event);
  }

  async handleTrigger(context: TriggerContext, event: TriggerEvent<PendingMessageTriggerDelivery[]>): Promise<void> {
    await deliverMessageTriggerDeliveries(context, event.payload);
  }

  async replayTriggerDeliveries(context: TriggerContext): Promise<void> { await replayMessageTriggerRoutes(context, this.routes); }
}
