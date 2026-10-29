import type { ReadingJobState } from './readingJob';
export function validateDetailReadingId(id:string):string { if(!/^[A-Za-z0-9_-]{1,128}$/.test(id)) throw new Error('상세 풀이를 확인할 수 없어요.'); return id; }
const states=new Set<ReadingJobState>(['pending_or_interrupted','ready','failed','unknown','invalidated']);
export type DetailJob={id:string;state:ReadingJobState};
export function parseDetailJob(input:unknown):DetailJob {
 if(!input || typeof input!=='object') throw new Error('상세 요청 상태를 확인할 수 없어요.');
 const v=input as Record<string,unknown>;
 if(typeof v.id!=='string' || !v.id || typeof v.state!=='string' || !states.has(v.state as ReadingJobState)) throw new Error('상세 요청 상태를 확인할 수 없어요.');
 return {id:v.id,state:v.state as ReadingJobState};
}
/** Write the opaque reading ID before sending; recovery only reads status. */
export async function startDetailJobRequest(id:string, save:(id:string)=>Promise<void>, send:(id:string)=>Promise<unknown>):Promise<DetailJob> {
 validateDetailReadingId(id); await save(id); return parseDetailJob(await send(id));
}
export async function recoverDetailJobRequest(id:string, read:(id:string)=>Promise<unknown>):Promise<DetailJob> {
 validateDetailReadingId(id); return parseDetailJob(await read(id));
}

/** Re-read the reading after completion, so the server checks entitlement again. */
export async function resolveDetailJob<T extends {id:string;detail_locked:boolean;detail?:string}>(job:DetailJob,id:string,read:(id:string)=>Promise<T>,clear:()=>Promise<void>):Promise<{job:DetailJob;reading:T|null}> {
 if(job.state!=='ready') return {job,reading:null};
 const result=await read(validateDetailReadingId(id));
 if(result.id!==id || typeof result.detail_locked!=='boolean') throw new Error('상세 풀이 권한을 확인할 수 없어요.');
 const safe=result.detail_locked ? {...result,detail:undefined} : result;
 await clear();
 return {job,reading:safe};
}
