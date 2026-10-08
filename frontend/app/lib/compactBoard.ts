import type { BoardLoad, BoardLoads, CompactDriverBoard, DriverBoard } from "./types";

export function expandBoard(value: DriverBoard | CompactDriverBoard): DriverBoard {
  if (!("plans" in value)) return value; // Compatible with the prior API during rollout.
  const plan = (id: number): BoardLoad => {
    const p = value.plans[id];
    if (!p) throw new Error("Incomplete status board response. Reload the board.");
    return structuredClone(p);
  };
  const loads: Record<string, BoardLoads> = {};
  for (const [id, v] of Object.entries(value.loads)) {
    loads[id] = { ...v, current: v.current === null ? null : plan(v.current), week: v.week.map(plan), next: v.next.map(plan), earlier: v.earlier.map(plan), hidden: v.hidden.map(plan), unavailable: v.unavailable.map(plan) };
  }
  const { plans: _, ...board } = value;
  void _;
  return { ...board, loads };
}
