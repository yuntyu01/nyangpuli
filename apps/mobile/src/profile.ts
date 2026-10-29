import { birthInput } from './birth.ts';
import type { Birth } from './birth.ts';
export type Profile = {id:string;version:number;birth:{year:number;month:number;day:number;hour:number;minute:number;time_zone:string;calendar?:'gregorian'|'korean-lunar';month_kind?:'regular'|'leap'}};
export function parseProfile(input:unknown): Profile {
  if (!input || typeof input!=='object') throw new Error('출생 정보를 확인할 수 없어요.');
  const p=input as Record<string,unknown>;
  if(typeof p.id!=='string' || !/^[a-f0-9]{32}$/.test(p.id) || !Number.isInteger(p.version) || (p.version as number)<1 || !p.birth || typeof p.birth!=='object') throw new Error('출생 정보를 확인할 수 없어요.');
  const b=p.birth as Record<string,unknown>;
  if(['year','month','day','hour','minute'].some(k=>typeof b[k]!=='number' || !Number.isInteger(b[k])) || typeof b.time_zone!=='string') throw new Error('출생 정보를 확인할 수 없어요.');
  if(b.calendar!==undefined && b.calendar!=='' && b.calendar!=='gregorian' && b.calendar!=='korean-lunar') throw new Error('출생 정보를 확인할 수 없어요.');
  birthInput({year:String(b.year),month:String(b.month),day:String(b.day),hour:String(b.hour),minute:String(b.minute),timeZone:b.time_zone,...(b.calendar==='korean-lunar' ? {calendar:'korean-lunar' as const,monthKind:b.month_kind as 'regular'|'leap'} : {})});
  return p as Profile;
}
export function profileBirthForm(p:Profile):Birth {return {year:String(p.birth.year),month:String(p.birth.month),day:String(p.birth.day),hour:String(p.birth.hour),minute:String(p.birth.minute),timeZone:p.birth.time_zone,...(p.birth.calendar==='korean-lunar'?{calendar:'korean-lunar' as const,monthKind:p.birth.month_kind}:{})};}
export function dailyRequest(profileId:string, timeZone:string, _birthTimeZone:string) {
  if(!profileId || !timeZone.trim()) throw new Error('프로필과 현재 시간대를 확인해 주세요.');
  try { new Intl.DateTimeFormat('en',{timeZone}).format(); } catch { throw new Error('현재 시간대를 확인해 주세요.'); }
  return {profile_id:profileId,time_zone:timeZone};
}

export function parseProfiles(input:unknown):Profile[] {
  if(!Array.isArray(input)) throw new Error('출생 정보 목록을 확인할 수 없어요.');
  const profiles=input.map(parseProfile);
  if(new Set(profiles.map(p=>p.id)).size!==profiles.length) throw new Error('출생 정보 목록을 확인할 수 없어요.');
  return profiles;
}
// The daily endpoint is summary-only. Never display its detail fields until a
// separate authenticated reading lookup confirms the same reading's access.
export function resolveDailyReading<T extends {id:string;detail?:string;detail_locked:boolean}>(summary:T, checked:T|null) {
  if (checked && checked.id===summary.id) return {reading:checked,verified:true};
  return {reading:{...summary,detail:undefined,detail_locked:true},verified:false};
}
