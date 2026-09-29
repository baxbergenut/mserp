import type { ChargeType, ChargeEligibility } from "@/app/lib/types";
import { controlClass, Field } from "@/app/components/management/ManagementUI";
import { eligibilityLabel } from "./charges";

export default function ChargeTypeFields({ value, onChange }: { value: ChargeType; onChange: (value: ChargeType) => void }) {
  return <div className="space-y-4">
    <Field label="Charge name"><input required maxLength={200} className={controlClass} value={value.name} onChange={e => onChange({ ...value, name: e.target.value })} /></Field>
    <Field label="Direction"><select aria-label="Direction" className={controlClass} value={value.direction} onChange={e => onChange({ ...value, direction: e.target.value as ChargeType["direction"] })}><option value="charge">Charge (deduct from pay)</option><option value="reimbursement">Reimbursement (add to pay)</option></select></Field>
    <fieldset className="space-y-2"><legend className="mb-2 text-xs text-zinc-400">Available amounts</legend>
      {value.amounts.map((amount, i) => <div key={i} className="flex items-center gap-2"><input aria-label={`Amount option ${i + 1}`} required inputMode="decimal" className={controlClass} value={amount} onChange={e => onChange({ ...value, amounts: value.amounts.map((v, j) => i === j ? e.target.value : v) })} /><span className="w-14 text-xs text-zinc-500">{i === 0 ? "Default" : ""}</span>{value.amounts.length > 1 && <button type="button" aria-label={`Remove amount option ${i + 1}`} className="text-xs text-zinc-400 hover:text-red-400" onClick={() => onChange({ ...value, amounts: value.amounts.filter((_, j) => i !== j) })}>Remove</button>}</div>)}
      <button type="button" disabled={value.amounts.length >= 50} className="text-xs text-blue-400" onClick={() => onChange({ ...value, amounts: [...value.amounts, ""] })}>+ Add amount option</button>
      <p className="text-xs text-zinc-500">Select an amount for each driver in Recurring assignments. Changing these options keeps existing driver amounts.</p>
    </fieldset>
    <Field label="Charge eligibility"><select aria-label="Charge eligibility" className={controlClass} value={value.eligibility} onChange={e => onChange({ ...value, eligibility: e.target.value as ChargeEligibility })}>{(["calendar", "loads", "no_loads"] as const).map(v => <option key={v} value={v}>{eligibilityLabel(v)}</option>)}</select></Field>
    <p className="text-xs text-zinc-500">Eligibility applies to every driver assigned this type from the current payroll week. Previous weeks and saved weekly overrides remain unchanged. Gross Board plans count as loads; status-only days do not.</p>
    <label className="flex gap-2 text-sm text-zinc-400"><input type="checkbox" checked={value.archived} onChange={e => onChange({ ...value, archived: e.target.checked })} />Archived — existing assignments continue</label>
  </div>;
}
