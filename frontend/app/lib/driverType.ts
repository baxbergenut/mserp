import type { EmploymentType } from "./types";

export function employmentTypeLabel(type?: EmploymentType) {
  switch (type) {
    case "company": return "Company driver";
    case "owner_operator": return "Owner operator";
    case "investor_driver": return "Investor truck driver";
    default: return "Unassigned";
  }
}
