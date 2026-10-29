// All three must be attempted even if one secure-storage operation fails.
export async function clearAccountDeviceData(clearToken:()=>Promise<void>,clearTarot:()=>Promise<void>,clearProfile:()=>Promise<void>):Promise<void> {
 const results=await Promise.allSettled([clearToken(),clearTarot(),clearProfile()]);
 if(results.some(result=>result.status==='rejected'))throw new Error('기기의 연결 정보 삭제를 완료하지 못했어요. 기기 설정에서 앱 데이터를 삭제해 주세요.');
}

/** Save the opaque cleanup scope before asking the server to delete. */
export async function startAccountDeletion(scope:string, saveIntent:(scope:string)=>Promise<void>, removeRemote:()=>Promise<void>):Promise<void> {
 if(!/^nyangpuli_pending_[a-f0-9]{64}$/.test(scope)) throw new Error('기기 연결 정보를 확인할 수 없어요.');
 await saveIntent(scope);
 await removeRemote();
}
