"use client";

import { useId, useState } from "react";
import { ChevronDown } from "lucide-react";
import type { DriverOption, ExpenseApplicant, ExpenseInput, InvestorOption, TruckOption } from "../lib/types";
import { expenseAssignment } from "./assignments";

export function ExpenseDriverSelect({ value, drivers, applicants = [], trucks, owners, onChange, className, required = false, label = "Driver" }: {
  value: ExpenseInput;
  drivers: DriverOption[];
  applicants?: ExpenseApplicant[];
  trucks: TruckOption[];
  owners: InvestorOption[];
  onChange: (value: ExpenseInput) => void;
  className: string;
  required?: boolean;
  label?: string;
}) {
  const listId = useId();
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const [search, setSearch] = useState("");
  const query = search.trim().toLowerCase();
  const matches = drivers.filter(driver => driver.fullName.toLowerCase().includes(query));
  const choices = [{ id: "", fullName: required ? "Select driver" : "No driver", applicant: false }, ...matches.map(d => ({ ...d, applicant:false })),
    ...(!required ? applicants.filter(a => a.fullName.toLowerCase().includes(query)).map(a => ({ ...a, applicant:true })) : [])];

  function choose(id: string, applicant = false) {
    const person = applicant ? applicants.find(a => a.id === id) : null;
    onChange(person ? { ...value, applicantId:person.id, driverId:null, driverName:person.fullName, truckId:null, unitNumber:"", coveredBy:"Company", ownerId:null } : expenseAssignment(value, "driver", id, drivers, trucks, owners));
    setSearch("");
    setOpen(false);
    setActive(-1);
  }

  return <div className="relative">
    <input
      role="combobox"
      aria-label={label}
      aria-autocomplete="list"
      aria-expanded={open}
      aria-controls={listId}
      aria-activedescendant={open && active >= 0 ? `${listId}-${active}` : undefined}
      autoComplete="off"
      required={required}
      ref={input => { input?.setCustomValidity(required && !value.driverId ? "Select an existing driver for a driver-covered expense." : !value.updatedAt && !!value.driverName && !value.driverId && !value.applicantId ? "Select a driver or applicant from the dropdown, or clear the driver field." : ""); }}
      value={open ? search : value.driverName}
      placeholder={open ? "Search drivers and applicants" : required ? "Select driver" : "Select driver or applicant"}
      className={`${className} pr-8`}
      onFocus={() => { setSearch(""); setOpen(true); setActive(-1); }}
      onClick={() => setOpen(true)}
      onBlur={() => { setSearch(""); setOpen(false); setActive(-1); }}
      onChange={event => {
        setSearch(event.target.value);
        setOpen(true);
        setActive(-1);
      }}
      onKeyDown={event => {
        if (event.key === "ArrowDown" || event.key === "ArrowUp") {
          event.preventDefault();
          const next = event.key === "ArrowDown" ? (active + 1) % choices.length : (active <= 0 ? choices.length : active) - 1;
          setOpen(true);
          setActive(next);
          document.getElementById(`${listId}-${next}`)?.scrollIntoView({ block: "nearest" });
        } else if (event.key === "Enter" && open) {
          event.preventDefault();
          if (active >= 0) choose(choices[active].id, choices[active].applicant);
          else setOpen(false);
        } else if (event.key === "Escape" && open) {
          event.preventDefault();
          event.stopPropagation();
          setOpen(false);
        }
      }}
    />
    <ChevronDown aria-hidden="true" className="pointer-events-none absolute right-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" />
    {open && <div id={listId} role="listbox" aria-label={label} className="absolute z-20 mt-1 max-h-56 w-full overflow-y-auto rounded-lg border border-zinc-800 bg-card py-1 shadow-xl">
      {choices.map((driver, index) => <button
        key={`${driver.applicant ? "applicant" : "driver"}-${driver.id}`}
        id={`${listId}-${index}`}
        type="button"
        role="option"
        aria-selected={driver.id ? driver.id === (driver.applicant ? value.applicantId : value.driverId) : !value.driverId && !value.applicantId && !value.driverName}
        tabIndex={-1}
        onMouseDown={event => event.preventDefault()}
        onMouseEnter={() => setActive(index)}
        onClick={() => choose(driver.id, driver.applicant)}
        className={`block w-full px-3 py-2 text-left text-[13px] text-zinc-200 hover:bg-blue-500/10 ${active === index ? "bg-blue-500/10" : ""}`}
      >{driver.fullName}{driver.applicant && <span className="ml-2 text-xs text-zinc-500">({"status" in driver && driver.status === "hired" ? "Pending setup" : "status" in driver && driver.status === "rejected" ? "Rejected applicant" : "status" in driver && driver.status === "withdrawn" ? "Withdrawn applicant" : "Applicant"}){applicants.filter(a => a.fullName === driver.fullName).length > 1 ? ` · ${driver.id.slice(-6)}` : ""}</span>}</button>)}
    </div>}
  </div>;
}
