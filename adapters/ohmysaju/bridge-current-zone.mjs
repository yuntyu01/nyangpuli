// Explicit local-time daily reference. Both natal and transit calculations use
// the pinned OhMySaju public calculateSaju API. This does not invoke or claim to
// reproduce calculateSajuDailyTransit, whose noon uses the natal time zone.
import { resolve, dirname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const pinned = 'a8c0cf64adfde8fb1ce253112529edd8e90cc94b';
const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const engineDir = process.env.OHMYSAJU_DIR || resolve(root, '.upstream/oh-my-saju');
try {
  let inputText = '';
  for await (const chunk of process.stdin) inputText += chunk;
  const input = JSON.parse(inputText);
  const { calculateSaju } = await import(pathToFileURL(resolve(engineDir, 'dist/index.js')).href);
  const b = input.birth;
  if (!Number.isInteger(b?.hour) || !input.date || typeof input.daily_time_zone !== 'string' || !input.daily_time_zone) throw new Error('Invalid request');
  const date = b.calendar === 'korean-lunar'
    ? { calendar: 'korean-lunar', year: b.year, month: b.month, day: b.day, monthKind: b.month_kind }
    : { calendar: 'gregorian', year: b.year, month: b.month, day: b.day };
  const rules = { ziHourPolicy: 'civilMidnight', dayHourClock: { kind: 'civil' } };
  const chart = calculateSaju({ birth: { date, time: { hour: b.hour, minute: b.minute }, timeZone: b.time_zone }, rules });
  const transit = calculateSaju({ birth: { date: { calendar: 'gregorian', ...input.date }, time: { hour: 12, minute: 0, second: 0, millisecond: 0 }, timeZone: input.daily_time_zone }, rules });
  if (chart.audit.engine.sourceRevision !== pinned || transit.audit.engine.sourceRevision !== pinned) throw new Error('Unpinned engine');
  // Expose only audited transit pillars and chronology. Ten-gods in a transit
  // chart use that chart's own day master, so they are not natal relationships.
  // Do not fabricate or mislabel ten-gods or interactions with the natal chart.
  const c = transit.chronology;
  const daily = {
    schemaVersion: 'nyangpuli-local-noon-transit-v1',
    date: { calendar: 'gregorian', ...input.date },
    representative: { policy: 'local-civil-noon', localTime: '12:00:00.000', civilDateTime: c.civilDateTime, dayHourDateTime: c.dayHourDateTime, instantUtc: c.instantUtc, epochMilliseconds: c.epochMilliseconds, timeZone: c.timeZone, offsetSeconds: c.offsetSeconds, dayHourClock: c.dayHourClock, solarTimeCorrection: c.solarTimeCorrection },
    pillars: { year: transit.pillars.year, month: transit.pillars.month, day: transit.pillars.day },
    tenGods: null,
    relationships: null,
    warnings: transit.warnings,
    notes: ['The representative instant is civil noon in the current IANA time zone.', 'Natal relationships and ten-gods are unavailable in this independently calculated transit; do not infer or state them as computed facts.'],
    audit: { engine: transit.audit.engine, calculation: 'public calculateSaju local-noon transit; distinct from upstream calculateSajuDailyTransit', rules: transit.audit.rules, evidence: transit.audit.evidence },
  };
  process.stdout.write(JSON.stringify({ chart, daily }));
} catch (error) {
  const allowed = new Set(['INVALID_LEAP_MONTH', 'INVALID_DATE', 'UNSUPPORTED_DATE_RANGE']);
  process.stderr.write(JSON.stringify({ error: allowed.has(error?.code) ? error.code : 'SAJU_ADAPTER_ERROR' }));
  process.exitCode = 1;
}
