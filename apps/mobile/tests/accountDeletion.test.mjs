import {test} from 'node:test';
import assert from 'node:assert/strict';
import {clearAccountDeviceData,startAccountDeletion} from '../src/accountDeletion.ts';
test('clears all secure device identifiers, even if one removal fails',async()=>{
 const calls=[];
 const clear=(name,fail=false)=>async()=>{calls.push(name);if(fail)throw new Error('storage failure');};
 await assert.rejects(clearAccountDeviceData(clear('token',true),clear('tarot'),clear('profile')),/기기의 연결 정보 삭제를 완료하지/);
 assert.deepEqual(calls,['token','tarot','profile']);
});

test('a lost deletion response retains the scope for local cleanup without claiming success',async()=>{
 const scope='nyangpuli_pending_'+ 'a'.repeat(64);const device=new Map();let remoteCalls=0;
 await assert.rejects(startAccountDeletion(scope,async s=>device.set('intent',s),async()=>{remoteCalls++;throw new Error('response lost')}));
 assert.equal(device.get('intent'),scope);
 const afterRestart=device.get('intent');
 await clearAccountDeviceData(async()=>device.delete(afterRestart),async()=>device.delete('tarot'),async()=>device.delete('profile'));
 device.delete('intent');
 assert.equal(remoteCalls,1);assert.equal(device.has('intent'),false);
});
test('deletion cannot be sent when its recovery marker cannot be saved',async()=>{
 const scope='nyangpuli_pending_'+ 'b'.repeat(64);let sent=false;
 await assert.rejects(startAccountDeletion(scope,async()=>{throw new Error('storage unavailable')},async()=>{sent=true}));
 assert.equal(sent,false);
});
