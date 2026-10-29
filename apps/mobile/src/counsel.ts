export type Answer = 'yes' | 'no' | 'unknown';
export type Situation = {safety: Answer; consent: Answer; contact: Answer};
export const emptySituation: Situation = {safety:'unknown',consent:'unknown',contact:'unknown'};

// Each value comes from a user-selected answer in this request. Unknown is not
// converted into a fact or treated as permission to contact someone.
export function counselInput(question:string, situation:Situation, requestKey:string, observedAt:string) {
 if (!question.trim() || !requestKey || !Number.isFinite(Date.parse(observedAt))) throw new Error('고민을 확인해 주세요.');
 const rows: [keyof Situation,string,string,string][] = [
  ['safety','safety','no_threat','threat_or_concern'],
  ['consent','dialogue_consent','mutual','not_confirmed'],
  ['contact','contact_boundary','allowed','unconfirmed'],
 ];
 return {question:question.trim(),include_research:true,facts:rows.flatMap(([answer,key,yes,no])=>situation[answer]==='unknown'?[]:[{
  key,value:situation[answer]==='yes'?yes:no,source_id:`situation-${requestKey}-${key}`,observed_at:observedAt,
 }])};
}
