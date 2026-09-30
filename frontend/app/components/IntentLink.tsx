"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import type { ComponentProps } from "react";

// Fetch route code on intent, not payroll records. Financial data stays fresh.
export function IntentLink(props: ComponentProps<typeof Link>) {
  const router = useRouter();
  const warm = () => {
    if (typeof props.href === "string" && props.href.startsWith("/") && !props.href.startsWith("//")) router.prefetch(props.href);
  };
  return <Link {...props} prefetch={false} onPointerEnter={event => { warm(); props.onPointerEnter?.(event); }} onFocus={event => { warm(); props.onFocus?.(event); }} />;
}
