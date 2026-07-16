import { statusKey, statusLabel } from "../utils";

// Ordered keyword -> color rules. First match wins, so put more specific
// keywords first. Anything unmatched falls back to neutral gray.
const STATUS_COLOR_RULES: Array<[string, string]> = [
  ["cancel", "#E0776D"], // muted red
  ["invoice", "#5FA8A0"], // teal
  ["paid", "#5FA8A0"],
  ["deliver", "#7FB37A"], // muted green
  ["transit", "#D9A75B"], // muted amber
  ["progress", "#D9A75B"],
  ["dispatch", "#6C8EEF"], // steel blue
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
