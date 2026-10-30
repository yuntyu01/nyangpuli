export type Memory = {id:string;key:string;value:string;source_event_id:string;observed_at:string;note?:string;class?:'fact'|'episode';origin?:'user_confirmed'|'user_stated'|'ai_inference';subject?:string;situation?:string;source_message_id?:string};
const keyLabels: Record<string,string> = {relationship_status:'관계 상태',contact_boundary:'연락 경계',relationship_event:'관계 사건',preference:'선호',personal_fact:'내 정보',event:'있었던 일',feeling:'그때의 마음',attempt:'해 본 방법',wish:'바람'};
/** Facts keep only the latest value; episodes are kept per time and situation. */
export function memoryLabel(m: Memory): string {
  const kind = keyLabels[m.key] ?? '기억';
  const origin = m.origin==='ai_inference' ? 'AI 추정 · 확인 전' : m.origin==='user_stated' ? '대화에서 직접 말함' : '직접 확인함';
  return `${kind} · ${m.class==='episode' ? '그때의 기록' : '현재 사실'} · ${origin}`;
}
export function memoryContext(m: Memory): string {
  return [m.subject && `대상: ${m.subject}`, m.situation && `상황: ${m.situation}`].filter(Boolean).join(' · ');
}
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
