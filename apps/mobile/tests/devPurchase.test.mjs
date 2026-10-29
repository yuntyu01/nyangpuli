import {test} from 'node:test';
import assert from 'node:assert/strict';
import {parseSimulatedPurchases, simulatedPurchase, testPurchaseEnabled} from '../src/devPurchase.ts';
test('test purchase requires both development mode and explicit flag',()=>{
 assert.equal(testPurchaseEnabled(false,'1'),false);
 assert.equal(testPurchaseEnabled(true,undefined),false);
 assert.equal(testPurchaseEnabled(true,'1'),true);
});
test('simulated request ties a transaction to a specific reading',()=>{
 assert.deepEqual(simulatedPurchase('reading','tx'),{reading_id:'reading',transaction_id:'tx',status:'purchased'});
 assert.throws(()=>simulatedPurchase('','tx'));
});
test('only explicit simulated restore response is accepted',()=>{
 const p={transaction_id:'tx',reading_id:'reading',status:'purchased'};
 assert.deepEqual(parseSimulatedPurchases({kind:'development_simulation_not_store_restore',purchases:[p]}),[p]);
 for(const value of [{kind:'store',purchases:[p]},{kind:'development_simulation_not_store_restore',purchases:[{...p,status:'unknown'}]},{}]) assert.throws(()=>parseSimulatedPurchases(value));
});
