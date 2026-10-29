import { test } from 'node:test';
import assert from 'node:assert/strict';
import { birthInput, todayInZone } from '../src/birth.ts';
const base = {year:'2000',month:'2',day:'29',hour:'0',minute:'0',timeZone:'Asia/Seoul'};
test('leap day and midnight are retained', () => assert.deepEqual(birthInput(base), {year:2000,month:2,day:29,hour:0,minute:0,time_zone:'Asia/Seoul'}));
test('unknown time, impossible date, and invalid zone are rejected', () => {
  for(const changes of [{hour:''},{year:'2001'},{timeZone:'invalid/zone'}]) assert.throws(()=>birthInput({...base,...changes}));
});
test('daily date follows selected zone across midnight', () => {
  const instant = new Date('2026-01-01T16:00:00Z');
  assert.deepEqual(todayInZone('Asia/Seoul',instant),{year:2026,month:1,day:2});
  assert.deepEqual(todayInZone('America/Los_Angeles',instant),{year:2026,month:1,day:1});
});
test('lunar dates retain explicit regular and leap months without guessing existence',()=>{
 const lunar={...base,year:'2023',month:'2',day:'30',calendar:'korean-lunar',monthKind:'leap'};
 assert.deepEqual(birthInput(lunar),{year:2023,month:2,day:30,hour:0,minute:0,time_zone:'Asia/Seoul',calendar:'korean-lunar',month_kind:'leap'});
 assert.equal(birthInput({...lunar,monthKind:'regular'}).month_kind,'regular');
 for(const changes of [{monthKind:undefined},{day:'31'},{year:'1800'}]) assert.throws(()=>birthInput({...lunar,...changes}));
});
