// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newProjectConnection } from './project.js';
export { ZendeskClient, type ZendeskClientOptions } from './client.js';
export { SearchTicketsOperation, GetTicketOperation, CreateTicketOperation, UpdateTicketOperation, buildTicketSearchQuery } from './operations.js';
export { DefaultIncrementalExportRequestsPerMinute } from './client.js';
export {
  TicketChangeCreated, TicketChangeDeleted, TicketChangeUpdated, TicketStatusDeleted, TicketChangedEventSchema, TicketChangedTriggerConfigurationSchema,
  TicketChangedTriggerDefinition, defineTicketChangedTriggerBinding,
  type TicketChangeKind, type TicketChangedEvent, type TicketChangedTriggerBindingConfig, type TicketChangedTriggerConfiguration,
} from './trigger_definition.js';
export { TicketExportCursorInvalidError } from './ticket_export_cursor.js';
export { TicketExportFetcher, newTicketChangedTriggerEvent, ticketChangeEventId, ticketChangeKind } from './ticket_export_fetcher.js';
export {
  decodeTicketChangedEventPayload, newProjectTicketTriggerRunner, newTicketChangedTrigger, newTicketTriggerRunner, parseTicketChangedTriggerConfiguration,
  type ProjectTicketChangedTriggerRoute, type ProjectTicketTriggerRunnerConfig, type TicketChangedTriggerConfig, type TicketChangedTriggerRoute, type TicketTriggerRunnerConfig,
} from './trigger.js';
