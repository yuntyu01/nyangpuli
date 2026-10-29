import {test} from 'node:test';
import assert from 'node:assert/strict';
import {clearAccountDeviceData} from '../src/accountDeletion.ts';
test('clears all secure device identifiers, even if one removal fails',async()=>{
 const calls=[];
 const clear=(name,fail=false)=>async()=>{calls.push(name);if(fail)throw new Error('storage failure');};
 await assert.rejects(clearAccountDeviceData(clear('token',true),clear('tarot'),clear('profile')),/계정은 삭제됐지만/);
 assert.deepEqual(calls,['token','tarot','profile']);
});
