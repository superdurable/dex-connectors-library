// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newProjectConnection } from './project.js';
export { ZohoDeskClient, type ZohoDeskClientOptions } from './client.js';
export { SearchTicketsOperation, GetTicketOperation, CreateTicketOperation, UpdateTicketOperation, AddCommentOperation } from './operations.js';
