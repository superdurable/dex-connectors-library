// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { IncomingMessage, ServerResponse } from 'node:http';
import {
  mustTriggerBindingDefinition, newTrigger, type ConnectionRef, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerContext,
  type TriggerDefinition, type TriggerEvent, type TriggerRunner, type TriggerTarget,
} from '@superdurable/connectors-sdk';
import { ObjectNotFoundError, decodeTriggerConfiguration, newDurableTriggerTarget, parseTriggerPayloadJSON, type LoadedProject, type Configuration as ProjectConfiguration, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { WebhookEndpoint, WebhookEndpointRunner, WebhookVerificationUnavailableError } from '@superdurable/connectors-sdk/webhooktrigger';
import { DocuSignClient, type DocuSignClientOptions } from './client.js';
import {
  EnvelopeEventReceivedTriggerConfigurationSchema, EnvelopeEventSchema, decodeConnectMessage, verifyConnectSignature,
  type EnvelopeEvent, type EnvelopeEventReceivedTriggerConfiguration,
} from './connect_event.js';
import type { Connection } from './generated/connection.js';
import { ConnectorID } from './generated/definitions.js';
import type { Credentials } from './generated/models.js';
import { newProjectConnection } from './project.js';

/** Manifest definition of envelopeEventReceived. */
export const EnvelopeEventReceivedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'envelopeEventReceived' },
  description: "Receive an HMAC-verified DocuSign Connect envelope-completed, envelope-declined, or envelope-voided event in the JSON SIM format, with the envelope ID and event name as its event ID and the envelope's text custom fields for correlation.",
};

/** One static application binding of envelopeEventReceived. */
export interface EnvelopeEventReceivedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Optional Connector Studio configuration units; the connector declares none, so Dex Web saves the binding as {}, which records all three outcomes. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static envelopeEventReceived binding for Flow Definition tooling and Dex Web. */
export function defineEnvelopeEventReceivedTriggerBinding(config: EnvelopeEventReceivedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: EnvelopeEventReceivedTriggerDefinition, ...config });
}

/** Configuration of one standalone envelopeEventReceived Trigger. */
export interface EnvelopeEventReceivedTriggerConfig {
  /** DocuSign connection from newProjectConnection or newConnection with a DocuSignClient. */ connection: Connection;
  /** Static connection name; must equal the connection's name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** The binding's event filter. */ configuration: EnvelopeEventReceivedTriggerConfiguration;
  /** Routes accepted events into the application. */ target: TriggerTarget<EnvelopeEvent>;
}

/**
 * Create one envelopeEventReceived Trigger fed by the connection's shared Connect endpoint, which
 * envelopeEventReceivedWebhookEndpoint returns for the application to mount. Every Trigger of one
 * connection shares that endpoint. Throws a TypeError or the schema's error on invalid wiring.
 */
export function newEnvelopeEventReceivedTrigger(config: EnvelopeEventReceivedTriggerConfig): TriggerRunner {
  const client = docuSignClientOf(config.connection);
  if (config.connectionName !== config.connection.reference.name) {
    throw new TypeError(`docusign connector trigger connectionName "${config.connectionName}" must equal its connection's name "${config.connection.reference.name}"`);
  }
  const configuration = EnvelopeEventReceivedTriggerConfigurationSchema.parse(config.configuration);
  const events = configuration.events ?? [];
  const source = envelopeEventReceivedWebhookEndpoint(config.connection).newSource(event => events.length === 0 || events.includes(event.payload.event));
  return newTrigger({
    definition: EnvelopeEventReceivedTriggerDefinition, source, target: config.target, logger: client.triggerLogger,
    binding: { connection: config.connection.reference, trigger: EnvelopeEventReceivedTriggerDefinition.trigger, name: config.bindingName },
  });
}

/**
 * Return the connection's Connect endpoint, creating it on first use, for an application to mount at the
 * URL to Publish of its Connect configuration. Every envelopeEventReceived Trigger built from this
 * connection feeds from it. It answers 503 while none of them runs and without a connect_hmac_key, so
 * Connect retries; 400 for a bad signature or a body that is not JSON SIM; 413 above connectMaxBodyBytes,
 * before the signature is checked; and 200 for another Connect event, unrecorded.
 */
export function envelopeEventReceivedWebhookEndpoint(connection: Connection): WebhookEndpoint<Credentials, EnvelopeEvent> {
  const client = docuSignClientOf(connection);
  let endpoints = webhookEndpointsByClient.get(client);
  if (!endpoints) { endpoints = new Map(); webhookEndpointsByClient.set(client, endpoints); }
  const key = `${connection.reference.provider}\0${connection.reference.name}`;
  let endpoint = endpoints.get(key);
  if (!endpoint) {
    endpoint = newConnectEndpoint(client, connection.reference);
    endpoints.set(key, endpoint);
  }
  return endpoint;
}

/** One envelopeEventReceived binding from the project configuration and its application target. */
export interface ProjectEnvelopeEventReceivedTriggerRoute {
  /** Configured binding name whose configuration filters the events. */ bindingName: string;
  /** Receives the binding's events through its durable project inbox, such as a newDexRPCTriggerTarget that resumes the waiting Flow. */ target: TriggerTarget<EnvelopeEvent>;
}

/**
 * Serves one connection's Connect endpoint while running its bindings. Mount serveHTTP at the Connect
 * configuration's URL to Publish and run it for the life of the process. It is safe for concurrent requests.
 */
export class EnvelopeEventReceivedEndpointRunner {
  readonly #runner: WebhookEndpointRunner;
  readonly #endpoint: WebhookEndpoint<Credentials, EnvelopeEvent>;

  /** @internal Use newProjectEnvelopeEventReceivedEndpointRunner. */
  constructor(endpoint: WebhookEndpoint<Credentials, EnvelopeEvent>, runners: readonly TriggerRunner[]) {
    this.#endpoint = endpoint;
    this.#runner = new WebhookEndpointRunner(endpoint, runners);
  }

  /** Verify, record, and acknowledge one Connect delivery. It answers 503 until run has replayed every binding's pending events and started receiving, so Connect retries. */
  serveHTTP(request: IncomingMessage, response: ServerResponse): Promise<void> { return this.#runner.serveHTTP(request, response); }

  /** Replay each binding's durable inbox, then deliver new events until the context ends, rejecting with TriggerDeliveryAbortedError, or a binding fails. */
  run(context: TriggerContext): Promise<void> { return this.#runner.run(context); }

  /** How many bindings are receiving events; a readiness check can wait until it equals the number of routes. */
  runningSourceCount(): number { return this.#endpoint.runningSourceCount(); }
}

/**
 * Open connectionName and every route's binding from the loaded project and wrap each target in the
 * binding's durable project inbox, so an event is stored before Connect receives 200. A route whose
 * binding is not configured throws ObjectNotFoundError; an invalid configuration, a blank or duplicated
 * binding name, or no routes throws. The client options' logger also applies to the inboxes.
 */
export function newProjectEnvelopeEventReceivedEndpointRunner(
  project: LoadedProject, connectionName: string, routes: readonly ProjectEnvelopeEventReceivedTriggerRoute[], options: DocuSignClientOptions = {},
): EnvelopeEventReceivedEndpointRunner {
  const connection = newProjectConnection(project, connectionName, options);
  const client = docuSignClientOf(connection);
  return newEnvelopeEventReceivedEndpointRunner(connection, project.configuration, routes, (key, target) => newDurableTriggerTarget(project.triggerInbox(key), key, target, {
    logger: client.triggerLogger, decodePayload: (_payloadJSON, payloadBytes) => EnvelopeEventSchema.parse(parseTriggerPayloadJSON(payloadBytes)),
  }));
}

/** Wraps one binding's target so each event is stored before its acknowledgement. */
export type DurableEnvelopeEventReceivedTarget = (key: TriggerInboxKey, target: TriggerTarget<EnvelopeEvent>) => TriggerTarget<EnvelopeEvent>;

/** @internal Run routes on connection with the bindings that configuration stores, wrapping each target with makeDurable. */
export function newEnvelopeEventReceivedEndpointRunner(
  connection: Connection, configuration: ProjectConfiguration, routes: readonly ProjectEnvelopeEventReceivedTriggerRoute[], makeDurable: DurableEnvelopeEventReceivedTarget,
): EnvelopeEventReceivedEndpointRunner {
  if (!routes || routes.length === 0) throw new TypeError('DocuSign envelopeEventReceived routes are required');
  const endpoint = envelopeEventReceivedWebhookEndpoint(connection);
  const connectionName = connection.reference.name;
  const triggerName = EnvelopeEventReceivedTriggerDefinition.trigger.triggerName;
  const bindingNames = new Set<string>();
  const runners = routes.map(route => {
    const bindingName = route?.bindingName?.trim();
    if (!bindingName || !route.target) throw new TypeError('DocuSign envelopeEventReceived binding name and target are required');
    if (bindingNames.has(bindingName)) throw new TypeError(`DocuSign envelopeEventReceived binding name "${bindingName}" is duplicated`);
    bindingNames.add(bindingName);
    const key: TriggerInboxKey = { connectorId: ConnectorID, connectionName, triggerName, bindingName };
    let bindingConfiguration: EnvelopeEventReceivedTriggerConfiguration;
    try { bindingConfiguration = decodeTriggerConfiguration(configuration, key, EnvelopeEventReceivedTriggerConfigurationSchema); }
    catch (error) {
      if (error instanceof ObjectNotFoundError) throw error;
      throw new Error(`DocuSign envelopeEventReceived binding "${bindingName}" configuration: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
    }
    return newEnvelopeEventReceivedTrigger({ connection, connectionName, bindingName, configuration: bindingConfiguration, target: makeDurable(key, route.target) });
  });
  return new EnvelopeEventReceivedEndpointRunner(endpoint, runners);
}

/** One endpoint per client and connection, as the Go client caches them, so every binding shares a URL. */
const webhookEndpointsByClient = new WeakMap<DocuSignClient, Map<string, WebhookEndpoint<Credentials, EnvelopeEvent>>>();

function newConnectEndpoint(client: DocuSignClient, connection: ConnectionRef): WebhookEndpoint<Credentials, EnvelopeEvent> {
  return new WebhookEndpoint<Credentials, EnvelopeEvent>({
    connectorId: ConnectorID, triggerName: EnvelopeEventReceivedTriggerDefinition.trigger.triggerName, connection,
    credentials: client.credentials, credentialRefresh: client.refreshDriver, maxBodyBytes: client.connectMaxBodyBytes,
    now: () => client.currentTime(), logger: client.triggerLogger,
    verifyRequest: async (request, credentials) => {
      // DocuSign shows the key quoted in some exports; Go strips every quote too.
      const hmacKey = (credentials.connectHmacKey?.reveal() ?? '').replaceAll('"', '');
      if (hmacKey === '') throw new WebhookVerificationUnavailableError();
      verifyConnectSignature(request.body, request.headers, hmacKey);
    },
    decodeEvent: (request): TriggerEvent<EnvelopeEvent> | undefined => decodeConnectMessage(request.body, request.receivedAt),
  });
}

function docuSignClientOf(connection: Connection): DocuSignClient {
  if (!(connection?.operations instanceof DocuSignClient)) throw new TypeError('DocuSign Triggers require a connection bound to a DocuSignClient');
  return connection.operations;
}
