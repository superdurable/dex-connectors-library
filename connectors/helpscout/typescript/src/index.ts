// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newProjectConnection } from './project.js';
export { HelpScoutClient, type HelpScoutClientOptions } from './client.js';
export { SearchConversationsOperation, GetConversationOperation, ReplyToConversationOperation, UpdateConversationOperation, FindCustomerByEmailOperation } from './operations.js';
