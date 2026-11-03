// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Failure } from '@superdurable/connectors-sdk';

/** Point at which a driver failure occurred. */
export type SessionPhase = 'connect' | 'begin' | 'statement' | 'commit';
/** Secret-safe MySQL failure and its Dex disposition. */
export interface ClassifiedFailure {
  /** Dex action for this failure. */
  disposition: 'retry' | 'providerRejected' | 'uncertain' | 'invalidResponse' | 'defect';
  /** Failure kind shared with Go. */
  kind: Failure['kind'];
  /** Public message without SQL, parameters, or credentials. */
  message: string;
  /** MySQL error number when the server supplied one. */
  errorNumber?: number;
  /** SQLSTATE when the server supplied one. */
  sqlState?: string;
}

const conflictRetry = new Set([1205, 1213]);
const availabilityRetry = new Set([1021, 1037, 1040, 1041, 1053, 1158, 1159, 1160, 1161, 1203, 1615, 1637, 1927, 4031]);
const authentication = new Set([1045, 1129, 1130, 1251, 1698, 1820, 1862, 3118, 3159, 4151]);
const authorization = new Set([1044, 1142, 1143, 1227, 1290, 1370, 1792, 1836]);
const notFound = new Set([1049, 1054, 1109, 1146, 1305]);
const conflict = new Set([3819, 4025]);
const statementTimeout = new Set([1317, 1969, 3024]);
const validation = new Set([1235, 1295, 1366]);
const connectionLoss = new Set(['ECONNRESET', 'ECONNREFUSED', 'EPIPE', 'ETIMEDOUT', 'PROTOCOL_CONNECTION_LOST', 'PROTOCOL_ENQUEUE_AFTER_FATAL_ERROR']);

/** Classify a native driver error by the same COMMIT boundary and server rules as Go. */
export function classifyDriverError(error: unknown, phase: SessionPhase): ClassifiedFailure {
  const driverError = error && typeof error === 'object' ? error as Record<string, unknown> : {};
  const number = typeof driverError.errno === 'number' ? driverError.errno : 0;
  const sqlState = typeof driverError.sqlState === 'string' ? driverError.sqlState : '';
  const code = typeof driverError.code === 'string' ? driverError.code : '';
  if (number > 0) {
    let disposition: ClassifiedFailure['disposition'] = 'providerRejected';
    let kind: Failure['kind'] = 'PROVIDER_REJECTION';
    if (phase === 'commit' && !conflictRetry.has(number) && sqlState !== '40001' && !sqlState.startsWith('23')) {
      return { disposition: 'uncertain', kind: 'AVAILABILITY', message: `the server returned error ${number} while committing, so whether the transaction committed is unknown`, errorNumber: number, sqlState };
    }
    if (conflictRetry.has(number) || sqlState === '40001') { disposition = 'retry'; kind = 'CONFLICT'; }
    else if (availabilityRetry.has(number)) { disposition = 'retry'; kind = 'AVAILABILITY'; }
    else if (number === 1226) kind = 'QUOTA_EXHAUSTED';
    else if (sqlState.startsWith('08')) { disposition = 'retry'; kind = 'TRANSPORT'; }
    else if (authentication.has(number) || sqlState.startsWith('28')) kind = 'AUTHENTICATION';
    else if (authorization.has(number) || sqlState === '25006') kind = 'AUTHORIZATION';
    else if (notFound.has(number) || sqlState.startsWith('42S')) kind = 'NOT_FOUND';
    else if (conflict.has(number) || sqlState.startsWith('23')) kind = 'CONFLICT';
    else if (statementTimeout.has(number)) kind = 'AVAILABILITY';
    else if (validation.has(number) || ['22', '42', '0A'].some(prefix => sqlState.startsWith(prefix))) kind = 'VALIDATION';
    return { disposition, kind, message: `the server returned error ${number} during ${phase}${statementTimeout.has(number) ? '; the statement exceeded statementTimeout or was interrupted' : ''}`, errorNumber: number, sqlState };
  }
  if (phase === 'connect') {
    if (code === 'ENOTFOUND') return { disposition: 'providerRejected', kind: 'NOT_FOUND', message: 'the MySQL host name does not resolve' };
    if (['CERT_HAS_EXPIRED', 'DEPTH_ZERO_SELF_SIGNED_CERT', 'UNABLE_TO_VERIFY_LEAF_SIGNATURE', 'ERR_TLS_CERT_ALTNAME_INVALID'].includes(code)) return { disposition: 'providerRejected', kind: 'AUTHENTICATION', message: 'the MySQL server certificate failed sslMode verification' };
    if (code === 'HANDSHAKE_NO_SSL_SUPPORT' || code === 'ERR_SSL_WRONG_VERSION_NUMBER') return { disposition: 'providerRejected', kind: 'PROVIDER_REJECTION', message: 'the MySQL server does not accept TLS, which sslMode requires' };
    return { disposition: 'retry', kind: 'AVAILABILITY', message: 'the MySQL server is unreachable or did not finish connecting in time' };
  }
  if (phase === 'commit') return { disposition: 'uncertain', kind: 'TRANSPORT', message: 'the connection failed after COMMIT was sent, so whether the transaction committed is unknown' };
  if (connectionLoss.has(code) || code === 'ABORT_ERR') return { disposition: 'retry', kind: 'TRANSPORT', message: `the connection failed during ${phase} before anything was committed` };
  return { disposition: 'invalidResponse', kind: 'PROTOCOL', message: `the driver could not decode the server's reply during ${phase}; nothing was committed` };
}
