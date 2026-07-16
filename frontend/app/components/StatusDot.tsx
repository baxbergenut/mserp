import { statusKey, statusLabel } from "../lib/utils";

// Ordered keyword -> color rules. First match wins.
const STATUS_COLOR_RULES: Array<[string, string]> = [
  ["cancel", "#E0776D"],
  ["invoice", "#5FA8A0"],
  ["paid", "#5FA8A0"],
  ["deliver", "#7FB37A"],
  ["transit", "#D9A75B"],
  ["progress", "#D9A75B"],
  ["dispatch", "#6C8EEF"],
  ["book", "#8A8A93"],
  ["pending", "#8A8A93"],
];

function colorForStatus(status: string): string {
  const key = statusKey(status);
  const match = STATUS_COLOR_RULES.find(([kw]) => key.includes(kw));
  return match?.[1] ?? "#6B6B70";
}

export function StatusDot({ status }: { status: string }) {
  const color = colorForStatus(status);

  return (
    <span className="inline-flex items-center gap-2">
      <span
        className="h-1.5 w-1.5 shrink-0 rounded-full"
        style={{ backgroundColor: color }}
        aria-hidden
      />
      <span className="text-[13px] text-zinc-300">{statusLabel(status)}</span>
    </span>
  );
}
