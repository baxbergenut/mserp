"use client";

import { Children, cloneElement, isValidElement, useEffect, useId, useRef, useState, type ReactNode, type ReactElement, type SelectHTMLAttributes } from "react";

function optionCount(children: ReactNode): number {
  return Children.toArray(children).reduce<number>((count, child) => count + (isValidElement<{ children?: ReactNode }>(child) ? child.type === "option" ? 1 : optionCount(child.props.children) : 0), 0);
}

// Expose choices immediately instead of nesting another dropdown inside the menu.
function visibleChoices(children: ReactNode): ReactNode {
  return Children.map(children, child => {
    if (!isValidElement<{ children?: ReactNode }>(child)) return child;
    if (child.type === "select") {
      const select = child as ReactElement<SelectHTMLAttributes<HTMLSelectElement>>;
      const rows = Math.max(2, Math.min(8, optionCount(select.props.children)));
      return cloneElement(select, {
        size: rows,
        style: { ...select.props.style, width: "100%", maxWidth: "100%", height: rows * 28 + 8, borderRadius: 8, padding: 3, boxShadow: "none" },
      });
    }
    if (child.type === "label") {
      const contents = Children.toArray(child.props.children);
      if (contents.some(node => isValidElement(node) && node.type === "select")) {
        const name = contents.filter(node => typeof node === "string").join(" ").trim();
        return cloneElement(child, {}, visibleChoices(contents.filter(node => typeof node !== "string").map(node => {
          if (!isValidElement<SelectHTMLAttributes<HTMLSelectElement>>(node) || node.type !== "select") return node;
          return cloneElement(node, { "aria-label": node.props["aria-label"] || name });
        })));
      }
    }
    return child.props.children ? cloneElement(child, {}, visibleChoices(child.props.children)) : child;
  });
}

/** Native popovers escape scrolling tables and provide outside-click/Escape dismissal. */
export function ColumnFilter({ label, active = false, children }: { label: string; active?: boolean; children: ReactNode }) {
  const id = useId();
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    const close = () => panel.current?.hidePopover();
    window.addEventListener("resize", close);
    return () => window.removeEventListener("resize", close);
  }, []);

  function toggle() {
    const popup = panel.current, button = trigger.current;
    if (!popup || !button) return;
    if (popup.matches(":popover-open")) { popup.hidePopover(); return; }
    const rect = (button.closest("th") ?? button).getBoundingClientRect();
    const width = Math.min(rect.width, window.innerWidth - 16);
    popup.style.width = `${width}px`;
    popup.style.left = `${Math.max(8, Math.min(rect.left, window.innerWidth - width - 8))}px`;
    popup.style.top = `${rect.bottom}px`;
    popup.style.maxHeight = `${Math.max(80, window.innerHeight - rect.bottom - 8)}px`;
    popup.showPopover();
    popup.querySelector<HTMLElement>("select,input,button")?.focus();
  }

  return <>
    <button ref={trigger} type="button" aria-label={`Filter ${label}`} aria-expanded={open} aria-controls={id}
      title={active ? `${label} filters active` : `Filter ${label}`}
      onClick={toggle} style={{ minHeight: 0, padding: 0 }} className={`inline-flex items-center whitespace-nowrap text-inherit hover:text-zinc-200 focus-visible:outline-2 focus-visible:outline-blue-500 ${active ? "!text-blue-400" : ""}`}>
      {label}
    </button>
    <div ref={panel} id={id} popover="auto" role="group" aria-label={`${label} filters`}
      onToggle={event => setOpen(event.newState === "open")}
      className="fixed m-0 space-y-2 overflow-auto rounded-b-lg border-0 bg-zinc-900 p-2 text-left text-[13px] font-normal text-zinc-300 [&_select]:min-w-0 [&_select]:w-full [&_option]:px-2 [&_option]:py-1 [&_input[type=date]]:min-w-0 [&_input[type=date]]:w-full [&_label]:block [&_label]:whitespace-normal">
      {visibleChoices(children)}
    </div>
  </>;
}
