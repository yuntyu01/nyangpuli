export type Birth = { year:string; month:string; day:string; hour:string; minute:string; timeZone:string };
export function birthInput(b:Birth) {
  const names = ['year','month','day','hour','minute'] as const;
  if(names.some(k=>!/^\d+$/.test(b[k])) || !b.timeZone.trim()) throw new Error('출생 날짜와 시간을 모두 입력해 주세요.');
  const [year,month,day,hour,minute]=names.map(k=>Number(b[k]));
  const date=new Date(Date.UTC(year,month-1,day));
  if(year<1800||year>2100||month<1||month>12||day<1||date.getUTCMonth()!==month-1||hour>23||minute>59) throw new Error('출생 날짜와 시간을 확인해 주세요.');
  try { new Intl.DateTimeFormat('en',{timeZone:b.timeZone}).format(); } catch { throw new Error('시간대를 확인해 주세요.'); }
  return {year,month,day,hour,minute,time_zone:b.timeZone};
}
export function todayInZone(timeZone:string, now=new Date()) {
  const parts=new Intl.DateTimeFormat('en-CA',{timeZone,year:'numeric',month:'numeric',day:'numeric'}).formatToParts(now);
  const value=(type:string)=>Number(parts.find(p=>p.type===type)?.value);
  return {year:value('year'),month:value('month'),day:value('day')};
}
