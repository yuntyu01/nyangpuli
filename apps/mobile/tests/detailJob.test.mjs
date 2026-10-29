import {test} from 'node:test';
import assert from 'node:assert/strict';
import {parseDetailJob,startDetailJobRequest,recoverDetailJobRequest,resolveDetailJob} from '../src/detailJob.ts';

test('lost detail start response is recovered by reading ID without a second POST',async()=>{
 let saved=null,posts=0;
 await assert.rejects(startDetailJobRequest('reading-1',async id=>{saved=id},async()=>{posts++;throw new Error('connection lost')}));
 assert.equal(saved,'reading-1');
 const job=await recoverDetailJobRequest(saved,async id=>{assert.equal(id,'reading-1');return {id:'job-1',state:'pending_or_interrupted',detail:'must not escape'}});
 assert.deepEqual(job,{id:'job-1',state:'pending_or_interrupted'});assert.equal(posts,1);
});
test('secure storage failure prevents an untrackable detail generation',async()=>{
 let sent=false;await assert.rejects(startDetailJobRequest('r1',async()=>{throw new Error('storage')},async()=>{sent=true}));assert.equal(sent,false);
});
test('ready detail rechecks entitlement, and a revoked result never exposes detail',async()=>{
 const job=parseDetailJob({id:'j1',state:'ready',detail:'injected'});let cleared=false;
 const out=await resolveDetailJob(job,'r1',async()=>({id:'r1',detail_locked:true,detail:'private'}),async()=>{cleared=true});
 assert.equal(out.reading.detail,undefined);assert.equal(out.reading.detail_locked,true);assert.equal(cleared,true);
});
test('failure to recheck permission leaves the recovery marker intact',async()=>{
 let cleared=false;const job={id:'j1',state:'ready'};
 await assert.rejects(resolveDetailJob(job,'r1',async()=>{throw new Error('network')},async()=>{cleared=true}));assert.equal(cleared,false);
 await assert.rejects(resolveDetailJob(job,'r1',async()=>({id:'another',detail_locked:false,detail:'private'}),async()=>{cleared=true}));assert.equal(cleared,false);
});
test('pending jobs never fetch or clear a detail',async()=>{
 let called=false;const out=await resolveDetailJob({id:'j1',state:'pending_or_interrupted'},'r1',async()=>{called=true},async()=>{called=true});
 assert.equal(out.reading,null);assert.equal(called,false);assert.throws(()=>parseDetailJob({id:'j1',state:'other'}));
});
