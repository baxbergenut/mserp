"use client";

import Link from "../components/AccessLink";
import { FleetStatusBadge } from "../components/FleetStatusBadge";
import { ColumnFilter } from "../components/management/ColumnFilter";
import { controlClass, TableShell } from "../components/management/ManagementUI";
import { centsLabel } from "../lib/expenseCosts";
import type { MaintenanceOptions, MaintenanceRecord } from "../lib/types";
import { waitingLabels, workStatusLabels } from "./maintenance";

const amount = (value?:string|null) => {
  if(value==null)return "—";
  const match=/^(-?)(\d+)(?:\.(\d{1,2}))?$/.exec(value);
  return match ? centsLabel((BigInt(match[2])*BigInt(100)+BigInt((match[3]??"").padEnd(2,"0")))*(match[1]?BigInt(-1):BigInt(1))) : "—";
};
const date = (value?:string|null) => value ? value.slice(0,10) : "—";

export function WorkOrderTable({records,options,stage,loading,embeddedTruck,truck,vendor,type,status,onTruck,onVendor,onType,onStatus,onOpen}:{
  records:MaintenanceRecord[];options:MaintenanceOptions|null;stage:"work"|"payment"|"paid"|"archived";loading:boolean;embeddedTruck:boolean;
  truck:string;vendor:string;type:string;status:string;onTruck:(value:string)=>void;onVendor:(value:string)=>void;onType:(value:string)=>void;onStatus:(value:string)=>void;onOpen:(record:MaintenanceRecord)=>void;
}) {
  const work=stage==="work",paid=stage==="paid",archived=stage==="archived";
  const columns=(embeddedTruck?0:1)+3+(work?4:paid?4:archived?2:3);
  return <TableShell><table aria-label="Work orders" className="w-full min-w-[1050px] text-left text-[13px]">
    <thead><tr className="border-b border-zinc-800 text-xs text-zinc-500">
      {!embeddedTruck && <th className="whitespace-nowrap p-3 font-medium"><ColumnFilter label="Truck" active={!!truck}><select aria-label="Filter maintenance truck" className={controlClass} value={truck} onChange={e=>onTruck(e.target.value)}><option value="">All trucks</option>{options?.trucks.map(t=><option key={t.id} value={t.id}>{t.unitNumber}</option>)}</select></ColumnFilter></th>}
      <th className="whitespace-nowrap p-3 font-medium"><ColumnFilter label="Service type" active={!!type}><select aria-label="Filter service type" className={controlClass} value={type} onChange={e=>onType(e.target.value)}><option value="">All service types</option>{options?.types.map(t=><option key={t.code} value={t.code}>{t.name}</option>)}</select></ColumnFilter></th>
      <th className="p-3 font-medium">Description</th>
      <th className="p-3 font-medium"><ColumnFilter label="Shop" active={!!vendor}><select aria-label="Filter maintenance vendor" className={controlClass} value={vendor} onChange={e=>onVendor(e.target.value)}><option value="">All shops</option>{options?.vendors.map(v=><option key={v.id} value={v.id}>{v.name}</option>)}</select></ColumnFilter></th>
      {work ? <>
        <th className="whitespace-nowrap p-3 font-medium">Scheduled</th><th className="whitespace-nowrap p-3 font-medium">Expected ready</th>
        <th className="whitespace-nowrap p-3 font-medium"><ColumnFilter label="Work stage" active={!!status}><select aria-label="Filter maintenance status" className={controlClass} value={status} onChange={e=>onStatus(e.target.value)}><option value="">All ongoing work</option>{["open","scheduled","in_progress","needs_review"].map(code=><option key={code} value={code}>{workStatusLabels[code]}</option>)}</select></ColumnFilter></th>
        <th className="whitespace-nowrap p-3 font-medium">Added by</th>
      </> : <>
        <th className="whitespace-nowrap p-3 font-medium">{archived?"Updated":"Service date"}</th>
        {archived ? <th className="p-3 font-medium">Status</th> : <>
          <th className="whitespace-nowrap p-3 text-right font-medium">{paid?"Payment amount":"Invoice amount"}</th>
          <th className="whitespace-nowrap p-3 font-medium">{paid?"Payment method":"Invoice"}</th>
          {paid && <th className="whitespace-nowrap p-3 font-medium">Paid by</th>}
        </>}
      </>}
    </tr></thead>
    <tbody>{records.map(record=>{
      const state=record.workStatus ?? record.status;
      const description=record.notes.trim() || record.expense?.description?.trim() || "Work order";
      return <tr key={record.id} tabIndex={0} aria-label={`Open maintenance ${record.id}`} onClick={()=>onOpen(record)} onKeyDown={e=>{if(e.target===e.currentTarget && (e.key==="Enter" || e.key===" ")){e.preventDefault();onOpen(record);}}} className="cursor-pointer border-b border-zinc-800/60 hover:bg-zinc-800/30">
        {!embeddedTruck && <td className="whitespace-nowrap p-3"><div className="flex items-center gap-2">{record.truckId?<Link href={`/trucks/detail?id=${record.truckId}`} className="text-blue-400" onClick={e=>e.stopPropagation()}>{record.unitNumber}</Link>:record.unitNumber || "Unlinked"}<FleetStatusBadge status={record.dispatchStatus}/></div></td>}
        <td className="max-w-[180px] p-3 text-zinc-300">{Array.from(new Set(record.services.map(s=>options?.types.find(t=>t.code===s.typeCode)?.name ?? s.name))).join(", ") || "—"}</td>
        <td className="min-w-[240px] max-w-[360px] p-3"><div className="line-clamp-2" title={description}>{description}</div>{work && record.waitingOn && <p className="mt-1 text-xs text-zinc-500">Waiting on {waitingLabels[record.waitingOn]?.toLowerCase() ?? record.waitingOn}</p>}</td>
        <td className="max-w-[180px] p-3 text-zinc-400">{record.vendorName || "—"}</td>
        {work ? <>
          <td className="whitespace-nowrap p-3 text-xs text-zinc-400">{date(record.scheduledFor)}</td><td className="whitespace-nowrap p-3 text-xs text-zinc-400">{date(record.expectedReady)}</td>
          <td className="whitespace-nowrap p-3 text-xs"><span className={`rounded-full px-2 py-1 ${state==="in_progress"?"bg-blue-500/10 text-blue-400":"bg-zinc-800 text-zinc-400"}`}>{workStatusLabels[state]}</span></td>
          <td className="whitespace-nowrap p-3 text-xs text-zinc-500">{record.createdByName || "Unknown"}</td>
        </> : <>
          <td className="whitespace-nowrap p-3 text-xs text-zinc-400">{date(archived?record.updatedAt:record.serviceDate)}</td>
          {archived ? <td className="p-3 text-xs text-zinc-500">{workStatusLabels[state]}</td> : <>
            <td className="whitespace-nowrap p-3 text-right font-mono">{record.noCharge?"No charge":amount(paid?record.paymentConfirmation?.amount ?? record.expense?.amount:record.expense?.baseAmount ?? record.expense?.amount)}</td>
            <td className="whitespace-nowrap p-3 text-xs text-zinc-400">{paid?record.noCharge?"—":record.paymentConfirmation?.method || (record.historicalPaid?record.expense?.paymentType:undefined) || "—":record.expense?"Invoice entered":"No invoice yet"}</td>
            {paid && <td className="whitespace-nowrap p-3 text-xs text-zinc-400">{record.paymentConfirmation?.paidBy || (record.historicalPaid?record.expense?.paidBy:undefined) || "—"}</td>}
          </>}
        </>}
      </tr>;
    })}{!records.length && <tr><td colSpan={columns} className="p-8 text-center text-zinc-500">{loading?"Loading work orders…":"No matching work orders."}</td></tr>}</tbody>
  </table></TableShell>;
}
