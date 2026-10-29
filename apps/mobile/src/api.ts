import * as SecureStore from 'expo-secure-store';
import * as Crypto from 'expo-crypto';
import { responseMessage, validateApiBase, noRedirect } from './network';
import { accountApiBase } from './account';
import { pendingScope, saveSessionToken } from './pendingReading';

const tokenKey = 'nyangpuli_token';
const tarotKey = 'nyangpuli_tarot_session';
const profileKey = 'nyangpuli_profile_id';
const legacyPendingKey = 'nyangpuli_pending_reading_key';
const deletionRecoveryKey = 'nyangpuli_deletion_recovery_scope';
const base = (process.env.EXPO_PUBLIC_API_URL || '').replace(/\/$/, '');
export type Reading = { id: string; domain: string; status: string; summary?: string; detail?: string; detail_locked: boolean; created_at: string };
export async function saveToken(token: string) { await saveSessionToken(token,SecureStore); }
export async function clearToken() { await SecureStore.deleteItemAsync(tokenKey); }
export async function pendingReadingScope() {
  const server=validateApiBase(base,__DEV__);
  const identity=await request<unknown>('/v1/auth/me');
  return pendingScope(server,identity,value=>Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256,value));
}
export async function getLegacyPendingReadingKey() { return await SecureStore.getItemAsync(legacyPendingKey); }
export async function clearLegacyPendingReadingKey() { await SecureStore.deleteItemAsync(legacyPendingKey); }
export async function saveDeletionRecoveryScope(scope:string) { if(!/^nyangpuli_pending_[a-f0-9]{64}$/.test(scope)) throw new Error('기기 연결 정보를 확인할 수 없어요.'); await SecureStore.setItemAsync(deletionRecoveryKey,scope); }
export async function getDeletionRecoveryScope() { const scope=await SecureStore.getItemAsync(deletionRecoveryKey); if(scope && !/^nyangpuli_pending_[a-f0-9]{64}$/.test(scope)) throw new Error('기기 연결 정보를 확인할 수 없어요.'); return scope; }
export async function clearDeletionRecoveryScope() { await SecureStore.deleteItemAsync(deletionRecoveryKey); }
export async function savePendingReadingKey(key:string) { await SecureStore.setItemAsync(await pendingReadingScope(),key); }
export async function getPendingReadingKey() { return await SecureStore.getItemAsync(await pendingReadingScope()); }
export async function clearPendingReadingKey() { await SecureStore.deleteItemAsync(await pendingReadingScope()); }
export async function clearDeletedAccountToken(scope:string) {
  if(!/^nyangpuli_pending_[a-f0-9]{64}$/.test(scope)) throw new Error('기기 연결 정보를 확인할 수 없어요.');
  let failure:unknown;
  try { await SecureStore.deleteItemAsync(scope); } catch(e) { failure=e; }
  try { await clearToken(); } catch(e) { failure=e; }
  if(failure) throw failure;
}
export async function saveTarotSessionId(id: string) { await SecureStore.setItemAsync(tarotKey, id); }
export async function getTarotSessionId() { return await SecureStore.getItemAsync(tarotKey); }
export async function clearTarotSessionId() { await SecureStore.deleteItemAsync(tarotKey); }
export async function saveProfileId(id:string) { await SecureStore.setItemAsync(profileKey,id); }
export async function getProfileId() { return await SecureStore.getItemAsync(profileKey); }
export async function clearProfileId() { await SecureStore.deleteItemAsync(profileKey); }
export async function hasToken() { return !!(await SecureStore.getItemAsync(tokenKey)); }
export function newRequestKey() { return Crypto.randomUUID(); }
export async function request<T>(path: string, options: { method?: string; body?: unknown; key?: string } = {}): Promise<T> {
  const destination = validateApiBase(base, __DEV__);
  const token = await SecureStore.getItemAsync(tokenKey);
  if (!token) throw new Error('계정 연결이 필요해요. 설정에서 연결해 주세요.');
  let response: Response;
  try {
    response = await fetch(`${destination}${path}`, noRedirect({ method: options.method || 'GET', headers: { Authorization: `Bearer ${token}`, ...(options.body ? { 'Content-Type': 'application/json' } : {}), ...(options.key ? { 'Idempotency-Key': options.key } : {}) }, body: options.body ? JSON.stringify(options.body) : undefined }));
  } catch { throw new Error('서버에 연결할 수 없어요. 네트워크와 서버 상태를 확인해 주세요.'); }
  if (!response.ok) {
    throw new Error(responseMessage(response.status));
  }
  try { return await response.json() as T; }
  catch { throw new Error('서버 응답을 읽을 수 없어요. 잠시 뒤 다시 시도해 주세요.'); }
}

// Account credentials are never sent to a remote cleartext destination, even in a development build.
export async function accountRequest<T>(path:string, body?:unknown, authenticated=false):Promise<T> {
  const destination=accountApiBase(base,__DEV__);
  const token=authenticated ? await SecureStore.getItemAsync(tokenKey) : null;
  if(authenticated && !token) throw new Error('먼저 계정을 연결해 주세요.');
  let response:Response;
  try { response=await fetch(`${destination}${path}`,noRedirect({method:'POST',headers:{...(token ? {Authorization:`Bearer ${token}`} : {}),...(body ? {'Content-Type':'application/json'} : {})},body:body ? JSON.stringify(body):undefined})); }
  catch { throw new Error('계정 서버에 연결할 수 없어요.'); }
  if(!response.ok) {
    if(response.status===401) throw new Error(authenticated ? '연결이 만료되었어요.' : '로그인 정보를 확인하거나 잠시 후 다시 시도해 주세요.');
    if(response.status===409) throw new Error('사용할 수 없는 사용자명이거나 이미 연결된 계정이에요.');
    if(response.status===400) throw new Error('사용자명과 비밀번호를 확인해 주세요.');
    throw new Error('계정 요청을 처리하지 못했어요.');
  }
  try { return await response.json() as T; } catch { throw new Error('계정 서버 응답을 읽을 수 없어요.'); }
}
