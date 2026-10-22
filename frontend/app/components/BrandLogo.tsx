import Image from "next/image";

export function BrandLogo({ compact = false, className = "w-24" }: { compact?: boolean; className?: string }) {
  return <span className={`relative inline-flex shrink-0 ${compact ? "h-8 w-8" : `aspect-[640/153] ${className}`}`}>
    <Image
      key={compact ? "symbol" : "wordmark"}
      src={compact ? "/brand/clustie-symbol-192.png" : "/brand/clustie-logo.png"}
      alt="Clustie"
      width={compact ? 192 : 640}
      height={compact ? 192 : 153}
      className="h-full w-full object-contain"
      unoptimized
      preload
    />
    {!compact && <span aria-hidden="true" className="brand-logo-wordmark pointer-events-none absolute inset-0" />}
  </span>;
}
