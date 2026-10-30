export type LinkSubject = 'self'|'other'|'pair';
export type ConversationLink = {id:string;reading_id:string;domain:string;subject:LinkSubject};
export type ChatMessage = {id:string;seq:number;role:'user'|'assistant';content:string;state:'complete'|'pending'|'failed'|'unknown'};
export type Conversation = {id:string;links:ConversationLink[];messages:ChatMessage[]};
export type ConversationItem = {id:string;updated_at:string;preview:string;link_domains:string[]};
export type Outbox = {conversationId:string;key:string;content:string};

const bad = () => new Error('대화 상태를 확인할 수 없어요.');
const id32 = (v:unknown) => typeof v==='string' && /^[a-f0-9]{32}$/.test(v);

/** Reject malformed or mixed-up conversation data before showing it. */
export function parseConversation(input:unknown, expectedId?:string): Conversation {
  if (!input || typeof input!=='object') throw bad();
  const c = input as Record<string,unknown>;
  if (!id32(c.id) || (expectedId && c.id!==expectedId) || !Array.isArray(c.links) || !Array.isArray(c.messages)) throw bad();
  const seqs = new Set<number>();
  for (const m of c.messages as Record<string,unknown>[]) {
    if (!m || !id32(m.id) || !Number.isInteger(m.seq) || seqs.has(m.seq as number) || !['user','assistant'].includes(String(m.role)) || typeof m.content!=='string' || !['complete','pending','failed','unknown'].includes(String(m.state))) throw bad();
    seqs.add(m.seq as number);
  }
  for (const l of c.links as Record<string,unknown>[]) {
    if (!l || !id32(l.id) || typeof l.reading_id!=='string' || typeof l.domain!=='string' || !['self','other','pair'].includes(String(l.subject))) throw bad();
  }
  return {id:c.id as string, links:c.links as ConversationLink[], messages:[...(c.messages as ChatMessage[])].sort((a,b)=>a.seq-b.seq)};
}

export function parseConversationList(input:unknown): ConversationItem[] {
  if (!Array.isArray(input)) throw bad();
  for (const item of input) if (!item || !id32(item.id) || typeof item.preview!=='string' || !Array.isArray(item.link_domains)) throw bad();
  return input as ConversationItem[];
}

/** A reply is still being generated when the newest assistant message is pending. */
export function replyPending(c:Conversation|null): boolean {
  const last = c?.messages.filter(m=>m.role==='assistant').at(-1);
  return last?.state==='pending';
}

/**
 * A new message keeps its request key until the server confirms it, so a resend
 * after a network error resolves to the same turn instead of a second reply.
 */
export function outboxFor(current:Outbox|null, conversationId:string, content:string, newKey:()=>string): Outbox {
  const text = content.trim();
  if (!text) throw new Error('하고 싶은 말을 입력해 주세요.');
  if ([...text].length>4000) throw new Error('메시지가 너무 길어요.');
  if (current && current.conversationId===conversationId && current.content===text) return current;
  return {conversationId, key:newKey(), content:text};
}

/** Whose result is linked. Saju may describe another person, so it must be chosen. */
export function linkSubject(domain:string, choice?:'self'|'other'): LinkSubject {
  if (domain==='compatibility') return 'pair';
  if (domain==='daily' || domain==='tarot') return 'self';
  if (domain==='saju' && (choice==='self' || choice==='other')) return choice;
  throw new Error('누구의 결과인지 선택해 주세요.');
}

export function linkLabel(l:ConversationLink): string {
  const domain = ({saju:'사주',daily:'오늘의 운세',compatibility:'궁합',tarot:'타로'} as Record<string,string>)[l.domain] ?? '풀이';
  const who = l.subject==='self' ? '나' : l.subject==='pair' ? '두 사람' : '다른 사람';
  return `${domain} (${who})`;
}

export function messageStateText(m:ChatMessage): string {
  if (m.state==='pending') return '답을 생각하고 있어요…';
  if (m.state==='failed') return '답변을 만들지 못했어요. 다시 말해 주세요.';
  if (m.state==='unknown') return '답변 결과를 확인할 수 없어요. 자동으로 다시 보내지 않아요.';
  return m.content;
}
