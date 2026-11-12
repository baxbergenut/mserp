import Image from "next/image";

export function BetaBadge({ className = "" }: { className?: string }) {
  return <span className={`rounded bg-amber-400 px-1.5 py-0.5 text-[10px] font-bold leading-none text-black ${className}`}>beta</span>;
}

export function BrandLogo({ compact = false, className = "w-24" }: { compact?: boolean; className?: string }) {
  if (!compact) {
    return <span role="img" aria-label="Clustie" className={`brand-logo-wordmark inline-block shrink-0 aspect-[640/153] ${className}`} />;
  }

  return <span className="inline-flex h-8 w-8 shrink-0">
    <Image
      src="/brand/clustie-symbol-192.png"
      alt="Clustie"
      width={192}
      height={192}
      className="h-full w-full object-contain"
      unoptimized
      preload
    />
  </span>;
}
