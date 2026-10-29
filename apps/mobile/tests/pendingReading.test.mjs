import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { pendingScope, saveSessionToken } from '../src/pendingReading.ts';
const hash=async s=>createHash('sha256').update(s).digest('hex');
test('a relogin to the same server account finds its pending key, while other accounts stay separate',async()=>{
  const records=new Map();
  const alice=await pendingScope('https://server.example',{account_id:'alice'},hash);
  records.set(alice,'request-key-a');
  const bob=await pendingScope('https://server.example',{account_id:'bob'},hash);
  const otherServer=await pendingScope('https://other.example',{account_id:'alice'},hash);
  assert.equal(records.get(bob),undefined);assert.equal(records.get(otherServer),undefined);
  const aliceAfterRelogin=await pendingScope('https://server.example',{account_id:'alice'},hash);
  assert.equal(records.get(aliceAfterRelogin),'request-key-a');
  assert.ok(!alice.includes('alice'));
});
test('a failed session replacement does not erase the previous account request',async()=>{
  const records=new Map();const scope=await pendingScope('https://server.example',{account_id:'alice'},hash);
  records.set(scope,'request-key-a');
  records.set('nyangpuli_token','old-token');
  const store={setItemAsync:async(key,value)=>{if(key==='nyangpuli_token')throw new Error('secure storage unavailable');records.set(key,value)}};
  await assert.rejects(saveSessionToken('new-token',store));
  assert.equal(records.get('nyangpuli_token'),'old-token');
  assert.equal(records.get(scope),'request-key-a');
});
test('malformed authenticated identity is rejected',async()=>{
  for(const identity of [null,{}, {account_id:''}]) await assert.rejects(pendingScope('https://server.example',identity,hash));
});
test('detail recovery is isolated by account and never uses the summary slot',async()=>{
 const {detailScope}=await import('../src/pendingReading.ts');
 const first=await pendingScope('https://server.example',{account_id:'alice'},hash);
 const second=await pendingScope('https://server.example',{account_id:'bob'},hash);
 assert.notEqual(detailScope(first),first);assert.notEqual(detailScope(first),detailScope(second));
 assert.throws(()=>detailScope('nyangpuli_token'));
});
test('account deletion attempts both recovery slots and token even after one removal fails',async()=>{
 const {clearAccountRecovery,detailScope}=await import('../src/pendingReading.ts');
 const scope=await pendingScope('https://server.example',{account_id:'alice'},hash);const calls=[];
 await assert.rejects(clearAccountRecovery(scope,async key=>{calls.push(key);if(key===scope)throw new Error('failure')},async()=>{calls.push('token')}));
 assert.deepEqual(calls,[scope,detailScope(scope),'token']);
});
