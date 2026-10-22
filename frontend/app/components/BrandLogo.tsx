import Image from "next/image";

export function BrandLogo({ compact = false, className = "w-36" }: { compact?: boolean; className?: string }) {
  return <span className="relative inline-flex shrink-0 items-center justify-center">
    <Image
      src={compact ? "/brand/clustie-symbol-192.png" : "/brand/clustie-logo.png"}
      alt="Clustie"
      width={compact ? 192 : 640}
      height={compact ? 192 : 153}
      className={compact ? "h-8 w-8" : `h-auto ${className}`}
      unoptimized
      preload
    />
    {!compact && <span aria-hidden="true" className="brand-logo-wordmark pointer-events-none absolute inset-0" />}
  </span>;
}
