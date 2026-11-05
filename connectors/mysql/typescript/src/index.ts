// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { MySQLClient, type MySQLClientOptions, type MySQLConnectionFactory } from './client.js';
export { QueryRowsOperation, ExecuteStatementOperation } from './operations.js';
export { classifyDriverError, type ClassifiedFailure, type SessionPhase } from './failures.js';
export { newProjectConnection } from './project.js';
