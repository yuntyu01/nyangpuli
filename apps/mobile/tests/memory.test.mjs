import {test} from 'node:test';
import assert from 'node:assert/strict';
import {correctionPayload,explicitMemoryPayload} from '../src/memory.ts';
const fact={id:'a',key:'contact_boundary',value:'이전',source_event_id:'event-1',observed_at:'2026-10-01T12:00:00Z',note:'사용자 입력'};
test('correction retains source and original event time',()=>{
 assert.deepEqual(correctionPayload(fact,'  지금은 연락하지 않음 '),{key:'contact_boundary',value:'지금은 연락하지 않음',source_event_id:'event-1',observed_at:'2026-10-01T12:00:00Z',note:'사용자 입력'});
 assert.throws(()=>correctionPayload(fact,'  '));
});
test('explicit events require a valid event date and source identity',()=>{
 assert.deepEqual(explicitMemoryPayload('relationship_status','연애 중','event','2026-10-01T12:00:00Z'),{key:'relationship_status',value:'연애 중',source_event_id:'event',observed_at:'2026-10-01T12:00:00.000Z'});
 for(const args of [['relationship_status','a','e','2026-02-30T12:00:00Z'],['relationship_status','a','','2026-01-01T12:00:00Z'],['bad','a','e','2026-01-01T12:00:00Z']]) assert.throws(()=>explicitMemoryPayload(...args));
});
