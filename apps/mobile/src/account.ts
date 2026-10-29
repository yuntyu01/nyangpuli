export function devAccountsEnabled(development:boolean, flag:string|undefined) { return development && flag==='1'; }
export function credentials(username:string,password:string) {
  const normalized=username.trim().toLowerCase();
  let size=0;
  try { size=encodeURIComponent(password).replace(/%[0-9A-F]{2}/gi,'x').length; } catch { throw new Error('비밀번호를 확인해 주세요.'); }
  if(!/^[a-z0-9][a-z0-9._-]{2,63}$/.test(normalized)) throw new Error('사용자명은 영문·숫자로 시작하는 3~64자의 영문, 숫자, 점, 밑줄, 하이픈을 사용해 주세요.');
  if(size<12 || size>128) throw new Error('비밀번호는 UTF-8 기준 12~128바이트로 입력해 주세요.');
  return {username:normalized,password};
}
export function accountApiBase(base:string, development:boolean) {
  let url:URL;
  try { url=new URL(base); } catch { throw new Error('서버 주소를 확인해 주세요.'); }
  if(url.username || url.password || url.search || url.hash || !url.hostname) throw new Error('서버 주소를 확인해 주세요.');
  if(url.protocol!=='https:' && !(development && url.protocol==='http:' && ['localhost','127.0.0.1','10.0.2.2'].includes(url.hostname))) throw new Error('계정 연결에는 HTTPS가 필요해요.');
  return base.replace(/\/$/,'');
}
export function sessionToken(value:unknown) {
  if(!value || typeof value!=='object' || typeof (value as Record<string,unknown>).token!=='string' || !(value as {token:string}).token) throw new Error('계정 연결을 완료하지 못했어요.');
  return (value as {token:string}).token;
}
