import * as SecureStore from 'expo-secure-store';
import * as Crypto from 'expo-crypto';

const tokenKey = 'nyangpuli_token';
const base = (process.env.EXPO_PUBLIC_API_URL || '').replace(/\/$/, '');
export type Reading = { id: string; domain: string; status: string; summary?: string; detail?: string; detail_locked: boolean; created_at: string };
export type Memory = { id: string; key: string; value: string; observed_at: string };
export async function saveToken(token: string) { await SecureStore.setItemAsync(tokenKey, token); }
export async function clearToken() { await SecureStore.deleteItemAsync(tokenKey); }
export async function hasToken() { return !!(await SecureStore.getItemAsync(tokenKey)); }
export function newRequestKey() { return Crypto.randomUUID(); }
export async function request<T>(path: string, options: { method?: string; body?: unknown; key?: string } = {}): Promise<T> {
  if (!base) throw new Error('서버 주소가 설정되지 않았어요. 앱 설정을 확인해 주세요.');
  const token = await SecureStore.getItemAsync(tokenKey);
  if (!token) throw new Error('계정 연결이 필요해요. 설정에서 연결해 주세요.');
  let response: Response;
  try {
    response = await fetch(`${base}${path}`, { method: options.method || 'GET', headers: { Authorization: `Bearer ${token}`, ...(options.body ? { 'Content-Type': 'application/json' } : {}), ...(options.key ? { 'Idempotency-Key': options.key } : {}) }, body: options.body ? JSON.stringify(options.body) : undefined });
  } catch { throw new Error('서버에 연결할 수 없어요. 네트워크와 서버 상태를 확인해 주세요.'); }
  if (!response.ok) {
    if (response.status === 401) throw new Error('계정 연결이 만료되었어요. 설정에서 다시 연결해 주세요.');
    if (response.status === 402) throw new Error('상세 열람 권한이 필요해요.');
    if (response.status === 503) throw new Error('현재 풀이를 만들 수 없어요. 잠시 뒤 다시 시도해 주세요.');
    throw new Error(`요청을 처리하지 못했어요 (${response.status}).`);
  }
  return await response.json() as T;
}
