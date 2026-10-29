import { test } from 'node:test';
import assert from 'node:assert/strict';
import { validateApiBase, responseMessage, noRedirect } from '../src/network.ts';
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

test('authenticated development requests reject non-loopback HTTP and redirects',()=>{
  assert.throws(()=>validateApiBase('http://remote.example',true));
  assert.throws(()=>validateApiBase('http://192.168.1.12',true));
  assert.equal(validateApiBase('http://10.0.2.2:8080',true),'http://10.0.2.2:8080');
  assert.equal(noRedirect({method:'POST'}).redirect,'error');
});
test('redirect response is rejected rather than sending authenticated data to the next URL in Node',async()=>{
  const {createServer}=await import('node:http');let redirected=0;
  const server=createServer((req,res)=>{if(req.url==='/redirect'){res.writeHead(307,{Location:'/destination'});res.end();}else{redirected++;res.writeHead(200);res.end();}});
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  try { const {port}=server.address();await assert.rejects(fetch(`http://127.0.0.1:${port}/redirect`,noRedirect({method:'POST',headers:{Authorization:'Bearer test'},body:'private'})));assert.equal(redirected,0); }
  finally { await new Promise((resolve,reject)=>server.close(e=>e?reject(e):resolve())); }
});
