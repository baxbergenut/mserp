import type { ExpenseInput, InvestorOption, TruckOption } from "../lib/types";
import { expenseResponsibility } from "./assignments";

export function ExpenseResponsibilitySelect({ value, trucks, owners, onChange, className, label = "Who will cover" }: {
  value: ExpenseInput; trucks: TruckOption[]; owners: InvestorOption[]; onChange: (value: ExpenseInput) => void; className: string; label?: string;
}) {
  const { truck, owner, privatelyOwned, driverOwnsTruck } = expenseResponsibility(value, trucks, owners);
  const displayedOwner = value.coveredBy === "Truck Owner" && value.ownerId ? owners.find(item => item.id === value.ownerId) : owner;
  const choices = ["Company", ...(value.driverId ? ["Driver"] : []), ...(privatelyOwned && !driverOwnsTruck ? ["Truck Owner"] : [])];
  return <select aria-label={label} value={value.coveredBy} onChange={event => onChange({ ...value, coveredBy: event.target.value, ownerId: event.target.value === "Truck Owner" ? truck?.ownerId ?? null : null })} className={className}>
    {value.coveredBy && !choices.includes(value.coveredBy) && <option value={value.coveredBy}>{value.coveredBy} (saved)</option>}
    {choices.map(choice => <option key={choice} value={choice}>{choice === "Truck Owner" ? `Truck Owner${displayedOwner?.fullName ? ` · ${displayedOwner.fullName}` : ""}` : choice}</option>)}
  </select>;
}

export function ExpensePayerSelect({ value, users, onChange, className, label = "Paid by" }: {
  value: string; users: string[]; onChange: (value: string) => void; className: string; label?: string;
}) {
  return <select aria-label={label} value={value} onChange={event => onChange(event.target.value)} className={className}>
    <option value="">Select user</option>
    {value && !users.includes(value) && <option value={value}>{value} (saved)</option>}
    {users.map(name => <option key={name} value={name}>{name}</option>)}
  </select>;
}
