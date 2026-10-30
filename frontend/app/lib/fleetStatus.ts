import type { DriverAvailability } from "./types";
export const availabilityLabels: Record<DriverAvailability,string> = {
 active:"Active",home:"Home",vacation:"Vacation",rest:"Rest",reset:"34 Reset",family_issue:"Family issue",health_issue:"Health issue",new_driver:"New driver",
};
export const conditionLabels = {ready:"Ready",shop:"Shop",in_use:"In use",out_of_service:"OOS",terminated:"Terminated"};
export function statusLabel(value:string) {return availabilityLabels[value as DriverAvailability] ?? conditionLabels[value as keyof typeof conditionLabels] ?? value.replaceAll("_"," ").replace(/^./,s=>s.toUpperCase());}
export function newYorkNow() { return new Intl.DateTimeFormat("sv-SE",{timeZone:"America/New_York",year:"numeric",month:"2-digit",day:"2-digit",hour:"2-digit",minute:"2-digit",hourCycle:"h23"}).format(new Date()).replace(" ","T"); }
export function newYorkISO(value:string) {
 const base=Date.parse(value+":00Z");if(!Number.isFinite(base)) throw new Error("Choose a valid effective time");
 let instant=base;
 for(let i=0;i<3;i++) {const shown=new Intl.DateTimeFormat("sv-SE",{timeZone:"America/New_York",year:"numeric",month:"2-digit",day:"2-digit",hour:"2-digit",minute:"2-digit",second:"2-digit",hourCycle:"h23"}).format(new Date(instant)).replace(" ","T");instant+=base-Date.parse(shown+"Z");}
 const result=new Date(instant);const shown=new Intl.DateTimeFormat("sv-SE",{timeZone:"America/New_York",year:"numeric",month:"2-digit",day:"2-digit",hour:"2-digit",minute:"2-digit",hourCycle:"h23"}).format(result).replace(" ","T");
 if(shown!==value) throw new Error("That time does not exist in New York because of daylight saving time");return result.toISOString();
}
