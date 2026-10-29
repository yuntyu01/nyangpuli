import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseReadingJob, readingJobMessage } from '../src/readingJob.ts';

test('pending and interrupted jobs stay recoverable without generating again',()=>{
  const pending=parseReadingJob({id:'r1',state:'pending_or_interrupted'});
  assert.equal(pending.state,'pending_or_interrupted');
  assert.match(readingJobMessage(pending.state),/상태를 다시 확인/);
  assert.match(readingJobMessage('unknown'),/자동으로 다시 생성하지/);
  assert.match(readingJobMessage('invalidated'),/기억이 변경/);
});
test('ready status exposes only a valid free summary',()=>{
  const parsed=parseReadingJob({id:'r1',state:'ready',summary:{id:'r1',domain:'counsel',status:'summary_ready',summary:'무료 요약',created_at:'2026-10-29',detail:'서버에서 내려오면 안 되는 상세',detail_locked:false}});
  assert.equal(parsed.summary.summary,'무료 요약');
  assert.equal(parsed.summary.detail,undefined);
  assert.equal(parsed.summary.detail_locked,true);
  assert.throws(()=>parseReadingJob({id:'r1',state:'ready',summary:{id:'other'}}));
  assert.throws(()=>parseReadingJob({id:'r1',state:'unexpected'}));
});

test('a lost creation response is recovered using the saved key without another generation',async()=>{
  const { startReadingJobRequest, recoverReadingJobRequest }=await import('../src/readingJob.ts');
  const steps=[]; let savedKey; let calls=0;
  await assert.rejects(startReadingJobRequest('k1',{question:'example'},async key=>{steps.push('saved');savedKey=key},async key=>{steps.push('sent');calls++;assert.equal(key,'k1');throw new Error('connection lost')}));
  assert.deepEqual(steps,['saved','sent']);
  const resumed=await recoverReadingJobRequest(savedKey,async key=>{assert.equal(key,'k1');return {id:'r1',state:'pending_or_interrupted'}});
  assert.equal(resumed.state,'pending_or_interrupted');assert.equal(calls,1);
});
test('storage failure prevents an untrackable generation request',async()=>{
  const { startReadingJobRequest }=await import('../src/readingJob.ts');
  let sent=false;
  await assert.rejects(startReadingJobRequest('k1',{},async()=>{throw new Error('storage unavailable')},async()=>{sent=true}));
  assert.equal(sent,false);
});

test('an unscoped legacy key remains intact if the current account cannot prove ownership',async()=>{
  const {recoverLegacyReadingJob}=await import('../src/readingJob.ts');let removed=false,saved=false;
  await assert.rejects(recoverLegacyReadingJob('old-key',null,async()=>{throw new Error('not found')},async()=>{saved=true},async()=>{removed=true}));
  assert.equal(saved,false);assert.equal(removed,false);
});
test('verified legacy pending work is saved to the scoped account before removing the old key',async()=>{
  const {recoverLegacyReadingJob}=await import('../src/readingJob.ts');const calls=[];
  const result=await recoverLegacyReadingJob('old-key',null,async()=>({id:'r1',state:'pending_or_interrupted'}),async key=>{calls.push(`save:${key}`)},async()=>{calls.push('remove')});
  assert.equal(result.state,'pending_or_interrupted');assert.deepEqual(calls,['save:old-key','remove']);
  let lookup=false;await assert.rejects(recoverLegacyReadingJob('old-key','different',async()=>{lookup=true},async()=>{},async()=>{}));assert.equal(lookup,false);
});
