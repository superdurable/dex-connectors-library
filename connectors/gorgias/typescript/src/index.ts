// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newProjectConnection } from './project.js';
export { GorgiasClient, type GorgiasClientOptions, type RequestSender } from './client.js';
export { SearchTicketsOperation, GetTicketOperation, FindCustomerByEmailOperation, CreateTicketOperation, UpdateTicketOperation, AddNoteOperation } from './operations.js';
