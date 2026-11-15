// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newProjectConnection } from './project.js';
export { FreshdeskClient, type FreshdeskClientOptions } from './client.js';
export { SearchTicketsOperation, GetTicketOperation, CreateTicketOperation, UpdateTicketOperation, AddNoteOperation } from './operations.js';
export {
  TicketChangeCreated, TicketChangeUpdated, TicketChangedEventSchema, TicketChangedTriggerConfigurationSchema, TicketChangedTriggerDefinition, defineTicketChangedTriggerBinding,
  type TicketChangeKind, type TicketChangedEvent, type TicketChangedTriggerBindingConfig, type TicketChangedTriggerConfiguration,
} from './trigger_definition.js';
export {
  MaxTicketListPage, TicketChangeCursorInvalidError, TicketChangeFetcher, TicketChangeOverlapMs, TicketChangePageSize, deliversTicketChangeKind, ticketChangeEventId,
} from './ticket_change_fetcher.js';
export {
  decodeTicketChangedEventPayload, newProjectTicketTriggerRunner, newTicketChangedTrigger, newTicketTriggerRunner, parseTicketChangedTriggerConfiguration,
  type ProjectTicketChangedTriggerRoute, type ProjectTicketTriggerRunnerConfig, type TicketChangedTriggerConfig, type TicketChangedTriggerRoute, type TicketTriggerRunnerConfig,
} from './trigger.js';
