"use client";
import {useState} from "react";
import {changeDriverAvailability,changeTruckCondition} from "@/app/lib/api";
import {availabilityLabels,conditionLabels,newYorkISO,newYorkNow} from "@/app/lib/fleetStatus";
import { DriverLeaveFields } from "./DriverLeaveFields";
import { leaveToday } from "@/app/lib/driverLeave";
import type {DriverLeavePlan,Driver,Truck} from "@/app/lib/types";
import {Modal,Field,ErrorBanner,controlClass} from "../management/ManagementUI";

export function FleetStatusDialog(props:{kind:"drivers";value:Driver;onSaved:(v:Driver)=>void;onClose:()=>void}|{kind:"trucks";value:Truck;onSaved:(v:Truck)=>void;onClose:()=>void}){
 const [status,setStatus]=useState(props.kind==="drivers"?props.value.availabilityStatus??"active":props.value.operatingStatus??"ready");
 const [effective,setEffective]=useState("");const [reason,setReason]=useState("");const [saving,setSaving]=useState(false);const [error,setError]=useState("");
 const [plan,setPlan]=useState<DriverLeavePlan>(props.kind==="drivers"?props.value.leavePlan??{}:{});
 const labels=props.kind==="drivers"?availabilityLabels:conditionLabels;
 return <Modal title={props.kind==="drivers"?"Change availability":"Change truck condition"} isSaving={saving} submitLabel="Save status" onClose={props.onClose} onSubmit={async event=>{
  event.preventDefault();if(saving)return;setSaving(true);setError("");try{
   const input={status,version:props.value.statusVersion??0,effectiveAt:effective?newYorkISO(effective):undefined,reason,...(props.kind==="drivers" && (status==="home"||status==="vacation") ? {leavePlan:plan,leavePlanVersion:props.value.leavePlanVersion??0} : {})};
   if(props.kind==="drivers")props.onSaved(await changeDriverAvailability(props.value.id,input));else props.onSaved(await changeTruckCondition(props.value.id,input));
  }catch(e){setError(e instanceof Error?e.message:"Unable to change status");}finally{setSaving(false);}
 }}><div className="space-y-4">{error&&<ErrorBanner message={error}/>}
 <Field label={props.kind==="drivers"?"Availability":"Truck condition"}><select aria-label={props.kind==="drivers"?"Availability":"Truck condition"} className={controlClass} value={status} disabled={saving} onChange={e=>{setStatus(e.target.value as typeof status);if(e.target.value==="home"&&(!plan.homeStart||plan.homeStart>leaveToday()||(!!plan.homeEnd&&plan.homeEnd<leaveToday())))setPlan({...plan,homeStart:leaveToday(),homeEnd:""});}}>{Object.entries(labels).map(([code,label])=><option key={code} value={code}>{label}</option>)}</select></Field>
 {props.kind==="drivers"&&(status==="home"||status==="vacation")&&<DriverLeaveFields key={status} plan={plan} onChange={setPlan} vacation={status==="vacation"} disabled={saving}/>}
 <Field label="Effective from (New York)" hint="Leave blank for now. A past time must follow the previous transition."><input className={controlClass} type="datetime-local" value={effective} max={newYorkNow()} onChange={e=>setEffective(e.target.value)}/></Field>
 <Field label="Reason (optional)"><textarea className={controlClass} maxLength={1000} value={reason} onChange={e=>setReason(e.target.value)}/></Field>
 </div></Modal>;
}
