// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { collapseWhitespace } from './text.js';

/** One instant with Go's nanosecond precision, which JavaScript Date cannot hold. */
export interface Instant { readonly unixSeconds: number; readonly nanoseconds: number }

const zoneOffsets = new Map<string, string>([
  ['UT', '+0000'], ['UTC', '+0000'], ['GMT', '+0000'], ['Z', '+0000'], ['Z00:00', '+0000'],
  ['EST', '-0500'], ['EDT', '-0400'], ['CST', '-0600'], ['CDT', '-0500'],
  ['MST', '-0700'], ['MDT', '-0600'], ['PST', '-0800'], ['PDT', '-0700'],
  ['CET', '+0100'], ['CEST', '+0200'], ['BST', '+0100'], ['IST', '+0530'], ['JST', '+0900'], ['AEST', '+1000'],
]);
const fullMonthNames = /January|February|March|April|June|July|August|September|Sept|October|November|December/g;
const monthAbbreviations = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'];
const weekdays = new Set(['mon', 'tue', 'tues', 'wed', 'thu', 'thur', 'thurs', 'fri', 'sat', 'sun',
  'monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday', 'sunday']);
// Rejects zero and epoch placeholders, which generators emit for a missing date.
const earliestUnixSeconds = 86_400;

const fraction = '(?:[.,](\\d+))?';
const date = '(\\d{4})-(\\d{2})-(\\d{2})';
const clockSeconds = `(\\d{1,2}):(\\d{2}):(\\d{2})${fraction}`;
const clockMinutes = '(\\d{1,2}):(\\d{2})';

interface Fields { year: number; month: number; day: number; hour: number; minute: number; second: number; fraction: string; offset: string }
type LayoutReader = (value: string) => Fields | undefined;

/** Build a reader for an ISO layout with the given clock and zone patterns, as one Go time.Parse layout. */
function isoLayout(separator: string, clock: 'seconds' | 'minutes' | 'none', zone: string): LayoutReader {
  const clockPattern = clock === 'seconds' ? clockSeconds : clock === 'minutes' ? clockMinutes : '';
  const pattern = new RegExp(`^${date}${clock === 'none' ? '' : separator + clockPattern}${zone ? `(${zone})` : ''}$`);
  return value => {
    const match = pattern.exec(value);
    if (!match) return undefined;
    const groups = match.slice(1);
    const [year, month, day] = groups.splice(0, 3).map(Number) as [number, number, number];
    let hour = 0; let minute = 0; let second = 0; let fractionDigits = '';
    if (clock === 'seconds') {
      const [hourText, minuteText, secondText, fractionText] = groups.splice(0, 4);
      hour = Number(hourText); minute = Number(minuteText); second = Number(secondText); fractionDigits = fractionText ?? '';
    } else if (clock === 'minutes') {
      const [hourText, minuteText] = groups.splice(0, 2);
      hour = Number(hourText); minute = Number(minuteText);
    }
    return { year, month, day, hour, minute, second, fraction: fractionDigits, offset: groups[0] ?? '' };
  };
}

function shortLayout(pattern: RegExp, isYearMonth: boolean): LayoutReader {
  return value => {
    const match = pattern.exec(value);
    if (!match) return undefined;
    return { year: Number(match[1]), month: isYearMonth ? Number(match[2]) : 1, day: 1, hour: 0, minute: 0, second: 0, fraction: '', offset: '' };
  };
}

// The ISO layouts of the Go connector, most specific first. A value without an offset is UTC.
const isoLayouts: LayoutReader[] = [
  isoLayout('T', 'seconds', 'Z|[+-]\\d{2}:\\d{2}'),
  isoLayout('T', 'seconds', 'Z|[+-]\\d{4}'),
  isoLayout('T', 'minutes', 'Z|[+-]\\d{2}:\\d{2}'),
  isoLayout('T', 'minutes', 'Z|[+-]\\d{4}'),
  isoLayout('T', 'seconds', ''),
  isoLayout('T', 'minutes', ''),
  isoLayout(' ', 'seconds', 'Z|[+-]\\d{2}:\\d{2}'),
  isoLayout(' ', 'seconds', ' (?:Z|[+-]\\d{2}:\\d{2})'),
  isoLayout(' ', 'seconds', ' [+-]\\d{4}'),
  isoLayout(' ', 'seconds', ' [+-]\\d{4} [A-Z]{3,5}'),
  isoLayout(' ', 'seconds', ''),
  isoLayout(' ', 'minutes', ''),
  isoLayout('', 'none', ''),
  shortLayout(/^(\d{4})-(\d{2})$/, true),
  shortLayout(/^(\d{4})$/, false),
];

const month = '(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)';

interface MailLayout { pattern: RegExp; order: 'dayFirst' | 'monthFirst' | 'clockBeforeYear'; isTwoDigitYear: boolean; hasSeconds: boolean; hasClock: boolean; hasZone: boolean }

function mailLayout(source: string, order: MailLayout['order'], isTwoDigitYear: boolean, hasSeconds: boolean, hasClock: boolean, hasZone: boolean): MailLayout {
  return { pattern: new RegExp(`^${source}$`, 'i'), order, isTwoDigitYear, hasSeconds, hasClock, hasZone };
}

const secondsClock = `(\\d{1,2}):(\\d{2}):(\\d{2})${fraction}`;
const minutesClock = '(\\d{1,2}):(\\d{2})()()';
const zone = '([+-]\\d{4})';
// The mail layouts of the Go connector after the weekday is removed and the zone made numeric.
const mailLayouts: MailLayout[] = [
  mailLayout(`(\\d{1,2}) ${month} (\\d{4}) ${secondsClock} ${zone}`, 'dayFirst', false, true, true, true),
  mailLayout(`(\\d{1,2}) ${month} (\\d{4}) ${minutesClock} ${zone}`, 'dayFirst', false, false, true, true),
  mailLayout(`(\\d{1,2}) ${month} (\\d{2}) ${secondsClock} ${zone}`, 'dayFirst', true, true, true, true),
  mailLayout(`(\\d{1,2}) ${month} (\\d{2}) ${minutesClock} ${zone}`, 'dayFirst', true, false, true, true),
  mailLayout(`(\\d{1,2}) ${month} (\\d{4}) ${secondsClock}()`, 'dayFirst', false, true, true, false),
  mailLayout(`(\\d{1,2}) ${month} (\\d{4}) ${minutesClock}()`, 'dayFirst', false, false, true, false),
  mailLayout(`(\\d{1,2}) ${month} (\\d{4})()()()()()`, 'dayFirst', false, false, false, false),
  mailLayout(`${month} (\\d{1,2}) (\\d{4}) ${secondsClock} ${zone}`, 'monthFirst', false, true, true, true),
  mailLayout(`${month} (\\d{1,2}) (\\d{4}) ${secondsClock}()`, 'monthFirst', false, true, true, false),
  mailLayout(`${month} (\\d{1,2}) ${secondsClock} (\\d{4})()`, 'clockBeforeYear', false, true, true, false),
  mailLayout(`${month} (\\d{1,2}) ${secondsClock} ${zone} (\\d{4})`, 'clockBeforeYear', false, true, true, true),
  mailLayout(`${month} (\\d{1,2}) (\\d{4})()()()()()`, 'monthFirst', false, false, false, false),
];

/** Read the date spellings feeds, sitemaps, and pages use, as the Go connector does; undefined means missing. */
export function parseLooseTimestamp(value: string): Instant | undefined {
  const collapsed = collapseWhitespace(value);
  if (collapsed === '' || Buffer.byteLength(collapsed, 'utf8') > 64) return undefined;
  const parsed = parseISOTimestamp(collapsed) ?? parseMailTimestamp(collapsed);
  if (!parsed || parsed.unixSeconds < earliestUnixSeconds) return undefined;
  return parsed;
}

/** Parse an RFC 3339 time that an input schema already validated. */
export function parseInputTimestamp(value: string): Instant | undefined { return parseISOTimestamp(value.toUpperCase()); }

function parseISOTimestamp(value: string): Instant | undefined {
  if (value.length < 4 || !/^[0-9]/.test(value)) return undefined;
  let candidate = value.toUpperCase();
  // A trailing UTC or GMT names the zone of a value with no offset, or repeats a zero offset.
  if (candidate.endsWith(' UTC') || candidate.endsWith(' GMT')) candidate = candidate.slice(0, -4).trim();
  for (const layout of isoLayouts) {
    const fields = layout(candidate);
    if (fields) return instantFromFields(fields);
  }
  return undefined;
}

function parseMailTimestamp(value: string): Instant | undefined {
  let fields = collapseWhitespace(value.replaceAll(',', ' ')).split(' ').filter(field => field !== '');
  if (fields.length > 0 && weekdays.has(fields[0]!.toLowerCase().replace(/\.$/, ''))) fields = fields.slice(1);
  // RFC 850 writes the date as 02-Nov-26 or 02-Nov-2026.
  if (fields.length > 0 && fields[0]!.split('-').length === 3) fields = [...fields[0]!.split('-'), ...fields.slice(1)];
  fields = fields.map(field => {
    const offset = zoneOffsets.get(field.toUpperCase()) ?? numericOffset(field);
    if (offset !== undefined) return offset;
    return (field.slice(0, 1).toUpperCase() + field.slice(1).toLowerCase()).replace(fullMonthNames, name => name.slice(0, 3));
  });
  const candidate = fields.join(' ');
  for (const layout of mailLayouts) {
    const match = layout.pattern.exec(candidate);
    if (!match) continue;
    const groups = match.slice(1);
    let dayText: string; let monthText: string; let yearText: string; let clock: string[]; let offset: string;
    if (layout.order === 'dayFirst') {
      [dayText, monthText, yearText] = groups.slice(0, 3) as [string, string, string];
      clock = groups.slice(3, 7) as string[]; offset = groups[7] ?? '';
    } else if (layout.order === 'monthFirst') {
      [monthText, dayText, yearText] = groups.slice(0, 3) as [string, string, string];
      clock = groups.slice(3, 7) as string[]; offset = groups[7] ?? '';
    } else {
      [monthText, dayText] = groups.slice(0, 2) as [string, string];
      clock = groups.slice(2, 6) as string[];
      offset = layout.hasZone ? groups[6] ?? '' : '';
      yearText = (layout.hasZone ? groups[7] : groups[6]) ?? '';
    }
    let year = Number(yearText);
    if (layout.isTwoDigitYear) year += year >= 69 ? 1900 : 2000;
    const instant = instantFromFields({
      year, month: monthAbbreviations.indexOf(monthText.toLowerCase()) + 1, day: Number(dayText),
      hour: layout.hasClock ? Number(clock[0]) : 0, minute: layout.hasClock ? Number(clock[1]) : 0,
      second: layout.hasSeconds ? Number(clock[2]) : 0, fraction: layout.hasSeconds ? clock[3] ?? '' : '', offset,
    });
    if (instant) return instant;
  }
  return undefined;
}

/** Rewrite +hh:mm, +hhmm, and GMT+hhmm offsets as +hhmm. */
function numericOffset(field: string): string | undefined {
  let upper = field.toUpperCase();
  if (upper.startsWith('GMT')) upper = upper.slice(3);
  if (upper.startsWith('UTC')) upper = upper.slice(3);
  if (upper.length < 5 || (upper[0] !== '+' && upper[0] !== '-')) return undefined;
  const digits = upper.slice(1).replaceAll(':', '');
  return /^[0-9]{4}$/.test(digits) ? upper[0] + digits : undefined;
}

function instantFromFields(fields: Fields): Instant | undefined {
  const { year, month: monthNumber, day, hour, minute, second } = fields;
  const isLeap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const monthDays = [31, isLeap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (monthNumber < 1 || monthNumber > 12 || day < 1 || day > monthDays[monthNumber - 1]! || hour > 23 || minute > 59 || second > 59) return undefined;
  let offsetSeconds = 0;
  if (fields.offset !== '' && fields.offset !== 'Z') {
    const offset = fields.offset.trim().split(' ')[0]!;
    if (offset !== 'Z') {
      const digits = offset.slice(1).replace(':', '');
      const offsetHours = Number(digits.slice(0, 2));
      const offsetMinutes = Number(digits.slice(2, 4));
      if (offsetHours > 24 || offsetMinutes > 60) return undefined;
      offsetSeconds = (offsetHours * 3600 + offsetMinutes * 60) * (offset[0] === '-' ? -1 : 1);
    }
  }
  const moment = new Date(Date.UTC(2000, monthNumber - 1, day, hour, minute, second));
  moment.setUTCFullYear(year, monthNumber - 1, day);
  const nanoseconds = Number((fields.fraction + '000000000').slice(0, 9));
  return { unixSeconds: Math.floor(moment.getTime() / 1000) - offsetSeconds, nanoseconds };
}

/** Format an instant in UTC as Go time.RFC3339Nano does. */
export function formatInstant(instant: Instant): string {
  const clock = new Date(instant.unixSeconds * 1000).toISOString().slice(0, 19);
  const digits = String(instant.nanoseconds).padStart(9, '0').replace(/0+$/, '');
  return clock + (digits ? '.' + digits : '') + 'Z';
}

/** Parse value and return its RFC 3339 UTC spelling, or undefined when it is missing or unreadable. */
export function optionalTimestamp(value: string): string | undefined {
  const parsed = parseLooseTimestamp(value);
  return parsed ? formatInstant(parsed) : undefined;
}

/** Compare two instants; negative when left is earlier. */
export function compareInstants(left: Instant, right: Instant): number {
  return left.unixSeconds !== right.unixSeconds ? left.unixSeconds - right.unixSeconds : left.nanoseconds - right.nanoseconds;
}
