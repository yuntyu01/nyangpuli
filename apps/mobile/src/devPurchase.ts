export type SimulatedPurchase = {transaction_id:string;reading_id:string;status:'pending'|'cancelled'|'purchased'|'refunded'};
export function testPurchaseEnabled(development:boolean, flag:string|undefined) { return development && flag==='1'; }
export function simulatedPurchase(readingId:string, transactionId:string) {
  if (!readingId.trim() || !transactionId.trim()) throw new Error('테스트 요청을 준비할 수 없어요.');
  return {reading_id:readingId,transaction_id:transactionId,status:'purchased' as const};
}
export function parseSimulatedPurchases(value:unknown):SimulatedPurchase[] {
  if (!value || typeof value!=='object') throw new Error('테스트 기록을 확인할 수 없어요.');
  const obj=value as Record<string,unknown>;
  if (obj.kind!=='development_simulation_not_store_restore' || !Array.isArray(obj.purchases)) throw new Error('테스트 기록을 확인할 수 없어요.');
  for(const entry of obj.purchases) {
    if (!entry || typeof entry!=='object' || typeof entry.reading_id!=='string' || !entry.reading_id || typeof entry.transaction_id!=='string' || !entry.transaction_id || !['pending','cancelled','purchased','refunded'].includes(entry.status)) throw new Error('테스트 기록을 확인할 수 없어요.');
  }
  return obj.purchases as SimulatedPurchase[];
}
