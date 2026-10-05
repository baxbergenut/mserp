"use client";

import { useEffect, useState } from "react";
import { fetchDrivers, fetchInvestors, updateTruck, uploadIRPFile } from "@/app/lib/api";
import type { Driver, Investor, Truck } from "@/app/lib/types";
import { Modal, ErrorBanner } from "@/app/components/management/ManagementUI";
import { TruckForm, truckToInput } from "../TruckForm";
import { renderPDFPages } from "@/app/lib/pdf";

export function TruckEditor({ truck, onClose, onSaved }: { truck: Truck; onClose: () => void; onSaved: (truck: Truck) => void }) {
  const [form, setForm] = useState(() => truckToInput(truck));
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [investors, setInvestors] = useState<Investor[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [fileName, setFileName] = useState(truck.irpFileName);
  const [error, setError] = useState("");
  useEffect(() => {
    let cancelled = false;
    Promise.all([fetchDrivers(), fetchInvestors()]).then(([d, i]) => {
      if (!cancelled) { setDrivers(d); setInvestors(i); setLoading(false); }
    }).catch(e => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, []);
  async function upload(file: File) {
    if (file.size > 10 * 1024 * 1024) { setError("Cab-card files must be 10 MB or smaller."); return; }
    setUploading(true); setError("");
    try {
      const pages = file.type === "application/pdf" || file.name.toLowerCase().endsWith(".pdf") ? await renderPDFPages(file) : [];
      const result = await uploadIRPFile(file, pages);
      setFileName(result.file.fileName);
      setForm(current => ({ ...current, irpFileId: result.file.id,
        unitNumber: result.fields.unitNumber || current.unitNumber, vin: result.fields.vin || current.vin,
        year: result.fields.year ?? current.year, make: result.fields.make || current.make,
        model: result.fields.model || current.model, licensePlate: result.fields.licensePlate || current.licensePlate,
        licenseState: result.fields.licenseState || current.licenseState,
        registrationExpires: result.fields.registrationExpires || current.registrationExpires,
      }));
    } catch (e) { setError(e instanceof Error ? e.message : "Unable to upload cab card"); }
    finally { setUploading(false); }
  }
  return <Modal title="Edit truck" description={`Truck ${truck.unitNumber}`} isSaving={saving || uploading || (loading && !error)} submitLabel="Save truck" onClose={onClose} onSubmit={event => {
    event.preventDefault(); if (loading || uploading) return; setSaving(true); setError("");
    void updateTruck(truck.id, form).then(onSaved).catch(e => setError(e.message)).finally(() => setSaving(false));
  }}>{error && <ErrorBanner message={error} />}{loading ? <p className="py-8 text-sm text-zinc-500">Loading assignment options…</p> : <TruckForm value={form} onChange={setForm} drivers={drivers} investors={investors} irpFileName={fileName} isUploadingIRP={uploading} onUploadIRP={upload} onRemoveIRP={() => { setFileName(null); setForm(current => ({ ...current, irpFileId: null })); }} />}</Modal>;
}
