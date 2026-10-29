#!/usr/bin/env node
// Frozen upstream observations for a future Go replacement. No LLM or network.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const inputsPath = join(root, 'fixtures/engine-regression-inputs.json');
const goldenPath = join(root, 'fixtures/engine-regression-golden.json');
const pinned = 'a8c0cf64adfde8fb1ce253112529edd8e90cc94b';

function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().map(k => [k, canonical(value[k])]));
  return value;
}
function digest(value) { return createHash('sha256').update(value).digest('hex'); }
function required(object, keys, location) {
  if (!object || typeof object !== 'object') throw new Error(`Missing ${location}`);
  return Object.fromEntries(keys.map(key => {
    if (!Object.hasOwn(object, key)) throw new Error(`Missing ${location}.${key}`);
    return [key, object[key]];
  }));
}
function pillars(value, names) {
  return Object.fromEntries(names.map(name => [name, required(value?.[name], ['cycleIndex', 'korean', 'hanja', 'stem', 'branch'], `pillars.${name}`)]));
}
function projection(value) {
  const c = value.chart;
  const out = {
    chart: {
      chronology: required(c?.chronology, ['gregorianDate', 'koreanLunarDate', 'civilDateTime', 'instantUtc', 'epochMilliseconds', 'timeZone', 'offsetSeconds', 'daylightSaving', 'disambiguation', 'dayHourDateTime', 'dayHourClock', 'solarTimeCorrection'], 'chart.chronology'),
      pillars: pillars(c?.pillars, ['year', 'month', 'day', 'hour']),
      facts: required(c?.facts, ['dayMaster', 'tenGods', 'voidBranches', 'structure'], 'chart.facts'),
      warning_codes: c.warnings.map(w => w.code).sort(),
    },
    daily: null,
  };
  if (value.daily !== undefined) {
    const d = value.daily;
    out.daily = {
      date: d.date,
      representative: required(d.representative, ['policy', 'civilDateTime', 'instantUtc', 'epochMilliseconds', 'timeZone', 'offsetSeconds', 'dayHourDateTime', 'dayHourClock', 'solarTimeCorrection'], 'daily.representative'),
      pillars: pillars(d.pillars, ['year', 'month', 'day']),
      tenGods: d.tenGods,
      relationships: d.relationships,
      warning_codes: d.warnings.map(w => w.code).sort(),
    };
  }
  return canonical(out);
}
function pathValue(value, path) { return path.split('.').reduce((v, key) => v?.[key], value); }
function provenance(engineDir) {
  const git = args => {
    const result = spawnSync('git', ['-C', engineDir, ...args], { encoding: 'utf8', timeout: 5000 });
    if (result.error || result.status !== 0) throw new Error('Upstream Git checkout unavailable; run setup-saju.sh.');
    return result.stdout.trim();
  };
  if (git(['rev-parse', 'HEAD']) !== pinned || git(['status', '--porcelain', '--untracked-files=no']) !== '') throw new Error('Upstream source must be the pinned clean checkout.');
  const dist = join(engineDir, 'dist');
  const hashes = {};
  function walk(folder) {
    for (const item of readdirSync(folder, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      const path = join(folder, item.name);
      if (item.isDirectory()) walk(path);
      else if (item.isFile()) hashes[relative(dist, path)] = digest(readFileSync(path));
      else throw new Error('Unexpected symlink/special file in upstream dist.');
    }
  }
  walk(dist);
  const packageInfo = JSON.parse(readFileSync(join(engineDir, 'package.json'), 'utf8'));
  return {
    source_revision: pinned,
    node_version: process.version,
    package_manager: packageInfo.packageManager ?? null,
    pnpm_lock_sha256: digest(readFileSync(join(engineDir, 'pnpm-lock.yaml'))),
    build_tree_sha256: digest(JSON.stringify(canonical(hashes))),
    build_file_count: Object.keys(hashes).length,
    adapter_sha256: digest(readFileSync(join(root, 'adapters/ohmysaju/bridge.mjs'))),
  };
}
function invoke(command, args, fixture, original) {
  const result = spawnSync(command, args, { cwd: root, input: JSON.stringify(fixture.input), encoding: 'utf8', timeout: 15000, maxBuffer: 4 * 1024 * 1024, shell: false });
  if (result.error || result.signal) throw new Error(`${fixture.id}: adapter execution failed or timed out`);
  let value;
  if (result.status === 0) {
    const raw = JSON.parse(result.stdout);
    if (original && raw.chart?.audit?.engine?.sourceRevision !== pinned) throw new Error(`${fixture.id}: source revision mismatch`);
    value = { outcome: 'success', semantic: projection(raw) };
  } else {
    const raw = JSON.parse(result.stderr);
    if (typeof raw.error !== 'string' || raw.error === '') throw new Error(`${fixture.id}: adapter failure must have a JSON error code`);
    value = { outcome: 'error', error_code: raw.error };
  }
  assert.equal(value.outcome, fixture.expected_outcome, `${fixture.id}: unexpected success/failure`);
  if (fixture.expected_error) assert.equal(value.error_code, fixture.expected_error, `${fixture.id}: error code`);
  for (const [path, expected] of Object.entries(fixture.assertions ?? {})) assert.deepEqual(pathValue(value.semantic, path), expected, `${fixture.id}: ${path}`);
  return value;
}
function parseArgs(args) {
  const options = { record: false, candidate: null, args: [] };
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--record') options.record = true;
    else if ((args[i] === '--candidate' || args[i] === '--arg') && i + 1 < args.length) {
      const option = args[i++];
      if (option === '--candidate') options.candidate = args[i];
      else options.args.push(args[i]);
    } else throw new Error('usage: node scripts/engine-regression.mjs [--record | --candidate EXECUTABLE [--arg VALUE]...]');
  }
  if (options.record && options.candidate) throw new Error('--record only records the original pinned adapter, never a candidate.');
  if (options.args.length && !options.candidate) throw new Error('--arg requires --candidate.');
  return options;
}
function firstDifference(a, b, path = '$') {
  if (JSON.stringify(a) === JSON.stringify(b)) return null;
  if (!a || !b || typeof a !== 'object' || typeof b !== 'object') return path;
  for (const key of [...new Set([...Object.keys(a), ...Object.keys(b)])].sort()) {
    const found = firstDifference(a[key], b[key], `${path}.${key}`);
    if (found) return found;
  }
  return path;
}

try {
  const options = parseArgs(process.argv.slice(2));
  const inputText = readFileSync(inputsPath, 'utf8');
  const inputs = JSON.parse(inputText);
  assert.equal(inputs.schema_version, 1);
  assert.equal(inputs.source_revision, pinned);
  assert(inputs.cases.length >= 12 && inputs.cases.length <= 20);
  assert.equal(new Set(inputs.cases.map(c => c.id)).size, inputs.cases.length, 'case IDs must be unique');
  const isOriginal = !options.candidate;
  const command = options.candidate ?? process.execPath;
  const args = options.candidate ? options.args : [join(root, 'adapters/ohmysaju/bridge.mjs')];
  const originalProvenance = isOriginal ? provenance(process.env.OHMYSAJU_DIR || join(root, '.upstream/oh-my-saju')) : null;
  const observed = {};
  for (const fixture of inputs.cases) observed[fixture.id] = invoke(command, args, fixture, isOriginal);
  const inputHash = digest(inputText);
  if (options.record) {
    writeFileSync(goldenPath, JSON.stringify({
      schema_version: 1,
      purpose: 'original_engine_compatibility_only_not_independent_truth_or_future_accuracy',
      recorded_at: new Date().toISOString(),
      inputs_sha256: inputHash,
      provenance: originalProvenance,
      projection_version: 1,
      results: observed,
    }, null, 2) + '\n');
    console.log(JSON.stringify({ status: 'recorded', cases: inputs.cases.length, independent_accuracy_claim: false }));
  } else {
    const golden = JSON.parse(readFileSync(goldenPath, 'utf8'));
    assert.equal(golden.schema_version, 1);
    assert.equal(golden.projection_version, 1);
    assert.equal(golden.provenance.source_revision, pinned);
    assert.equal(golden.inputs_sha256, inputHash, 'Inputs changed: do not silently reuse or overwrite the reference.');
    assert.deepEqual(Object.keys(observed).sort(), Object.keys(golden.results).sort(), 'golden case set differs');
    if (isOriginal) {
      for (const key of ['pnpm_lock_sha256', 'build_tree_sha256', 'adapter_sha256']) assert.equal(originalProvenance[key], golden.provenance[key], `Original provenance changed: ${key}`);
    }
    const failed = [];
    for (const [id, result] of Object.entries(observed)) {
      const difference = firstDifference(canonical(golden.results[id]), canonical(result));
      if (difference) failed.push({ id, first_difference: difference });
    }
    const report = { status: failed.length ? 'failed' : 'passed', runner: isOriginal ? 'pinned_original' : 'candidate', cases: inputs.cases.length, passed: inputs.cases.length - failed.length, failed, reference_revision: pinned, independent_accuracy_claim: false };
    console.log(JSON.stringify(report, null, 2));
    if (failed.length) process.exitCode = 1;
  }
} catch (error) {
  console.error(`Engine regression failed: ${error.message}`);
  process.exitCode = 1;
}
