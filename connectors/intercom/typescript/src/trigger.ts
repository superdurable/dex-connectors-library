// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac, timingSafeEqual } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";
import {
  mustTriggerBindingDefinition,
  newTrigger,
  type ConnectionRef,
  type ConnectorConfigurationUI,
  type TriggerBindingDefinition,
  type TriggerContext,
  type TriggerDefinition,
  type TriggerEvent,
  type TriggerRunner,
  type TriggerTarget,
} from "@superdurable/connectors-sdk";
import {
  decodeTriggerConfiguration,
  newDurableTriggerTarget,
  parseTriggerPayloadJSON,
  type LoadedProject,
  type TriggerInboxKey,
} from "@superdurable/connectors-sdk/projectconfig";
import {
  WebhookEndpoint,
  WebhookEndpointRunner,
  WebhookVerificationUnavailableError,
  type WebhookRequest,
} from "@superdurable/connectors-sdk/webhooktrigger";
import { z } from "zod";
import { IntercomClient, type IntercomClientOptions } from "./client.js";
import type { Connection } from "./generated/connection.js";
import { ConnectorID } from "./generated/definitions.js";
import { ConversationSchema, type Conversation, type Credentials } from "./generated/models.js";
import { decodeNotificationConversation, decodeUnixSecondsTime } from "./operations.js";
import { newProjectConnection } from "./project.js";

/** A conversation a user, lead, or visitor started. */
export const TopicConversationUserCreated = "conversation.user.created";
/** A reply from the customer. */
export const TopicConversationUserReplied = "conversation.user.replied";
/** A reply from a teammate. */
export const TopicConversationAdminReplied = "conversation.admin.replied";
/** A one-to-one conversation a teammate started. */
export const TopicConversationAdminSingleCreated = "conversation.admin.single.created";
/** An assignment by a teammate. */
export const TopicConversationAdminAssigned = "conversation.admin.assigned";
/** An assignment of an open conversation. */
export const TopicConversationAdminOpenAssigned = "conversation.admin.open.assigned";
/** An internal note from a teammate. */
export const TopicConversationAdminNoted = "conversation.admin.noted";
/** A conversation a teammate closed. */
export const TopicConversationAdminClosed = "conversation.admin.closed";
/** A conversation a teammate reopened. */
export const TopicConversationAdminOpened = "conversation.admin.opened";
/** A conversation a teammate snoozed. */
export const TopicConversationAdminSnoozed = "conversation.admin.snoozed";
/** A snoozed conversation that reopened. */
export const TopicConversationAdminUnsnoozed = "conversation.admin.unsnoozed";
/** A reply from Fin or another bot. */
export const TopicConversationOperatorReplied = "conversation.operator.replied";
/** A priority change. */
export const TopicConversationPriorityUpdated = "conversation.priority.updated";
/** A customer's conversation rating. */
export const TopicConversationRatingAdded = "conversation.rating.added";

const supportedConversationTopics: readonly string[] = Object.freeze([
  TopicConversationUserCreated, TopicConversationUserReplied, TopicConversationAdminReplied, TopicConversationAdminSingleCreated,
  TopicConversationAdminAssigned, TopicConversationAdminOpenAssigned, TopicConversationAdminNoted, TopicConversationAdminClosed,
  TopicConversationAdminOpened, TopicConversationAdminSnoozed, TopicConversationAdminUnsnoozed, TopicConversationOperatorReplied,
  TopicConversationPriorityUpdated, TopicConversationRatingAdded,
]);

/**
 * Return every topic the conversationEvent Trigger decodes, in the Go connector's stable order. Each call
 * returns a new array. The Trigger answers 200 to every other topic, including Intercom's periodic ping,
 * without recording it.
 */
export function conversationEventTopics(): string[] {
  return [...supportedConversationTopics];
}

/**
 * Topics one conversationEvent binding accepts. An empty or omitted list accepts every topic
 * conversationEventTopics returns; a topic the binding rejects is still answered 200. The topics a binding
 * can receive are those subscribed in the Intercom app's Configure > Webhooks page. Parsing rejects an
 * unsupported or repeated topic.
 */
export const ConversationEventTriggerConfigurationSchema = z
  .strictObject({
    /** Accepted webhook topics, such as conversation.user.created; empty accepts all supported topics. */
    topics: z.array(z.string()).nullable().optional(),
  })
  .superRefine((configuration, context) => {
    const seen = new Set<string>();
    for (const topic of configuration.topics ?? []) {
      if (!supportedConversationTopics.includes(topic))
        context.addIssue({ code: "custom", message: `Intercom webhook topic ${JSON.stringify(topic)} is not a supported conversation topic` });
      else if (seen.has(topic))
        context.addIssue({ code: "custom", message: `Intercom webhook topic ${JSON.stringify(topic)} is listed twice` });
      seen.add(topic);
    }
  });
/** Topics of one conversationEvent binding. */
export type ConversationEventTriggerConfiguration = z.infer<typeof ConversationEventTriggerConfigurationSchema>;

/**
 * One verified Intercom conversation webhook notification; field names match the Go connector. The
 * Trigger event's ID is Intercom's notification ID, which a redelivery of the same notification reuses.
 */
export interface ConversationEvent {
  /** Intercom's notification ID, such as notif_ccd8a4d0-f965-11e3-a367-c779cae3e1b3. */ notificationId: string;
  /** The webhook topic, such as conversation.user.created. */ topic: string;
  /**
   * Intercom's app_id of the workspace the event happened in, omitted when Intercom sends none. An app
   * installed in several workspaces receives every workspace's notifications at one URL.
   */
  workspaceId?: string | undefined;
  /** The conversation as the notification carried it, without its parts or first-message body. */ conversation: Conversation;
  /**
   * When Intercom created the notification, as RFC 3339. Intercom does not guarantee delivery order, so
   * compare it, not arrival order. It is also the Trigger event's occurredAt.
   */
  notifiedAt: string;
}

/** Runtime validator of a ConversationEvent, used to decode events replayed from a durable inbox. */
export const ConversationEventSchema = z.object({
  notificationId: z.string(),
  topic: z.string(),
  workspaceId: z.string().optional(),
  conversation: ConversationSchema,
  notifiedAt: z.string(),
}) satisfies z.ZodType<ConversationEvent>;

/** Manifest definition of conversationEvent. */
export const ConversationEventTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: "conversationEvent" },
  description: "Receive one Intercom conversation webhook notification, verified with its X-Hub-Signature, such as a new inbound conversation or a customer reply, filtered by topic.",
};

/** Studio unit that lists the workspace's admins and stores the chosen admin's numeric ID. */
export const UIUnitAdminPicker = "adminPicker";
/** Admin picker output port. */
export const UIAdminPickerPortAdminID = "adminId";
/** Studio unit that selects a conversationEvent binding's webhook topics. */
export const UIUnitConversationTopicPicker = "conversationTopicPicker";
/** Topic picker output port, a list of topic names. */
export const UIConversationTopicPickerPortTopics = "topics";

/** One static application binding of conversationEvent. */
export interface ConversationEventTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units for the binding, such as the conversationTopicPicker. */ configurationUI: ConnectorConfigurationUI;
}

/** Declare a static conversationEvent binding for Flow Definition tooling and Dex Web; throws on invalid metadata. */
export function defineConversationEventTriggerBinding(config: ConversationEventTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: ConversationEventTriggerDefinition, ...config });
}

/** One conversationEvent binding's configuration and application target. */
export interface ConversationEventTriggerConfig {
  /** Intercom connection from newProjectConnection or newConnection with an IntercomClient. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Topics the binding accepts. */ configuration: ConversationEventTriggerConfiguration;
  /** Routes accepted events into the application, such as newDexFlowTriggerTarget. */ target: TriggerTarget<ConversationEvent>;
}

/**
 * Create one conversationEvent Trigger fed by the connection's shared webhook endpoint, which
 * newConversationEventWebhookHandler serves. Throws for an invalid configuration, a connection name that
 * differs from the connection's, or a connection without an IntercomClient. Its run replays a durable
 * target first and then receives the endpoint's events until the context ends.
 */
export function newConversationEventTrigger(config: ConversationEventTriggerConfig): TriggerRunner {
  const client = intercomClientOf(config.connection);
  if (config.connectionName !== config.connection.reference.name)
    throw new TypeError(`intercom connector trigger connectionName "${config.connectionName}" must equal its connection's name "${config.connection.reference.name}"`);
  const configuration = ConversationEventTriggerConfigurationSchema.parse(config.configuration);
  const endpoint = conversationEventWebhookEndpoint(client, config.connection.reference);
  const source = endpoint.newSource(event => acceptsConversationTopic(configuration, event.payload.topic));
  return newTrigger({
    definition: ConversationEventTriggerDefinition,
    binding: { connection: config.connection.reference, trigger: ConversationEventTriggerDefinition.trigger, name: config.bindingName },
    source, target: config.target, logger: client.triggerLogger,
  });
}

/** A node:http request listener for one connection's Intercom webhook URL. */
export type ConversationEventWebhookHandler = (request: IncomingMessage, response: ServerResponse) => Promise<void>;

/**
 * Return the connection's webhook handler for an application to mount at the public HTTPS URL entered in
 * the Intercom app's Configure > Webhooks page. It answers Intercom's HEAD validation request with 200.
 * Every conversationEvent Trigger built from this connection feeds from it, and it answers 503 to a
 * notification while none of them runs, so Intercom retries.
 */
export function newConversationEventWebhookHandler(connection: Connection): ConversationEventWebhookHandler {
  const endpoint = conversationEventWebhookEndpoint(intercomClientOf(connection), connection.reference);
  return (request, response) => serveWithHeadValidation(endpoint, request, response);
}

/** One stored conversationEvent binding and its application target. */
export interface ProjectConversationEventTriggerRoute {
  /** Names the stored Trigger binding whose configuration selects the topics. */ bindingName: string;
  /**
   * Receives the binding's events through its durable project inbox, such as a newDexFlowTriggerTarget
   * that starts one Flow per conversation.
   */
  target: TriggerTarget<ConversationEvent>;
}

/**
 * Serves one connection's Intercom webhook endpoint while running its bindings. An application mounts
 * serveHTTP at the URL entered in the Intercom app's Configure > Webhooks page and runs it for the life
 * of the process. Requests may be served concurrently.
 */
export class ConversationEventEndpointRunner {
  readonly #endpoint: WebhookEndpoint<Credentials, ConversationEvent>;
  readonly #endpointRunner: WebhookEndpointRunner;

  /** @internal Use newProjectConversationEventEndpointRunner. */
  constructor(endpoint: WebhookEndpoint<Credentials, ConversationEvent>, runners: readonly TriggerRunner[]) {
    this.#endpoint = endpoint;
    this.#endpointRunner = new WebhookEndpointRunner({ serveHTTP: (request, response) => serveWithHeadValidation(endpoint, request, response) }, runners);
  }

  /**
   * Answer Intercom's HEAD validation request, then verify, record, and acknowledge one notification. It
   * answers 503 to a notification until run has replayed every binding's pending events and started
   * receiving, so Intercom retries; 400 to a bad signature or envelope, 405 to another method, and 413 to
   * a body over webhookMaxBodyBytes.
   */
  serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void> {
    return this.#endpointRunner.serveHTTP(request, response);
  }

  /**
   * Replay each binding's durable inbox and then deliver new notifications until the context ends or a
   * binding fails. It rejects with TriggerDeliveryAbortedError after the context ends.
   */
  run(context: TriggerContext): Promise<void> {
    return this.#endpointRunner.run(context);
  }

  /**
   * Report how many bindings are receiving notifications. A readiness check can wait until it equals the
   * number of routes; until it is positive, every notification is answered 503.
   */
  runningSourceCount(): number {
    return this.#endpoint.runningSourceCount();
  }
}

/**
 * Open connectionName and every route's stored binding from the loaded project and wrap each target in
 * the binding's durable project inbox, so a notification is in project storage before Intercom receives
 * 200. Throws when there are no routes, a binding name is blank or repeated, a target is missing, or a
 * binding is not stored (ObjectNotFoundError) or invalid. The client options' logger also applies to the
 * inboxes.
 */
export function newProjectConversationEventEndpointRunner(
  project: LoadedProject,
  connectionName: string,
  routes: readonly ProjectConversationEventTriggerRoute[],
  options: IntercomClientOptions = {},
): ConversationEventEndpointRunner {
  if (!project) throw new TypeError("loaded project is required");
  if (!routes?.length) throw new TypeError("Intercom conversationEvent routes are required");
  const connection = newProjectConnection(project, connectionName, options);
  const client = intercomClientOf(connection);
  const endpoint = conversationEventWebhookEndpoint(client, connection.reference);
  const triggerName = ConversationEventTriggerDefinition.trigger.triggerName;
  const bindingNames = new Set<string>();
  const runners = routes.map(route => {
    const bindingName = route?.bindingName?.trim();
    if (!bindingName || !route.target) throw new TypeError("Intercom conversationEvent binding name and target are required");
    if (bindingNames.has(bindingName)) throw new TypeError(`Intercom conversationEvent binding name "${bindingName}" is duplicated`);
    bindingNames.add(bindingName);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName: connection.reference.name, triggerName, bindingName };
    let configuration: ConversationEventTriggerConfiguration;
    try { configuration = decodeTriggerConfiguration(project.configuration, key, ConversationEventTriggerConfigurationSchema); }
    catch (error: unknown) {
      if (error instanceof z.ZodError) throw new TypeError(`Intercom conversationEvent binding "${bindingName}" configuration: ${error.issues.map(issue => issue.message).join("; ")}`, { cause: error });
      throw error;
    }
    const durableTarget = newDurableTriggerTarget(project.triggerInbox(key), key, route.target, {
      logger: client.triggerLogger,
      decodePayload: (_payloadJSON, payloadBytes) => ConversationEventSchema.parse(parseTriggerPayloadJSON(payloadBytes)),
    });
    return newConversationEventTrigger({ connection, connectionName: connection.reference.name, bindingName, configuration, target: durableTarget });
  });
  return new ConversationEventEndpointRunner(endpoint, runners);
}

const hubSignatureHeader = "x-hub-signature";
const hubSignaturePrefix = "sha1=";
/** Bounds notification IDs, which applications embed in Flow IDs. */
const maximumNotificationIDBytes = 256;
const sha1SignatureHex = /^[0-9A-Fa-f]{40}$/;

/** Each client's shared endpoint per connection, as the Go client caches them. */
const conversationEventEndpoints = new WeakMap<IntercomClient, Map<string, WebhookEndpoint<Credentials, ConversationEvent>>>();

/** Return the connection's shared endpoint, creating it on first use. */
function conversationEventWebhookEndpoint(client: IntercomClient, connection: ConnectionRef): WebhookEndpoint<Credentials, ConversationEvent> {
  let endpoints = conversationEventEndpoints.get(client);
  if (!endpoints) { endpoints = new Map(); conversationEventEndpoints.set(client, endpoints); }
  const key = `${connection.provider}\0${connection.name}`;
  let endpoint = endpoints.get(key);
  if (!endpoint) {
    endpoint = new WebhookEndpoint<Credentials, ConversationEvent>({
      connectorId: ConnectorID, triggerName: ConversationEventTriggerDefinition.trigger.triggerName, connection,
      credentials: client.webhookCredentialProvider(), maxBodyBytes: client.webhookMaxBodyBytes,
      verifyRequest: verifyConversationEventRequest, decodeEvent: decodeConversationEventRequest,
      now: client.now, logger: client.triggerLogger,
    });
    endpoints.set(key, endpoint);
  }
  return endpoint;
}

/** Answer the HEAD request Intercom sends to validate a webhook URL when it is saved; pass every other request on. */
async function serveWithHeadValidation(endpoint: WebhookEndpoint<Credentials, ConversationEvent>, request: IncomingMessage, response: ServerResponse): Promise<void> {
  if (request.method === "HEAD") { request.resume(); response.writeHead(200).end(); return; }
  await endpoint.serveHTTP(request, response);
}

function intercomClientOf(connection: Connection): IntercomClient {
  if (!(connection?.operations instanceof IntercomClient)) throw new TypeError("Intercom Triggers require a connection bound to an IntercomClient");
  return connection.operations;
}

function acceptsConversationTopic(configuration: ConversationEventTriggerConfiguration, topic: string): boolean {
  const topics = configuration.topics ?? [];
  return topics.length === 0 ? supportedConversationTopics.includes(topic) : topics.includes(topic);
}

/** Check X-Hub-Signature: sha1= and the hex HMAC-SHA1 of the body under the client secret. */
async function verifyConversationEventRequest(request: WebhookRequest, credentials: Credentials): Promise<void> {
  const secret = credentials.clientSecret?.reveal() ?? "";
  if (!secret) throw new WebhookVerificationUnavailableError(new Error("Intercom client_secret is not configured"));
  const header = request.headers[hubSignatureHeader];
  // node:http joins repeated headers with ", "; Go's Header.Get reads only the first value.
  const value = (Array.isArray(header) ? header[0] : header)?.split(",")[0]?.trim() ?? "";
  if (!value.startsWith(hubSignaturePrefix)) throw new Error("X-Hub-Signature is missing or does not start with sha1=");
  const encoded = value.slice(hubSignaturePrefix.length);
  if (!sha1SignatureHex.test(encoded)) throw new Error("X-Hub-Signature is not a hex SHA-1 signature");
  const expected = createHmac("sha1", secret).update(request.body).digest();
  if (!timingSafeEqual(Buffer.from(encoded, "hex"), expected)) throw new Error("X-Hub-Signature does not match the body");
}

/** Decode a verified notification; a topic other than a supported conversation topic is valid but unhandled. */
function decodeConversationEventRequest(request: WebhookRequest): TriggerEvent<ConversationEvent> | undefined {
  let notification: Record<string, unknown>;
  try {
    const parsed: unknown = JSON.parse(Buffer.from(request.body).toString("utf8"));
    if (parsed === null) notification = {};
    else if (typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("not an object");
    else notification = parsed as Record<string, unknown>;
  } catch { throw new Error("Intercom notification is not JSON"); }
  const type = envelopeString(notification.type), id = envelopeString(notification.id), topic = envelopeString(notification.topic);
  const workspaceId = envelopeString(notification.app_id);
  const createdAt = decodeUnixSecondsTime(notification.created_at);
  // Go decodes data as an object for every topic, so a malformed one fails even for an unhandled topic.
  const data = notification.data;
  if (data !== undefined && data !== null && (typeof data !== "object" || Array.isArray(data)))
    throw new Error("Intercom notification is not JSON");
  if (type !== "notification_event" || topic === "" || !isPrintableIdentifier(id, maximumNotificationIDBytes))
    throw new Error("Intercom notification envelope is invalid");
  if (!supportedConversationTopics.includes(topic)) return undefined;
  let conversation: Conversation;
  try { conversation = decodeNotificationConversation(request.body); }
  catch (error: unknown) { throw new Error(`Intercom notification item: ${error instanceof Error ? error.message : "invalid"}`); }
  const notifiedAt = createdAt === goZeroTime ? formatGoTime(request.receivedAt) : createdAt;
  return {
    id, occurredAt: new Date(notifiedAt),
    payload: { notificationId: id, topic, ...(workspaceId ? { workspaceId } : {}), conversation, notifiedAt },
  };
}

const goZeroTime = "0001-01-01T00:00:00Z";

/** Decode an envelope string as Go decodes into a string field: null is empty and another type fails. */
function envelopeString(value: unknown): string {
  if (value === undefined || value === null) return "";
  if (typeof value !== "string") throw new Error("Intercom notification envelope is invalid");
  return value;
}

function isPrintableIdentifier(value: string, maximumBytes: number): boolean {
  if (value === "" || Buffer.byteLength(value) > maximumBytes) return false;
  for (let index = 0; index < value.length; index++) {
    const code = value.charCodeAt(index);
    if (code <= 0x20 || code > 0x7e) return false;
  }
  return true;
}

/** Format a time as Go's time.Time JSON does: RFC 3339 in UTC without trailing fractional zeros. */
function formatGoTime(time: Date): string {
  return time.toISOString().replace(/\.000Z$/, "Z").replace(/(\.\d*?)0+Z$/, "$1Z");
}
