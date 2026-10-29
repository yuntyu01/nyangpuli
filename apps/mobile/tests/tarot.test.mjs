import {test} from 'node:test';
import assert from 'node:assert/strict';
import {availablePosition,parseTarotSession,requiredCards} from '../src/tarot.ts';
const id='1234567890abcdef1234567890abcdef';
const card={position:3,card_id:20,name:'Judgement',reversed:true};
const partial={id,question:'질문',spread:'past-present-future',selected:[card],complete:false};
test('partial draw restores the same card and refuses the same position',()=>{
 const session=parseTarotSession(partial);
 assert.deepEqual(session.selected,[card]);
 assert.equal(availablePosition(session,3),false);
 assert.equal(availablePosition(session,4),true);
 assert.equal(availablePosition(session,78),false);
 assert.equal(requiredCards(session.spread),3);
});
test('complete single draw preserves orientation and blocks further selection',()=>{
 const session=parseTarotSession({id,question:'질문',spread:'single-focus',selected:[card],complete:true,meanings:[{card_id:20,name:'Judgement',orientation:'reversed',position:'Focus',meaning:'meaning',source:'source'}]});
 assert.equal(session.selected[0].reversed,true);
 assert.equal(availablePosition(session,4),false);
});
test('invalid, duplicate, or contradictory server states do not permit selection',()=>{
 for(const changes of [{selected:[card,card]},{complete:true},{id:'other'},{spread:'unknown'}]) assert.throws(()=>parseTarotSession({...partial,...changes}));
});
