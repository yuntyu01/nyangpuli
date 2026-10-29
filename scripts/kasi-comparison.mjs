// Compares the pinned original engine with a third-party-collected KASI-derived
// dataset. This is not a raw KASI response or a live API call.
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import assert from 'node:assert/strict';
const fixture = readFileSync('fixtures/kasi/collector-dataset.json');
assert.equal(createHash('sha256').update(fixture).digest('hex'), 'd651d5a77d7970cde4b36f414995b6ea833b4d50760f23fe0f462c96fdf8ca1a');
const rows = JSON.parse(fixture);
assert.equal(rows.length, 200);
const root = resolve(process.env.OHMYSAJU_DIR || '.upstream/oh-my-saju');
const { solarToLunar, lunarToSolar } = await import(pathToFileURL(resolve(root, 'dist/calendar.js')));
const { calculateSaju } = await import(pathToFileURL(resolve(root, 'dist/index.js')));
const excluded = new Set(['1637-6-13', '1643-3-13']);
let converted = 0, dayPillars = 0;
for (const row of rows) {
 const [year, month, day] = row.solar;
 if (!excluded.has(row.solar.join('-'))) {
  assert.deepEqual(solarToLunar(year, month, day), {year: row.lunar[0], month: row.lunar[1], day: row.lunar[2], isLeapMonth: row.leap}, `solar ${row.solar}`);
  assert.deepEqual(lunarToSolar(...row.lunar, row.leap), {year, month, day}, `lunar ${row.lunar}`);
  converted++;
 }
 if (year >= 1801 && year <= 2100) {
  const report = calculateSaju({birth:{date:{calendar:'gregorian',year,month,day},time:{hour:12,minute:0},timeZone:'Asia/Seoul'}});
  assert.equal(report.audit.engine.sourceRevision, 'a8c0cf64adfde8fb1ce253112529edd8e90cc94b');
  assert.equal(report.pillars.day.korean, row.ko[2], `day ${row.solar}`);
  dayPillars++;
 }
}
assert.equal(converted,198); assert.equal(dayPillars,115);
console.log(JSON.stringify({status:'passed',source:'third_party_collected_dataset_not_raw_official_response',rows:200,bidirectional_matches:converted,known_historical_differences:2,day_pillars:dayPillars,live_api:false}));
