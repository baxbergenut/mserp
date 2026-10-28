"use client";

import { useCallback, useEffect, useState, type ReactNode } from "react";
import Link from "@/app/components/AccessLink";
import { Package, Plus, RefreshCw, Truck } from "lucide-react";
import { usePermissions } from "@/app/lib/access";
import { fetchInventory, fetchPrePassFleet, fetchTrailers, fetchTruckOptions } from "@/app/lib/api";
import type { InventoryCollection, InventoryData, InventoryFields, InventoryRecord, PrePassFleetList, TruckOption } from "@/app/lib/types";
import { ErrorBanner, Field, ManagementHeader, ManagementSearch, Modal, TablePagination, TableShell, controlClass } from "@/app/components/management/ManagementUI";
import { PrePassFleetTab } from "@/app/tolls/PrePassFleetTab";
import { InventoryEditor, inventoryLabel, inventoryPhone, newInventoryRecord, readable, type InventoryContext } from "./InventoryForms";
import { effectiveTruck, installationState, StatusBadge, visibleDevice } from "./InventoryStatus";
import { SuppliesMatrix } from "./SuppliesMatrix";
import { InventoryImports } from "./InventoryImports";

const tabs = [
  ["tablets", "Tablets"], ["phones", "Phones"], ["dashcams", "Dashcams"], ["samsara-gateways", "Samsara Gateways"],
  ["supplies", "Supplies"], ["lines", "AT&T Lines"], ["transponders", "Transponders"], ["equipment", "Truck Equipment"], ["locations", "Locations"],
] as const;
type Tab = typeof tabs[number][0] | "imports";
export const emptyInventory: InventoryData = { assets: [], lines: [], supplies: [], locations: [], equipment: [], "transponder-notes": [], balances: [], movements: [] };
const button = "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-lg border border-zinc-800 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-50";
const primary = "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-lg bg-blue-600 px-3.5 py-2 text-[13px] font-medium text-white hover:bg-blue-500 disabled:opacity-50";
const filterClass = controlClass.replace("w-full", "w-auto max-w-52 shrink-0");
const th = "whitespace-nowrap px-4 py-3 text-left text-xs font-medium text-zinc-500", td = "px-4 py-3 text-xs text-zinc-300";

function startingTab(selected: string | null): Tab {
  if (selected === "samsara") return "samsara-gateways";
  if (selected === "samsara-cameras" || selected === "devices") return "dashcams";
  if (selected === "history") return "equipment";
  return selected === "imports" || tabs.some(([key]) => key === selected) ? selected as Tab : "tablets";
}

export function InventoryWorkspace({ trailers = false, truckId = "" }: { trailers?: boolean; truckId?: string }) {
  const permissions = usePermissions();
  const canManage = permissions.includes(trailers ? "trailers.manage" : "inventory.manage");
  const canTrucks = permissions.includes("trucks.read"), canTolls = permissions.includes("tolls.read");
  const [data, setData] = useState<InventoryData>(emptyInventory), [trailerRows, setTrailerRows] = useState<InventoryRecord[]>([]), [trucks, setTrucks] = useState<TruckOption[]>([]);
  const [tab, setTab] = useState<Tab>(truckId ? "equipment" : "tablets"), [search, setSearch] = useState(""), [status, setStatus] = useState("record:active"), [assigned, setAssigned] = useState(""), [location, setLocation] = useState(""), [brand, setBrand] = useState("");
  const [page, setPage] = useState(1), [pageSize, setPageSize] = useState(25), [loading, setLoading] = useState(true), [error, setError] = useState("");
  const [editor, setEditor] = useState<{ collection: InventoryCollection; record: InventoryRecord } | null>(null), [detail, setDetail] = useState<{ collection: InventoryCollection; record: InventoryRecord } | null>(null);
  const [assigning, setAssigning] = useState(false), [chosenAsset, setChosenAsset] = useState(""), [chosenTruck, setChosenTruck] = useState(truckId);
  const context: InventoryContext = { data, trucks };
  const load = useCallback(async (signal?: AbortSignal) => {
    if (!trailers && (tab === "transponders" || tab === "imports")) return;
    setLoading(true);
    try {
      if (trailers) { const records = await fetchTrailers(signal); if (!signal?.aborted) setTrailerRows(records); }
      else { const result = await fetchInventory(signal,tab === "supplies" ? "supplies" : tab === "locations" ? "locations" : "devices"); if (!signal?.aborted) setData({ ...emptyInventory, ...result }); }
      if (!signal?.aborted) setError("");
    } catch (reason) { if (!signal?.aborted) setError(reason instanceof Error ? reason.message : "Unable to load inventory"); }
    finally { if (!signal?.aborted) setLoading(false); }
  }, [trailers,tab]);
  useEffect(() => {
    const timer=setTimeout(() => {if (!trailers && !truckId) {
      const selected=new URLSearchParams(window.location.search).get("tab");setTab(startingTab(selected));if(selected==="samsara-cameras")setBrand("Samsara");
    }},0);return () => clearTimeout(timer);
  },[trailers,truckId]);
  useEffect(() => {const controller=new AbortController();const timer=setTimeout(() => void load(controller.signal),0);return () => {clearTimeout(timer);controller.abort();};},[load]);
  // Truck labels and editor choices load independently from the inventory rows.
  useEffect(() => {
    if (!canTrucks || trailers) return;
    let active = true;
    void fetchTruckOptions().then(choices => {if(active)setTrucks(choices);}).catch(e => {if(active)setError(e.message);});
    return () => {active=false;};
  },[canTrucks,trailers]);
  const changeTab = (next: Tab) => {
    setTab(next); setPage(1); setSearch(""); setAssigned(""); setLocation(""); setBrand(""); setStatus("record:active");
    if (!truckId) window.history.replaceState(null, "", window.location.pathname + "?tab=" + next);
  };
  const saved = () => { setEditor(null); setDetail(null); void load(); };
  const collection: InventoryCollection = trailers ? "trailers" : tab === "lines" ? "lines" : tab === "supplies" ? "supplies" : tab === "locations" ? "locations" : "assets";
  const deviceType = tab === "phones" ? "phone" : tab === "dashcams" ? "dashcam" : tab === "samsara-gateways" ? "samsara_gateway" : "tablet";
  let records = trailers ? trailerRows : data[collection as Exclude<InventoryCollection, "trailers">];
  if (collection === "assets") records = records.filter(record => visibleDevice(record.data) && (tab === "equipment" ? !!effectiveTruck(record, data) : record.data.deviceType === deviceType));
  if (truckId) records = records.filter(record => effectiveTruck(record, data) === truckId);
  const filtered = records.filter(record => {
    const d = record.data, truck = trucks.find(item => item.id === effectiveTruck(record, data));
    const line = data.lines.find(item => item.data.assetId === record.id && item.data.active);
    const linkedAsset = data.assets.find(item => item.id === d.assetId), gateway = data.assets.find(item => item.id === d.gatewayId);
    const camera = data.assets.find(item => item.data.active && item.data.gatewayId === record.id);
    const haystack = JSON.stringify([d, truck?.unitNumber, inventoryPhone(line?.data.wirelessNumber), line?.data.wirelessNumber, inventoryPhone(d.wirelessNumber), linkedAsset?.data.imei, gateway?.data.serial, camera?.data.serial]).toLowerCase();
    const hasHolder = !!(effectiveTruck(record, data) || d.custodian || d.locationId);
    const [kind, code] = status.split(":");
    const matchesStatus = status === "all" || kind === "record" && (code === "active" ? d.active : !d.active) || kind === "condition" && (d.condition || "unknown") === code || kind === "presence" && installationState(record, data) === code || kind === "status" && (d.status || "unknown") === code;
    return (!search || haystack.includes(search.toLowerCase())) && matchesStatus && (!brand || d.brand === brand) &&
      (!location || d.locationId === location || d.storageLocation === location) && (!assigned || assigned === "assigned" && hasHolder || assigned === "unassigned" && !hasHolder);
  });
  const custody = (record: InventoryRecord) => {
    const linkedTruck = effectiveTruck(record, data), truck = trucks.find(item => item.id === linkedTruck);
    if (linkedTruck) return canTrucks ? <Link href={"/trucks/detail?id=" + linkedTruck} className="text-blue-400">{truck?.unitNumber || "Truck"}</Link> : "Assigned Truck";
    return record.data.custodian || (record.data.locationId ? inventoryLabel(data.locations.find(item => item.id === record.data.locationId)) : record.data.gatewayId ? "Gateway Unassigned" : "Unassigned");
  };
  const add = (selected: InventoryCollection = collection) => {
    const defaults: Partial<InventoryFields> = selected === "assets" ? { deviceType, ...(deviceType === "dashcam" ? { brand: brand || "Samsara" } : {}), condition: "unknown", status: "unknown", equipmentState: "unknown", ...(truckId ? { truckId } : {}) } :
      selected === "trailers" ? { status: "stored_unused", condition: "unknown" } : selected === "supplies" ? { unit: "units" } : selected === "lines" ? { status: "unknown" } : {};
    setEditor({ collection: selected, record: newInventoryRecord(defaults) });
  };
  const title = trailers ? "Trailers" : truckId ? "Truck Equipment" : "Inventory";
  const addLabel = trailers ? "Add Trailer" : collection === "assets" ? tab === "equipment" || truckId ? "Assign Device" : "Add " + readable(deviceType === "samsara_gateway" ? "gateway" : deviceType) : tab === "lines" ? "Add Line" : "Add Location";
  const headers = trailers ? ["Unit", "VIN", "Year / Make", "Plate", "Storage", "Availability", "Condition", ""] :
    collection === "lines" ? ["Wireless Number", "Device IMEI", "User", "Carrier Status", "Plan", "Contract End", ""] :
      collection === "locations" ? ["Location", "Notes", ""] :
        tab === "samsara-gateways" ? ["Gateway Serial", "Linked Dashcam", "Truck / Custody", "Condition", "Installation", ""] :
          tab === "dashcams" ? ["Brand", "IMEI / Serial", "Model", "Truck / Custody", "Linked Gateway", "Condition", "Installation", ""] :
            tab === "equipment" ? ["Device", "IMEI / Serial", "Phone Number", "Truck / Custody", "Condition", "Installation", ""] :
              ["Make / Model", "IMEI", tab === "phones" ? "Phone Number" : "Tablet Number", "Truck / Custody", "Condition", "Installation", ""];
  const columns = (record: InventoryRecord): ReactNode[] => {
    const d = record.data, line = data.lines.find(item => item.data.active && item.data.assetId === record.id);
    const condition = <StatusBadge value={d.condition} />, installation = <StatusBadge value={installationState(record, data)} />;
    if (trailers) return [d.unitNumber, d.vin, [d.year, d.make].filter(Boolean).join(" "), [d.plate, d.plateState].filter(Boolean).join(" "), d.storageLocation, <StatusBadge key="availability" value={d.status} />, condition];
    if (collection === "lines") return [inventoryPhone(d.wirelessNumber), inventoryLabel(data.assets.find(item => item.id === d.assetId)), d.custodian, <StatusBadge key="carrier" value={d.status} />, d.plan, d.contractEnd];
    if (collection === "locations") return [d.name, d.notes];
    if (tab === "samsara-gateways") return [d.serial, inventoryLabel(data.assets.find(item => item.data.active && item.data.deviceType === "dashcam" && item.data.gatewayId === record.id)), custody(record), condition, installation];
    if (tab === "dashcams") return [d.brand, d.imei || d.serial, d.model, custody(record), d.gatewayId ? inventoryLabel(data.assets.find(item => item.id === d.gatewayId)) : "—", condition, installation];
    if (tab === "equipment") return [[readable(d.deviceType), d.brand || d.make, d.model].filter(Boolean).join(" · "), d.imei || d.serial, inventoryPhone(line?.data.wirelessNumber), custody(record), condition, installation];
    return [[d.make, d.model].filter(Boolean).join(" · ") || inventoryLabel(record), d.imei, line ? inventoryPhone(line.data.wirelessNumber) : "No Service Line", custody(record), condition, installation];
  };
  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize)), currentPage = Math.min(page, totalPages);
  const assignable = data.assets.filter(asset => asset.data.active && visibleDevice(asset.data) && !effectiveTruck(asset, data) && !asset.data.gatewayId && !["lost", "retired"].includes(asset.data.condition || "") && (tab === "equipment" || asset.data.deviceType === deviceType));
  return <div className="space-y-5 animate-fade-in">
    <ManagementHeader icon={trailers ? Truck : Package} title={title} count={trailers ? trailerRows.length : undefined} />
    {error && <ErrorBanner message={error} />}
    {truckId && <TruckInventorySummary truckId={truckId} context={context} />}
    {trailers ? <p className="text-xs text-zinc-500">Stored company trailers. Condition requires review. Operational Schneider/JB Hunt trailers remain on Status Board.</p> :
      <nav aria-label="Inventory Sections" className="flex flex-wrap gap-1 border-b border-zinc-800">{tabs.filter(([key]) => (key !== "transponders" || canTolls && !truckId) && (!truckId || !["locations", "supplies"].includes(key))).map(([key, label]) =>
        <button key={key} role="tab" aria-selected={tab === key} className={"border-b-2 px-3 py-2.5 text-xs " + (tab === key ? "border-blue-500 text-blue-400" : "border-transparent text-zinc-500 hover:text-zinc-300")} onClick={() => changeTab(key)}>{label}</button>
      )}</nav>}
    {tab === "imports" && !trailers ? <InventoryImports context={context} onCommitted={saved} /> :
      tab === "transponders" && !trailers ? canTolls ? <PrePassFleetTab view="transponders" /> : <p className="text-sm text-zinc-500">Tolls access is required to view PrePass transponders.</p> :
        tab === "supplies" && !trailers ? <SuppliesMatrix context={context} canManage={canManage} canTrucks={canTrucks} loading={loading} onRefresh={() => void load()} onAdd={() => add("supplies")} onEdit={record => setEditor({ collection: "supplies", record })} onError={setError} onSaved={record => setData(previous => ({ ...previous, equipment: [...previous.equipment.filter(item => item.id !== record.id), record] }))} /> : <>
          <div role="group" aria-label="Inventory controls" className="flex flex-wrap items-center gap-2">
            <div className="min-w-40 flex-1 basis-full sm:basis-48 [&>div]:max-w-none"><ManagementSearch value={search} onChange={value => { setSearch(value); setPage(1); }} placeholder={trailers ? "Search unit, VIN, plate or storage…" : "Search identifier, number, truck or notes…"} /></div>
            <select aria-label="Status Filter" className={filterClass} value={status} onChange={event => { setStatus(event.target.value); setPage(1); }}>
              <option value="record:active">Active Records</option><option value="all">All Records</option><option value="record:archived">Archived Records</option>
              {collection === "assets" && <><optgroup label="Condition">{["good", "damaged", "repair", "lost", "retired", "unknown"].map(value => <option key={value} value={"condition:" + value}>{readable(value)}</option>)}</optgroup><optgroup label="Installation">{["reported_present", "verified_installed", "missing", "unknown", "unassigned"].map(value => <option key={value} value={"presence:" + value}>{readable(value)}</option>)}</optgroup></>}
              {collection === "lines" && <optgroup label="Carrier Status">{["active", "suspended", "cancelled", "pending", "unknown"].map(value => <option key={value} value={"status:" + value}>{readable(value)}</option>)}</optgroup>}
            </select>
            {tab === "dashcams" && !trailers && <select aria-label="Brand Filter" className={filterClass} value={brand} onChange={event => { setBrand(event.target.value); setPage(1); }}><option value="">All Brands</option><option value="Samsara">Samsara</option><option value="FiveELD">FiveELD</option></select>}
            {!trailers && collection === "assets" && tab !== "equipment" && <select aria-label="Assignment Filter" className={filterClass} value={assigned} onChange={event => { setAssigned(event.target.value); setPage(1); }}><option value="">All Assignments</option><option value="assigned">Assigned</option><option value="unassigned">Unassigned</option></select>}
            {trailers ? <input aria-label="Storage Location Filter" placeholder="Storage Location" className={filterClass} value={location} onChange={event => { setLocation(event.target.value); setPage(1); }} /> :
              collection === "assets" && <select aria-label="Location Filter" className={filterClass} value={location} onChange={event => { setLocation(event.target.value); setPage(1); }}><option value="">All Locations</option>{data.locations.map(item => <option key={item.id} value={item.id}>{inventoryLabel(item)}</option>)}</select>}
            <button className={button} disabled={loading} onClick={() => void load()}><RefreshCw className="h-4 w-4" />Refresh</button>
            {canManage && (!(tab === "equipment" || truckId) || canTrucks) && <button className={primary} onClick={() => { if (!trailers && (tab === "equipment" || truckId)) { setChosenAsset(""); setChosenTruck(truckId); setAssigning(true); } else add(); }}><Plus className="h-4 w-4" />{addLabel}</button>}
          </div>
          {loading && <p role="status" className="text-sm text-zinc-500">Loading…</p>}
          <TableShell><table className="w-full"><thead><tr>{headers.map((label, index) => <th key={index} className={th}>{label}</th>)}</tr></thead><tbody>
            {filtered.slice((currentPage - 1) * pageSize, currentPage * pageSize).map(record => <tr key={record.id} className="border-t border-zinc-800/60 hover:bg-zinc-800/20">
              {columns(record).map((value, index) => <td key={index} className={td}>{index === 0 ? <button className="text-left font-medium text-blue-400 hover:underline" onClick={() => setDetail({ collection, record })}>{value || inventoryLabel(record)}{!record.data.active && <span className="ml-2 text-zinc-500">Archived</span>}</button> : value || "—"}</td>)}
              <td className={td}>{canManage && <button className="text-blue-400" onClick={() => setEditor({ collection, record })}>Edit</button>}</td>
            </tr>)}
            {!loading && !filtered.length && <tr><td colSpan={headers.length} className="p-10 text-center text-sm text-zinc-500">{search || status !== "record:active" || assigned || location || brand ? "No matching records." : "No records yet."}</td></tr>}
          </tbody></table></TableShell>
          <TablePagination page={currentPage} pageSize={pageSize} totalItems={filtered.length} totalPages={totalPages} onPageChange={setPage} onPageSizeChange={value => { setPageSize(value); setPage(1); }} />
        </>}
    {editor && <InventoryEditor key={editor.collection + ":" + editor.record.id} collection={editor.collection} record={editor.record} context={context} onClose={() => setEditor(null)} onSaved={saved} />}
    {assigning && <Modal title="Assign Existing Device" isSaving={false} submitLabel="Continue" submitDisabled={!chosenAsset || !chosenTruck} onClose={() => setAssigning(false)} onSubmit={event => {
      event.preventDefault(); const asset = data.assets.find(item => item.id === chosenAsset); if (!asset || !chosenTruck) return;
      setEditor({ collection: "assets", record: { ...asset, data: { ...asset.data, truckId: chosenTruck, locationId: "", custodian: "", gatewayId: "", equipmentState: "unknown" } } }); setAssigning(false);
    }}>
      <div className="grid gap-4 sm:grid-cols-2"><Field label="Device"><select aria-label="Device" className={controlClass} value={chosenAsset} onChange={event => setChosenAsset(event.target.value)}><option value="">Select a device…</option>{assignable.map(asset => <option key={asset.id} value={asset.id}>{[readable(asset.data.deviceType), asset.data.brand || asset.data.make, inventoryLabel(asset)].filter(Boolean).join(" · ")}</option>)}</select></Field>
      <Field label="Truck"><select aria-label="Truck" className={controlClass} value={chosenTruck} onChange={event => setChosenTruck(event.target.value)}><option value="">Select a truck…</option>{trucks.map(truck => <option key={truck.id} value={truck.id}>{truck.unitNumber}</option>)}</select></Field></div>
    </Modal>}
    {detail && <Modal title={inventoryLabel(detail.record)} isSaving={false} hideSubmit submitLabel="Close" cancelLabel="Close" onSubmit={event => event.preventDefault()} onClose={() => setDetail(null)}>
      <dl className="grid gap-3 text-xs sm:grid-cols-2">{Object.entries(detail.record.data).filter(([key, value]) => value !== "" && !(key === "assetLabel" && detail.record.data.deviceType === "samsara_gateway")).map(([key, value]) => <div key={key}>
        <dt className="capitalize text-zinc-500">{key.replace(/Id$/, "").replace(/([A-Z])/g, " $1")}</dt>
        <dd className="mt-1 break-words text-zinc-200">{key === "wirelessNumber" ? inventoryPhone(String(value)) : ["condition", "status"].includes(key) ? <StatusBadge value={String(value)} /> : key === "equipmentState" ? <StatusBadge value={detail.collection === "assets" ? installationState(detail.record, data) : String(value)} /> :
          key === "truckId" ? custody(detail.record) : key === "gatewayId" || key === "assetId" ? inventoryLabel(data.assets.find(asset => asset.id === value)) :
            key === "locationId" ? inventoryLabel(data.locations.find(item => item.id === value)) : key === "supplyId" ? inventoryLabel(data.supplies.find(item => item.id === value)) :
              key === "registrationUrl" ? <a href={String(value)} className="text-blue-400" target="_blank" rel="noreferrer">Open Registration</a> : typeof value === "boolean" ? value ? "Yes" : "No" : String(value)}</dd>
      </div>)}</dl>
    </Modal>}
  </div>;
}

function TruckInventorySummary({ truckId, context }: { truckId: string; context: InventoryContext }) {
  const canTolls = usePermissions().includes("tolls.read");
  const [prepass, setPrepass] = useState<PrePassFleetList | null>(null), [error, setError] = useState("");
  useEffect(() => {
    if (!canTolls) return; const controller = new AbortController();
    void fetchPrePassFleet(controller.signal).then(value => { if (!controller.signal.aborted) setPrepass(value); }).catch(reason => { if (!controller.signal.aborted) setError(reason.message); });
    return () => controller.abort();
  }, [canTolls]);
  const assets = context.data.assets.filter(asset => asset.data.active && visibleDevice(asset.data) && effectiveTruck(asset, context.data) === truckId);
  const gateway = assets.find(asset => asset.data.deviceType === "samsara_gateway"), tablet = assets.find(asset => asset.data.deviceType === "tablet");
  const line = context.data.lines.find(item => item.data.active && item.data.assetId === tablet?.id);
  const cameras = assets.filter(asset => asset.data.deviceType === "dashcam");
  const tag = prepass?.vehicles.find(item => item.truckId === truckId)?.vehicle.transponderNumber;
  const items = [["Samsara Gateway", gateway?.data.serial], ["Dashcams", cameras.map(camera => [camera.data.brand, camera.data.serial || camera.data.imei].filter(Boolean).join(" · ")).join(", ")], ["Tablet IMEI / Number", [tablet?.data.imei, inventoryPhone(line?.data.wirelessNumber)].filter(Boolean).join(" · ")], ...(canTolls ? [["PrePass Device", error ? "Roster Unavailable" : tag || "Not Linked"]] : [])];
  return <section className="space-y-3"><dl className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">{items.map(([label, value]) => <div key={label} className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-4"><dt className="text-xs text-zinc-500">{label}</dt><dd className="mt-2 break-words text-sm text-zinc-200">{value || "Not Recorded"}</dd></div>)}</dl></section>;
}
