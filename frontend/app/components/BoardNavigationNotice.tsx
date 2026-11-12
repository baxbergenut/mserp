"use client";

import { createPortal } from "react-dom";

export function BoardNavigationNotice({ message, onStay, onRetry, leave }: {
  message: string;
  onStay: () => void;
  onRetry?: () => void;
  leave?: { label: string; onClick: () => void };
}) {
  return createPortal(
    <div className="mserp-ui fixed bottom-4 right-4 z-50 flex max-w-[calc(100%-2rem)] flex-wrap items-center gap-x-4 gap-y-2 rounded-lg border border-amber-500/60 bg-background px-4 py-3 text-sm shadow-lg">
      <span role="status">{message}</span>
      <button type="button" className="underline" onClick={onStay}>Stay here</button>
      {onRetry && <button type="button" className="underline" onClick={onRetry}>Retry and continue</button>}
      {leave && <button type="button" className="underline" onClick={leave.onClick}>{leave.label}</button>}
    </div>,
    document.body,
  );
}
