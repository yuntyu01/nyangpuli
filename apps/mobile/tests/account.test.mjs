import {test} from 'node:test';
import assert from 'node:assert/strict';
import {accountApiBase,credentials,devAccountsEnabled,sessionToken} from '../src/account.ts';
test('development account UI requires both development mode and flag',()=>{
 assert.equal(devAccountsEnabled(false,'1'),false);assert.equal(devAccountsEnabled(true,undefined),false);assert.equal(devAccountsEnabled(true,'1'),true);
});
test('normalizes username and validates password byte length',()=>{
 assert.deepEqual(credentials('User.Name','abcdefghijkl'),{username:'user.name',password:'abcdefghijkl'});
 for(const c of [['a','abcdefghijkl'],['bad name','abcdefghijkl'],['valid','짧음'],['valid','12345678901']]) assert.throws(()=>credentials(...c));
 assert.equal(credentials('valid','한글한글한글한글').password,'한글한글한글한글');
});
test('credentials only travel via TLS or explicit development loopback',()=>{
 assert.equal(accountApiBase('http://10.0.2.2:8080',true),'http://10.0.2.2:8080');
 for(const [base,dev] of [['http://example.test',true],['http://localhost',false],['https://x:y@example.test',true]]) assert.throws(()=>accountApiBase(base,dev));
 assert.equal(accountApiBase('https://example.test/',false),'https://example.test');
});
test('missing session response rejected',()=>{assert.throws(()=>sessionToken({}));assert.equal(sessionToken({token:'token'}),'token');});
