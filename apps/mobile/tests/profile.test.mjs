import {test} from 'node:test';
import assert from 'node:assert/strict';
import {dailyRequest,parseProfile,parseProfiles,profileBirthForm} from '../src/profile.ts';
const profile={id:'1234567890abcdef1234567890abcdef',version:2,birth:{year:2000,month:2,day:29,hour:0,minute:0,time_zone:'Asia/Seoul'}};
test('profile restores exact birth and server version',()=>{
 assert.equal(parseProfile(profile).version,2);
 assert.deepEqual(profileBirthForm(profile),{year:'2000',month:'2',day:'29',hour:'0',minute:'0',timeZone:'Asia/Seoul'});
});
test('malformed or unknown time profile is rejected',()=>{
 assert.throws(()=>parseProfile({...profile,version:0}));
 assert.throws(()=>parseProfile({...profile,birth:{...profile.birth,hour:null}}));
});
test('daily request uses separately selected current timezone',()=>{
 assert.deepEqual(dailyRequest(profile.id,'Asia/Seoul','Asia/Seoul'),{profile_id:profile.id,time_zone:'Asia/Seoul'});
 assert.throws(()=>dailyRequest(profile.id,'invalid/zone','Asia/Seoul'));
 assert.throws(()=>dailyRequest(profile.id,'America/Los_Angeles','Asia/Seoul'));
});

test('profile discovery preserves choices and rejects duplicates',()=>{
 assert.deepEqual(parseProfiles([profile]),[profile]);
 assert.deepEqual(parseProfiles([]),[]);
 assert.throws(()=>parseProfiles([profile,profile]));
});
test('daily summary never exposes detail until the same reading is rechecked', async()=>{
 const {resolveDailyReading}=await import('../src/profile.ts');
 const summary={id:'r',summary:'free',detail:'private',detail_locked:false};
 assert.deepEqual(resolveDailyReading(summary,null),{reading:{id:'r',summary:'free',detail:undefined,detail_locked:true},verified:false});
 assert.equal(resolveDailyReading(summary,{...summary,id:'different'}).verified,false);
 assert.equal(resolveDailyReading(summary,{...summary,id:'r'}).verified,true);
});
