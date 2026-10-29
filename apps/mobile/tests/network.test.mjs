import { test } from 'node:test';
import assert from 'node:assert/strict';
import { validateApiBase, responseMessage } from '../src/network.ts';
test('production sends tokens only to HTTPS destinations', () => {
  assert.throws(()=>validateApiBase('http://example.test',false));
  assert.equal(validateApiBase('http://localhost:8080/',true),'http://localhost:8080');
  assert.equal(validateApiBase('https://example.test/',false),'https://example.test');
});
test('rejects malformed URLs and embedded credentials', () => {
  for (const value of ['', 'nonsense', 'https://user:pass@example.test', 'https://example.test/?token=x', 'file:///tmp/x']) assert.throws(()=>validateApiBase(value,true));
});
test('server failure messages do not expose a response body', () => {
  assert.match(responseMessage(401),/연결/);
  assert.match(responseMessage(402),/권한/);
  assert.match(responseMessage(503),/현재/);
  assert.equal(responseMessage(500),'요청을 처리하지 못했어요 (500).');
});
