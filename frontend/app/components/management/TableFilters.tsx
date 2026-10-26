import type { ReactNode } from "react";

/** Visible native controls share the search toolbar's wrapping layout. */
export function TableFilters({ children }: { children: ReactNode }) {
  return <div role="group" aria-label="Table filters" className="contents text-[12px] text-zinc-400 [&_select]:!w-auto [&_select]:max-w-full [&_select]:sm:max-w-60 [&_label]:inline-flex [&_label]:min-w-0 [&_label]:max-w-full [&_label]:flex-wrap [&_label]:items-center [&_label]:gap-2 [&_label]:space-y-0 [&_input[type=date]]:min-w-0 [&_input[type=date]]:w-auto [&_input[type=date]]:max-w-full">
    {children}
  </div>;
}
