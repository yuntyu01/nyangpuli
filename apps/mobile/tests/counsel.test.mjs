import {test} from 'node:test';
import assert from 'node:assert/strict';
import {counselInput,emptySituation} from '../src/counsel.ts';
test('unanswered situations are not represented as facts',()=>{
 const result=counselInput('고민',emptySituation,'req','2026-10-29T10:00:00Z');
 assert.deepEqual(result.facts,[]);
});
test('explicit situation answers retain source and time and do not infer permission',()=>{
 const result=counselInput(' 연락 ',{safety:'yes',consent:'no',contact:'unknown'},'req','2026-10-29T10:00:00Z');
 assert.deepEqual(result.facts.map(x=>[x.key,x.value]),[['safety','no_threat'],['dialogue_consent','not_confirmed']]);
 assert.ok(result.facts.every(x=>x.source_id.startsWith('situation-req-') && x.observed_at==='2026-10-29T10:00:00Z'));
});
