import {test} from 'node:test';
import assert from 'node:assert/strict';
import {parseConversation,outboxFor,linkSubject,replyPending,linkLabel,messageStateText} from '../src/conversation.ts';
const id=n=>String(n).repeat(32).slice(0,32);
const conv={id:id('a'),links:[{id:id('b'),reading_id:'r',domain:'tarot',subject:'self'}],messages:[
 {id:id('d'),seq:2,role:'assistant',content:'',state:'pending'},{id:id('c'),seq:1,role:'user',content:'안녕',state:'complete'}]};
test('conversation is validated, ordered and tied to the requested id',()=>{
 const c=parseConversation(conv,id('a'));
 assert.deepEqual(c.messages.map(m=>m.seq),[1,2]);
 assert.equal(replyPending(c),true);
 assert.throws(()=>parseConversation(conv,id('e')));
 assert.throws(()=>parseConversation({...conv,messages:[...conv.messages,{...conv.messages[0],id:id('f')}]}));
 assert.throws(()=>parseConversation({...conv,links:[{...conv.links[0],subject:'friend'}]}));
});
test('a resend keeps the same request key; new text or conversation gets a new one',()=>{
 let n=0; const key=()=>`k${++n}`;
 const first=outboxFor(null,'c1',' 오늘 힘들었어 ',key);
 assert.deepEqual(first,{conversationId:'c1',key:'k1',content:'오늘 힘들었어'});
 assert.equal(outboxFor(first,'c1','오늘 힘들었어',key),first);
 assert.equal(outboxFor(first,'c1','다른 말',key).key,'k2');
 assert.equal(outboxFor(first,'c2','오늘 힘들었어',key).key,'k3');
 assert.throws(()=>outboxFor(null,'c1','   ',key));
});
test('linked results carry an explicit subject and saju requires a choice',()=>{
 assert.equal(linkSubject('compatibility'),'pair');
 assert.equal(linkSubject('tarot'),'self');
 assert.equal(linkSubject('saju','other'),'other');
 assert.throws(()=>linkSubject('saju'));
 assert.throws(()=>linkSubject('counsel'));
 assert.equal(linkLabel({id:'x',reading_id:'r',domain:'saju',subject:'other'}),'사주 (다른 사람)');
});
test('uncertain replies are described, not retried',()=>{
 assert.match(messageStateText({id:'x',seq:1,role:'assistant',content:'',state:'unknown'}),/자동으로 다시 보내지 않아요/);
});
