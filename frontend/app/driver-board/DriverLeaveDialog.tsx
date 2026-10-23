"use client";
import { useState } from "react";
import type { DriverLeavePlan } from "@/app/lib/types";
import { leaveToday } from "@/app/lib/driverLeave";
import { Modal } from "../components/management/ManagementUI";
import { DriverLeaveFields } from "../components/fleet/DriverLeaveFields";

export function DriverLeaveDialog({ name, initial, status, changingStatus, onApply, onClose }: {
  name: string; initial?: DriverLeavePlan; status: string; changingStatus: boolean;
  onApply: (plan: DriverLeavePlan) => void; onClose: () => void;
}) {
  const vacation = status === "VACATION";
  const [plan, setPlan] = useState<DriverLeavePlan>({ ...initial, ...(!vacation && changingStatus && (!initial?.homeStart || initial.homeStart > leaveToday() || (!!initial.homeEnd && initial.homeEnd < leaveToday())) ? { homeStart: leaveToday(), homeEnd: "" } : {}) });
  return <Modal isSaving={false} title={`${vacation ? "Vacation return" : "Home time"} · ${name}`} submitLabel="Apply dates" onClose={onClose} onSubmit={e => { e.preventDefault(); onApply(plan); }}>
    <div className="space-y-4"><DriverLeaveFields plan={plan} onChange={setPlan} vacation={vacation} />
      {!changingStatus && <button type="button" className="ui-button" onClick={() => onApply(vacation ? { ...plan, vacationReturn: "" } : { ...plan, homeStart: "", homeEnd: "" })}>Clear dates</button>}
    </div>
  </Modal>;
}
