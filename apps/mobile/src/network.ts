/** Validate the API destination before attaching a bearer token or personal data. */
export function validateApiBase(base: string, development: boolean): string {
  if (!base) throw new Error('서버 주소가 설정되지 않았어요.');
  let url: URL;
  try { url = new URL(base); } catch { throw new Error('서버 주소가 올바르지 않아요.'); }
  if (url.username || url.password || url.search || url.hash || !url.hostname) throw new Error('서버 주소가 올바르지 않아요.');
  if (url.protocol !== 'https:' && !(development && url.protocol === 'http:' && ['localhost','127.0.0.1','10.0.2.2'].includes(url.hostname))) throw new Error('안전한 서버 연결이 필요해요.');
  return base.replace(/\/$/, '');
}
/** The runtime must reject redirects before reusing authorization or private payloads. */
export function noRedirect(options:RequestInit):RequestInit { return {...options,redirect:'error'}; }
export function responseMessage(status: number): string {
  if (status === 401) return '계정 연결이 만료되었어요. 설정에서 다시 연결해 주세요.';
  if (status === 402) return '상세 열람 권한이 필요해요.';
  if (status === 409) return '정보가 변경되었거나 충돌했어요. 다시 불러와 확인해 주세요.';
  if (status === 503) return '현재 서비스를 이용할 수 없어요. 잠시 뒤 다시 시도해 주세요.';
  return `요청을 처리하지 못했어요 (${status}).`;
}
