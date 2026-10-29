import type { Reading } from './api';

export type ReadingJobState = 'pending_or_interrupted' | 'ready' | 'failed' | 'unknown' | 'invalidated';
export type ReadingJob = { id: string; state: ReadingJobState; summary?: Reading };
const states = new Set<ReadingJobState>(['pending_or_interrupted','ready','failed','unknown','invalidated']);

/** Validate the server status and project only the free summary fields. */
export function parseReadingJob(value: unknown): ReadingJob {
  if (!value || typeof value !== 'object') throw new Error('요청 상태를 확인할 수 없어요.');
  const v = value as Record<string, unknown>;
  if (typeof v.id !== 'string' || !v.id || typeof v.state !== 'string' || !states.has(v.state as ReadingJobState)) throw new Error('요청 상태를 확인할 수 없어요.');
  const state = v.state as ReadingJobState;
  if (state !== 'ready') return { id: v.id, state };
  const summary = v.summary;
  if (!summary || typeof summary !== 'object') throw new Error('준비된 결과를 확인할 수 없어요.');
  const r = summary as Record<string, unknown>;
  if (typeof r.id !== 'string' || r.id !== v.id || typeof r.domain !== 'string' || typeof r.status !== 'string' || typeof r.summary !== 'string' || !r.summary || typeof r.created_at !== 'string') throw new Error('준비된 결과를 확인할 수 없어요.');
  return { id:v.id, state, summary:{ id:r.id, domain:r.domain, status:r.status, summary:r.summary, created_at:r.created_at, detail_locked:true } };
}

export function readingJobMessage(state: ReadingJobState | null): string {
  switch (state) {
    case 'pending_or_interrupted': return '풀이를 준비 중이거나 결과를 확인하지 못했어요. 잠시 뒤 상태를 다시 확인해 주세요.';
    case 'unknown': return '요청의 처리 결과를 확인할 수 없어요. 자동으로 다시 생성하지 않아요.';
    case 'failed': return '풀이를 완성하지 못했어요. 보관함을 확인한 뒤 다시 요청할 수 있어요.';
    case 'invalidated': return '기억이 변경되어 이 풀이를 더 이상 사용할 수 없어요.';
    default: return '요청이 접수됐는지 확인해 주세요. 자동으로 다시 생성하지 않아요.';
  }
}

/** Persist the opaque request key before sending; a lost response can be recovered by that key. */
export async function startReadingJobRequest(key:string, body:unknown, save:(key:string)=>Promise<void>, send:(key:string,body:unknown)=>Promise<unknown>):Promise<ReadingJob> {
  await save(key);
  return parseReadingJob(await send(key,body));
}
/** Recovery only reads status; it never issues a new generation request. */
export async function recoverReadingJobRequest(key:string, read:(key:string)=>Promise<unknown>):Promise<ReadingJob> {
  return parseReadingJob(await read(key));
}

/** A legacy unscoped key is never migrated until an authenticated status lookup succeeds. */
export async function recoverLegacyReadingJob(key:string, currentKey:string|null, read:(key:string)=>Promise<unknown>, save:(key:string)=>Promise<void>, removeLegacy:()=>Promise<void>):Promise<ReadingJob> {
  if (currentKey && currentKey!==key) throw new Error('현재 계정에 확인 중인 다른 요청이 있어요. 먼저 그 요청을 확인해 주세요.');
  const job=await recoverReadingJobRequest(key,read);
  if (job.state!=='ready' && !currentKey) await save(key);
  await removeLegacy();
  return job;
}
