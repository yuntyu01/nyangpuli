/** An opaque per-server, per-account namespace; tokens never form part of the key. */
export async function pendingScope(server:string, account:unknown, digest:(value:string)=>Promise<string>):Promise<string> {
  if (!account || typeof account!=='object' || typeof (account as Record<string,unknown>).account_id!=='string' || !(account as {account_id:string}).account_id) throw new Error('계정을 확인할 수 없어요.');
  const accountId=(account as {account_id:string}).account_id;
  if(accountId.length>200) throw new Error('계정을 확인할 수 없어요.');
  const hash=await digest(JSON.stringify([server,accountId]));
  if(!/^[a-f0-9]{64}$/i.test(hash)) throw new Error('계정을 확인할 수 없어요.');
  return `nyangpuli_pending_${hash.toLowerCase()}`;
}

/** Replace only the token; a failed write must never remove other accounts' pending keys. */
export async function saveSessionToken(token:string, store:{setItemAsync(key:string,value:string):Promise<void>}) {
  await store.setItemAsync('nyangpuli_token',token);
}
