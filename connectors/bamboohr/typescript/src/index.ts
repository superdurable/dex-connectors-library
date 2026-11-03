// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export * from './generated/models.js';
export * from './generated/definitions.js';
export * from './generated/steps.js';
export { Connection, newConnection, newConnectionWithOperations } from './generated/connection.js';
export { newProjectConnection } from './project.js';
export { BambooHRClient, type BambooHRClientOptions, type BambooHRRequestSender } from './client.js';
export { AddEmployeeOperation, FindEmployeeByEmailOperation, GetEmployeeOperation, ListEmployeeChangesOperation, ListTimeOffRequestsOperation, UpdateEmployeeOperation, buildTimeOffRequestFilter } from './operations.js';
