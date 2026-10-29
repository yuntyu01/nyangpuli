import * as SecureStore from 'expo-secure-store';
import * as Crypto from 'expo-crypto';
import { responseMessage, validateApiBase } from './network';

const tokenKey = 'nyangpuli_token';
const tarotKey = 'nyangpuli_tarot_session';
const base = (process.env.EXPO_PUBLIC_API_URL || '').replace(/\/$/, '');
export type Reading = { id: string; domain: string; status: string; summary?: string; detail?: string; detail_locked: boolean; created_at: string };
export async function saveToken(token: string) { await SecureStore.setItemAsync(tokenKey, token); }
export async function clearToken() { await SecureStore.deleteItemAsync(tokenKey); }
export async function saveTarotSessionId(id: string) { await SecureStore.setItemAsync(tarotKey, id); }
export async function getTarotSessionId() { return await SecureStore.getItemAsync(tarotKey); }
export async function clearTarotSessionId() { await SecureStore.deleteItemAsync(tarotKey); }
export async function hasToken() { return !!(await SecureStore.getItemAsync(tokenKey)); }
export function newRequestKey() { return Crypto.randomUUID(); }
export async function request<T>(path: string, options: { method?: string; body?: unknown; key?: string } = {}): Promise<T> {
  const destination = validateApiBase(base, __DEV__);
  const token = await SecureStore.getItemAsync(tokenKey);
  if (!token) throw new Error('계정 연결이 필요해요. 설정에서 연결해 주세요.');
  let response: Response;
  try {
    response = await fetch(`${destination}${path}`, { method: options.method || 'GET', headers: { Authorization: `Bearer ${token}`, ...(options.body ? { 'Content-Type': 'application/json' } : {}), ...(options.key ? { 'Idempotency-Key': options.key } : {}) }, body: options.body ? JSON.stringify(options.body) : undefined });
  } catch { throw new Error('서버에 연결할 수 없어요. 네트워크와 서버 상태를 확인해 주세요.'); }
  if (!response.ok) {
    throw new Error(responseMessage(response.status));
  }
  try { return await response.json() as T; }
  catch { throw new Error('서버 응답을 읽을 수 없어요. 잠시 뒤 다시 시도해 주세요.'); }
}
