"use client";

import { FileUp, X } from "lucide-react";

export function ReceiptUpload({ file, disabled, onChange, onError }: {
  file: File | null; disabled: boolean; onChange: (file: File | null) => void; onError: (error: string) => void;
}) {
  return <section className="mt-6 space-y-2">
    <h3 className="text-[13px] font-semibold text-zinc-200">Receipt / invoice</h3>
    <div className="flex flex-wrap items-center gap-2">
      <label className={`ui-button inline-flex items-center gap-2 ${disabled ? "pointer-events-none opacity-50" : "cursor-pointer"}`}>
        <FileUp className="h-4 w-4" />Upload receipt / invoice
        <input aria-label="Upload receipt / invoice" type="file" disabled={disabled} className="sr-only"
          accept=".pdf,.txt,.png,.jpg,.jpeg,.webp" onChange={event => {
            const next = event.target.files?.[0]; event.target.value = "";
            if (!next) return;
            if (!next.size || next.size > 20 * 1024 * 1024 || !/\.(pdf|txt|png|jpe?g|webp)$/i.test(next.name)) {
              onError("Choose a nonempty PDF, TXT, PNG, JPEG or WEBP up to 20 MB."); return;
            }
            onError(""); onChange(next);
          }} />
      </label>
      {file && <div className="flex min-w-0 items-center gap-2 text-xs text-zinc-400"><span className="break-all">{file.name}</span>
        <button type="button" disabled={disabled} aria-label="Remove receipt / invoice" className="rounded p-1 hover:text-zinc-200 disabled:opacity-50" onClick={() => onChange(null)}><X className="h-4 w-4" /></button>
      </div>}
    </div>
    <p className="text-xs text-zinc-500">Saved with this entry. PDF, TXT or image · up to 20 MB.</p>
  </section>;
}
