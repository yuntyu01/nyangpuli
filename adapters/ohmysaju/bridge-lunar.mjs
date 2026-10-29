// Korean lunar birth adapter. Nyangpuli's adapter. OhMySaju remains TypeScript/Node; this is not a Go port.
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
  const { calculateSajuDailyTransit } = await import(pathToFileURL(resolve(engineDir, 'dist/timing.js')).href);
  const b = input.birth;
  if (b?.calendar !== 'korean-lunar' || !['regular', 'leap'].includes(b?.month_kind)) throw new Error('Explicit Korean lunar month kind is required.');
  if (!Number.isInteger(b?.hour)) throw new Error('Exact birth hour is required; unknown time must not be replaced by noon.');
  const natalRequest = {
    birth: {
      date: { calendar: 'korean-lunar', year: b.year, month: b.month, day: b.day, monthKind: b.month_kind },
      time: { hour: b.hour, minute: b.minute },
      timeZone: b.time_zone,
    },
    rules: { ziHourPolicy: 'civilMidnight', dayHourClock: { kind: 'civil' } },
  };
  const chart = calculateSaju(natalRequest);
  if (chart.audit.engine.sourceRevision !== pinned) {
    throw new Error('OhMySaju source revision is not the pinned clean build. Run bash scripts/setup-saju.sh.');
  }
  const daily = input.date ? calculateSajuDailyTransit({ natalRequest, date: { calendar: 'gregorian', ...input.date } }) : undefined;
  process.stdout.write(JSON.stringify({ chart, daily }));
} catch (error) {
  // No request/birth details are written to stderr.
  process.stderr.write(JSON.stringify({ error: String(error?.code || 'SAJU_ADAPTER_ERROR'), message: String(error?.message || error) }));
  process.exitCode = 1;
}
