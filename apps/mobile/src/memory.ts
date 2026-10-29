export type Memory = {id:string;key:string;value:string;source_event_id:string;observed_at:string;note?:string};
export type MemoryRevision = {fact:{value:string;observed_at:string};changed_at:string};
/** A correction preserves its original source identity and event time. */
export function correctionPayload(memory: Memory, value: string) {
  if (!memory.id || !memory.key || !memory.source_event_id || !memory.observed_at || !value.trim()) throw new Error('기억 내용을 입력하고 다시 확인해 주세요.');
  return {key:memory.key,value:value.trim(),source_event_id:memory.source_event_id,observed_at:memory.observed_at,...(memory.note ? {note:memory.note}: {})};
}
export function explicitMemoryPayload(key:string, value:string, eventId:string, observedAt:string) {
  if (!['relationship_status','contact_boundary','relationship_event','preference'].includes(key) || !value.trim() || !eventId || !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?Z$/.test(observedAt) || !Number.isFinite(Date.parse(observedAt))) throw new Error('기억 내용과 사건 시각을 확인해 주세요.');
  const normalized=new Date(observedAt).toISOString();
  // Date.parse normalizes impossible dates, so ensure date/time components match.
  if(normalized.slice(0,19)!==observedAt.slice(0,19)) throw new Error('사건 시각을 확인해 주세요.');
  return {key,value:value.trim(),source_event_id:eventId,observed_at:normalized};
}
