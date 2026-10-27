"use client";
import Link from "next/link";
import type { ComponentProps } from "react";
import { canVisitPage, usePermissions, useAdministrator } from "../lib/access";

export default function AccessLink(props: ComponentProps<typeof Link>) {
  const permissions = usePermissions();
  const administrator = useAdministrator();
  const href = typeof props.href === "string" ? props.href : props.href.pathname;
  if (href?.startsWith("/") && !canVisitPage(permissions, href.split("?")[0], administrator)) {
    return <span className={props.className} title={props.title}>{props.children}</span>;
  }
  return <Link {...props} />;
}
