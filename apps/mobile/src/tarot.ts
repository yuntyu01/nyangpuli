export type Spread = 'single-focus' | 'past-present-future';
export type TarotCard = {position:number;card_id:number;name:string;reversed:boolean};
export type TarotMeaning = {card_id:number;name:string;orientation:string;position:string;meaning:string;source:string};
export type TarotSession = {id:string;question:string;spread:Spread;selected:TarotCard[];complete:boolean;meanings?:TarotMeaning[]};
export function requiredCards(spread: Spread) { return spread === 'single-focus' ? 1 : 3; }
/** Reject malformed or contradictory session data before offering another selection. */
export function parseTarotSession(input: unknown): TarotSession {
  if (!input || typeof input !== 'object') throw new Error('카드 상태를 확인할 수 없어요.');
  const s = input as Record<string, unknown>;
  if (typeof s.id !== 'string' || !/^[a-f0-9]{32}$/.test(s.id) || typeof s.question !== 'string' || !['single-focus','past-present-future'].includes(String(s.spread)) || !Array.isArray(s.selected) || typeof s.complete !== 'boolean') throw new Error('카드 상태를 확인할 수 없어요.');
  const spread=s.spread as Spread;
  const positions=new Set<number>(); const cards=new Set<number>();
  for (const value of s.selected) {
    if (!value || typeof value !== 'object') throw new Error('카드 상태를 확인할 수 없어요.');
    const card=value as Record<string,unknown>;
    if (!Number.isInteger(card.position) || (card.position as number)<0 || (card.position as number)>=78 || !Number.isInteger(card.card_id) || (card.card_id as number)<0 || (card.card_id as number)>=78 || typeof card.name!=='string' || typeof card.reversed!=='boolean' || positions.has(card.position as number) || cards.has(card.card_id as number)) throw new Error('카드 상태를 확인할 수 없어요.');
    positions.add(card.position as number); cards.add(card.card_id as number);
  }
  const count=requiredCards(spread);
  if (s.selected.length>count || s.complete!==(s.selected.length===count) || (s.complete && (!Array.isArray(s.meanings) || s.meanings.length!==count))) throw new Error('카드 상태를 확인할 수 없어요.');
  if (Array.isArray(s.meanings) && s.meanings.some(m=>!m || typeof m !== 'object' || typeof m.meaning !== 'string' || typeof m.position !== 'string' || typeof m.source !== 'string')) throw new Error('카드 상태를 확인할 수 없어요.');
  return s as TarotSession;
}
export function availablePosition(session: TarotSession, position: number): boolean {
  return Number.isInteger(position) && position>=0 && position<78 && !session.complete && !session.selected.some(c=>c.position===position);
}
