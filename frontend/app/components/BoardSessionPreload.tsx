"use client";

import { usePathname } from "next/navigation";
import { useLayoutEffect } from "react";
import { trackBoardDocument } from "@/app/lib/boardTiming";
import { preload } from "react-dom";

export function BoardSessionPreload() {
  const path = usePathname();
  useLayoutEffect(() => trackBoardDocument(path), [path]);
  // A direct board visit always needs this session. Emit the hint only in
  // the exported document, so switching boards in the app starts no request.
  if (typeof window === "undefined" && process.env.NEXT_PUBLIC_API_URL === "/api"
      && (path === "/driver-board" || path === "/gross-board")) {
    preload("/api/auth/session", { as: "fetch", crossOrigin: "use-credentials" });
  }
  return null;
}
