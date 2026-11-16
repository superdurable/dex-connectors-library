// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * A Go time.Time value as the employeeChanged cursor keeps it: an instant with nanosecond precision and the
 * UTC offset it was written with, so a re-encoded cursor matches the Go connector byte for byte.
 */
export interface GoTime {
  /** Whole seconds since the Unix epoch. */ readonly unixSeconds: number;
  /** Nanoseconds past unixSeconds, 0 to 999999999. */ readonly nanoseconds: number;
  /** The written UTC offset in seconds; 0 prints as Z. */ readonly offsetSeconds: number;
}

const rfc3339Pattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(?:Z|([+-])(\d{2}):(\d{2}))$/;
const monthNames = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'];
const imfFixdatePattern = /^(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun), (\d{2}) ([A-Z][a-z]{2}) (\d{4}) (\d{2}):(\d{2}):(\d{2}) GMT$/;
const rfc850Pattern = /^(?:Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday), (\d{2})-([A-Za-z]{3})-(\d{2}) (\d{2}):(\d{2}):(\d{2}) (?:GMT|UTC)$/;
const ansiCPattern = /^(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun) ([A-Za-z]{3}) ([ 0-9][0-9]) (\d{2}):(\d{2}):(\d{2}) (\d{4})$/;

/** Go's zero time.Time, 0001-01-01T00:00:00Z. */
const zeroUnixSeconds = -62_135_596_800;

/** Parse text as Go's time.Parse(time.RFC3339) does, keeping its offset; throws for any other text. */
export function parseGoTime(text: string): GoTime {
  const match = rfc3339Pattern.exec(text);
  if (!match) throw new TypeError('not an RFC 3339 time');
  const [, year, month, day, hour, minute, second, fraction = '', sign, zoneHour = '0', zoneMinute = '0'] = match;
  const offsetSeconds = sign ? (sign === '+' ? 1 : -1) * (Number(zoneHour) * 3600 + Number(zoneMinute) * 60) : 0;
  if (Number(zoneHour) > 24 || Number(zoneMinute) > 60) throw new TypeError('time zone offset out of range');
  const unixSeconds = civilUnixSeconds(Number(year), Number(month), Number(day), Number(hour), Number(minute), Number(second));
  if (unixSeconds === undefined) throw new TypeError('RFC 3339 time out of range');
  return { unixSeconds: unixSeconds - offsetSeconds, nanoseconds: Number(fraction.slice(0, 9).padEnd(9, '0')), offsetSeconds };
}

/** Format moment as Go's time.Time JSON encoding does: RFC 3339 with trailing fraction zeros trimmed. */
export function formatGoTime(moment: GoTime): string {
  if (Math.abs(moment.offsetSeconds) >= 24 * 3600) throw new RangeError('Time.MarshalJSON: timezone hour outside of range [0,23]');
  const local = new Date((moment.unixSeconds + moment.offsetSeconds) * 1000);
  const year = local.getUTCFullYear();
  if (year < 0 || year > 9999) throw new RangeError('Time.MarshalJSON: year outside of range [0,9999]');
  const two = (value: number) => String(value).padStart(2, '0');
  let text = `${String(year).padStart(4, '0')}-${two(local.getUTCMonth() + 1)}-${two(local.getUTCDate())}T${two(local.getUTCHours())}:${two(local.getUTCMinutes())}:${two(local.getUTCSeconds())}`;
  if (moment.nanoseconds) text += `.${String(moment.nanoseconds).padStart(9, '0').replace(/0+$/, '')}`;
  if (!moment.offsetSeconds) return `${text}Z`;
  const minutes = Math.floor(Math.abs(moment.offsetSeconds) / 60);
  return `${text}${moment.offsetSeconds < 0 ? '-' : '+'}${two(Math.floor(minutes / 60))}:${two(minutes % 60)}`;
}

/** Compare two instants, ignoring their offsets. */
export function compareGoTimes(left: GoTime, right: GoTime): number {
  return left.unixSeconds - right.unixSeconds || left.nanoseconds - right.nanoseconds;
}

/** The same instant in UTC, as Go's Time.UTC returns it. */
export function goTimeInUTC(moment: GoTime): GoTime {
  return { unixSeconds: moment.unixSeconds, nanoseconds: moment.nanoseconds, offsetSeconds: 0 };
}

/** The same instant shifted by seconds. */
export function addGoTimeSeconds(moment: GoTime, seconds: number): GoTime {
  return { ...moment, unixSeconds: moment.unixSeconds + seconds };
}

/** Whether moment is Go's zero time instant. */
export function isZeroGoTime(moment: GoTime): boolean {
  return moment.unixSeconds === zeroUnixSeconds && moment.nanoseconds === 0;
}

/** A JavaScript Date in UTC truncated to whole seconds, as Go's now().UTC().Truncate(time.Second). */
export function goTimeFromDate(date: Date): GoTime {
  return { unixSeconds: Math.floor(date.getTime() / 1000), nanoseconds: 0, offsetSeconds: 0 };
}

/** The instant as a Date; nanoseconds below a millisecond drop. */
export function goTimeToDate(moment: GoTime): Date {
  return new Date(moment.unixSeconds * 1000 + Math.floor(moment.nanoseconds / 1_000_000));
}

/** Parse an HTTP Date header in the three formats Go's http.ParseTime accepts; undefined otherwise. */
export function parseHTTPDate(value: string): GoTime | undefined {
  let day: string | undefined, month: string | undefined, year: string | undefined, hour: string | undefined, minute: string | undefined, second: string | undefined;
  let match = imfFixdatePattern.exec(value);
  if (match) [, day, month, year, hour, minute, second] = match;
  else if ((match = rfc850Pattern.exec(value))) {
    [, day, month, year, hour, minute, second] = match;
    year = String((Number(year) >= 69 ? 1900 : 2000) + Number(year));
  } else if ((match = ansiCPattern.exec(value))) [, month, day, hour, minute, second, year] = match;
  else return undefined;
  const monthNumber = monthNames.indexOf(month!.toLowerCase()) + 1;
  if (!monthNumber) return undefined;
  const unixSeconds = civilUnixSeconds(Number(year), monthNumber, Number(day!.trim()), Number(hour), Number(minute), Number(second));
  return unixSeconds === undefined ? undefined : { unixSeconds, nanoseconds: 0, offsetSeconds: 0 };
}

/** Seconds since the epoch of a UTC civil time, or undefined when a field is out of range. */
function civilUnixSeconds(year: number, month: number, day: number, hour: number, minute: number, second: number): number | undefined {
  if (month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || second > 59) return undefined;
  const date = new Date(0);
  date.setUTCFullYear(year, month - 1, day);
  date.setUTCHours(hour, minute, second, 0);
  if (date.getUTCFullYear() !== year || date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day) return undefined;
  return date.getTime() / 1000;
}
