"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "@/app/components/AccessLink";
import { Package, Plus, RefreshCw, Truck } from "lucide-react";
import { usePermissions } from "@/app/lib/access";
import { fetchInventory, fetchInventoryHistory, fetchPrePassFleet, fetchTrailers, fetchTruckOptions } from "@/app/lib/api";
import type { InventoryCollection, InventoryData, InventoryEvent, InventoryMovement, InventoryRecord, PrePassFleetList, TruckOption } from "@/app/lib/types";
import { ErrorBanner, ManagementHeader, ManagementSearch, Modal, TablePagination, TableShell, controlClass } from "@/app/components/management/ManagementUI";
import { PrePassFleetTab } from "@/app/tolls/PrePassFleetTab";
import { InventoryEditor, StockMovementDialog, inventoryLabel, inventoryPhone, newInventoryRecord, readable, type InventoryContext } from "./InventoryForms";
import { InventoryImports } from "./InventoryImports";

const tabs = [ ["tablets", "Tablets"], ["samsara-gateways", "Samsara gateways"], ["samsara-cameras", "Samsara dashcams"], ["devices", "Other devices"], ["supplies", "Supplies"], ["lines", "AT&T lines"], ["transponders", "Transponders"], ["equipment", "Truck equipment"], ["history", "History"], ["locations", "Locations"] ] as const;
type Tab = typeof tabs[number][0] | "imports";
export const emptyInventory: InventoryData = { assets: [], lines: [], supplies: [], locations: [], equipment: [], "transponder-notes": [], balances: [], movements: [] };
const button = "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-lg border border-zinc-800 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-50";
const primaryButton = "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-lg bg-blue-600 px-3.5 py-2 text-[13px] font-medium text-white transition hover:bg-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30";
const filterClass = controlClass.replace("w-full", "w-auto max-w-full shrink-0");
const th = "whitespace-nowrap px-4 py-3 text-left text-xs font-medium text-zinc-500";
const td = "px-4 py-3 text-xs text-zinc-300";
const date = (value?: string) => value ? new Date(value).toLocaleString() : "Unknown";
function belongs(record: InventoryRecord, truckId: string, data: InventoryData) { return effectiveTruck(record, data) === truckId; }
function effectiveTruck(record: InventoryRecord, data: InventoryData): string { const parent = data.assets.find(asset => asset.id === (record.data.gatewayId || record.data.assetId)); return record.data.truckId || parent?.data.truckId || ""; }
function holderLabel(holder: string, context: InventoryContext) { const [kind, ...id] = holder.split(":"); const key = id.join(":"); return kind === "location" ? inventoryLabel(context.data.locations.find(location => location.id === key)) : kind === "truck" ? `Truck ${context.trucks.find(truck => truck.id === key)?.unitNumber || key}` : kind === "custodian" ? key : readable(holder); }

export function InventoryWorkspace({ trailers = false, truckId = "" }: { trailers?: boolean; truckId?: string }) {
  const permissions = usePermissions();
  const canManage = permissions.includes(trailers ? "trailers.manage" : "inventory.manage");
  const canTrucks = permissions.includes("trucks.read"), canTolls = permissions.includes("tolls.read");
  const [data, setData] = useState<InventoryData>(emptyInventory), [trailerRows, setTrailerRows] = useState<InventoryRecord[]>([]), [trucks, setTrucks] = useState<TruckOption[]>([]);
  const [tab, setTab] = useState<Tab>(truckId ? "equipment" : "tablets"), [search, setSearch] = useState(""), [status, setStatus] = useState("active"), [assigned, setAssigned] = useState(""), [location, setLocation] = useState("");
  const [page, setPage] = useState(1), [pageSize, setPageSize] = useState(25), [loading, setLoading] = useState(true), [error, setError] = useState("");
  const [editor, setEditor] = useState<{ collection: InventoryCollection; record: InventoryRecord } | null>(null), [detail, setDetail] = useState<{ collection: InventoryCollection; record: InventoryRecord } | null>(null);
  const [moving, setMoving] = useState<InventoryMovement | true | null>(null), [events, setEvents] = useState<InventoryEvent[]>([]);
  const context: InventoryContext = { data, trucks };
  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    try {
      if (trailers) { const records = await fetchTrailers(signal); if (!signal?.aborted) setTrailerRows(records); }
      else { const result = await fetchInventory(signal); if (!signal?.aborted) setData({ ...emptyInventory, ...result }); }
      if (canTrucks && !trailers) { const options = await fetchTruckOptions(); if (!signal?.aborted) setTrucks(options); }
      if (!signal?.aborted) setError("");
    } catch (reason) { if (!signal?.aborted) setError(reason instanceof Error ? reason.message : "Unable to load inventory"); }
    finally { if (!signal?.aborted) setLoading(false); }
  }, [trailers, canTrucks]);
  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => { if (!trailers && !truckId) { const selected = new URLSearchParams(window.location.search).get("tab"); if (selected === "samsara") setTab("samsara-gateways"); else if (selected === "imports" || tabs.some(([key]) => key === selected)) setTab(selected as Tab); } void load(controller.signal); }, 0);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [load, trailers, truckId]);
  const changeTab = (next: Tab) => { setTab(next); setPage(1); setSearch(""); setAssigned(""); setLocation(""); if (!truckId) window.history.replaceState(null, "", `${window.location.pathname}?tab=${next}`); };
  const saved = () => { setEditor(null); setMoving(null); setDetail(null); void load(); };
  const history = (collection = "", id = "") => { setError(""); void fetchInventoryHistory(collection, id).then(setEvents).catch(reason => setError(reason.message)); };
  const collection: InventoryCollection = trailers ? "trailers" : tab === "lines" ? "lines" : tab === "supplies" ? "supplies" : tab === "locations" ? "locations" : tab === "equipment" ? "equipment" : "assets";
  let records = trailers ? trailerRows : data[collection as Exclude<InventoryCollection, "trailers">];
  if (collection === "assets") records = records.filter(record => tab === "tablets" ? record.data.deviceType === "tablet" : tab === "samsara-gateways" ? record.data.deviceType === "samsara_gateway" : tab === "samsara-cameras" ? record.data.deviceType === "samsara_camera" : !["tablet", "samsara_gateway", "samsara_camera"].includes(record.data.deviceType || ""));
  if (truckId) records = records.filter(record => belongs(record, truckId, data));
  const filtered = records.filter(record => {
    const truck = trucks.find(item => item.id === effectiveTruck(record, data));
    const line = data.lines.find(item => item.data.assetId === record.id && item.data.active);
    const linkedAsset = data.assets.find(item => item.id === record.data.assetId);
    const gateway = data.assets.find(item => item.id === record.data.gatewayId), camera = data.assets.find(item => item.data.active && item.data.gatewayId === record.id);
    const haystack = JSON.stringify([record.data, truck?.unitNumber, line?.data.wirelessNumber, inventoryPhone(line?.data.wirelessNumber), inventoryPhone(record.data.wirelessNumber), linkedAsset?.data.imei, linkedAsset?.data.serial, gateway?.data.serial, camera?.data.serial]).toLowerCase();
    return (!search || haystack.includes(search.toLowerCase())) && (status === "all" || status === "active" && record.data.active || status === "archived" && !record.data.active || status === record.data.condition || status === record.data.equipmentState) && (!location || record.data.locationId === location || record.data.storageLocation === location) && (!assigned || assigned === "assigned" && (effectiveTruck(record, data) || record.data.custodian || record.data.assetId) || assigned === "unassigned" && !(effectiveTruck(record, data) || record.data.custodian || record.data.assetId));
  });
  const showDetail = (record: InventoryRecord) => { setDetail({ collection, record }); setEvents([]); history(collection, record.id); };
  const add = (selected: InventoryCollection = collection) => setEditor({ collection: selected, record: newInventoryRecord(selected === "assets" ? { deviceType: tab === "tablets" ? "tablet" : tab === "samsara-gateways" ? "samsara_gateway" : tab === "samsara-cameras" ? "samsara_camera" : "tt_camera", condition: "unknown", equipmentState: "unknown" } : selected === "trailers" ? { status: "stored_unused", condition: "unknown" } : selected === "equipment" ? { truckId, equipmentState: "unknown" } : selected === "supplies" ? { unit: "units" } : {}) });
  const custody = (record: InventoryRecord) => {
    const linkedTruck = effectiveTruck(record, data), truck = trucks.find(item => item.id === linkedTruck);
    return linkedTruck ? canTrucks ? <Link href={`/trucks/detail?id=${linkedTruck}`} className="text-blue-400">{truck?.unitNumber || "Truck"}</Link> : "Assigned truck" : record.data.custodian || (record.data.locationId ? inventoryLabel(data.locations.find(item => item.id === record.data.locationId)) : record.data.gatewayId ? "Gateway unassigned" : "Unassigned");
  };
  const title = trailers ? "Trailers" : truckId ? "Truck equipment" : "Inventory";
  const addLabel = trailers ? "Add trailer" : collection === "assets" ? tab === "samsara-gateways" ? "Add gateway" : tab === "samsara-cameras" ? "Add dashcam" : "Add device" : `Add ${collection === "equipment" ? "equipment" : collection === "lines" ? "line" : collection === "locations" ? "location" : "supply item"}`;
  const headers = trailers ? ["Unit", "VIN", "Year / make", "Plate", "Storage", "Availability / condition", ""] : collection === "assets" ? tab === "tablets" ? ["Make / model", "IMEI", "Tablet number", "Truck / custody", "Condition / presence", ""] : tab === "samsara-gateways" ? ["Gateway serial", "Asset label", "Linked dashcam", "Truck / custody", "Condition / presence", ""] : tab === "samsara-cameras" ? ["Dashcam serial", "Asset label", "Linked gateway", "Truck / custody", "Condition / presence", ""] : ["Device", "IMEI / serial", "Truck / custody", "Condition / presence", ""] : collection === "lines" ? ["Wireless number", "Device IMEI", "User", "Service status", "Plan", "Contract end", ""] : collection === "supplies" ? ["Item / year", "Unit", "Tracking", "Storage stock", "Opening count", "", ""] : collection === "equipment" ? ["Equipment / year", "Truck", "Presence", "Sent", "Attached", "Verified", ""] : ["Location", "Notes", "", "", "", "", ""];
  return <div className="space-y-5 animate-fade-in">
    <ManagementHeader icon={trailers ? Truck : Package} title={title} count={trailers ? trailerRows.length : undefined} />
    {error && <ErrorBanner message={error} />}
    {truckId && <TruckInventorySummary truckId={truckId} context={context} />}
    {trailers ? <p className="text-xs text-zinc-500">Stored company trailers. Condition requires review. Operational Schneider/JB Hunt trailers remain on Status Board.</p> : <nav aria-label="Inventory sections" className="flex flex-wrap gap-1 border-b border-zinc-800">{tabs.filter(([key]) => (key !== "transponders" || canTolls && !truckId) && (!truckId || !["locations", "supplies"].includes(key))).map(([key, label]) => <button key={key} role="tab" aria-selected={tab === key} className={`border-b-2 px-3 py-2.5 text-xs ${tab === key ? "border-blue-500 text-blue-400" : "border-transparent text-zinc-500 hover:text-zinc-300"}`} onClick={() => { changeTab(key); if (key === "history") history(); }}>{label}</button>)}</nav>}
    {tab === "imports" && !trailers ? <InventoryImports context={context} onCommitted={saved} /> : tab === "history" && !trailers ? <>
      <div className="flex flex-wrap items-center gap-2"><button className={button} onClick={() => { history(); void load(); }}>Refresh history</button>{canManage && <button className={button} onClick={() => setMoving(true)}>Stock movement</button>}</div><InventoryHistory events={truckId ? events.filter(event => belongs(event.after, truckId, data) || !!event.before && belongs(event.before, truckId, data)) : events} />
      <StockHistory context={context} movements={truckId ? data.movements.filter(movement => movement.from === `truck:${truckId}` || movement.to === `truck:${truckId}`) : data.movements} canManage={canManage} onReverse={setMoving} />
    </> : <>
      {tab === "transponders" && !trailers && (canTolls ? <PrePassFleetTab view="transponders" /> : <p className="text-sm text-zinc-500">Tolls access is required to view PrePass transponders.</p>)}
      {(trailers || tab !== "transponders") && <>
        <div aria-label="Inventory controls" role="group" className="flex flex-wrap items-center gap-2">
          <div className="min-w-48 flex-1 basis-60 [&>div]:max-w-none"><ManagementSearch value={search} onChange={value => { setSearch(value); setPage(1); }} placeholder={trailers ? "Search unit, VIN, plate or storage…" : "Search identifier, number, truck or notes…"} /></div>
          <select aria-label="Status filter" className={filterClass} value={status} onChange={event => { setStatus(event.target.value); setPage(1); }}>{["active", "all", "archived", ...conditionsForFilter].map(value => <option key={value} value={value}>{readable(value)}</option>)}</select>
          {!trailers && <select aria-label="Assignment filter" className={filterClass} value={assigned} onChange={event => { setAssigned(event.target.value); setPage(1); }}><option value="">All assignments</option><option value="assigned">Assigned</option><option value="unassigned">Unassigned</option></select>}
          {trailers ? <input aria-label="Storage location filter" placeholder="Storage location" className={filterClass} value={location} onChange={event => { setLocation(event.target.value); setPage(1); }} /> : <select aria-label="Location filter" className={filterClass} value={location} onChange={event => { setLocation(event.target.value); setPage(1); }}><option value="">All locations</option>{data.locations.map(item => <option key={item.id} value={item.id}>{inventoryLabel(item)}</option>)}</select>}
          <button className={button} onClick={() => void load()} disabled={loading}><RefreshCw className="h-3.5 w-3.5" />Refresh</button>
          {!trailers && canManage && tab !== "transponders" && <button className={button} onClick={() => setMoving(true)}>Stock movement</button>}
          {canManage && <button className={primaryButton} onClick={() => add()}><Plus className="h-4 w-4" />{addLabel}</button>}
        </div>
        {loading && <p className="text-sm text-zinc-500" role="status">Loading…</p>}
        <TableShell><table className="w-full"><thead><tr>{headers.map((label, index) => <th key={index} className={th}>{label}</th>)}</tr></thead><tbody>
          {filtered.slice((Math.min(page, Math.max(1, Math.ceil(filtered.length / pageSize))) - 1) * pageSize, Math.min(page, Math.max(1, Math.ceil(filtered.length / pageSize))) * pageSize).map(record => {
            const d = record.data, line = data.lines.find(item => item.data.active && item.data.assetId === record.id), camera = data.assets.find(item => item.data.active && item.data.gatewayId === record.id);
            const balances = data.balances.filter(balance => balance.supplyId === record.id && balance.holder.startsWith("location:"));
            const condition = [d.condition, d.equipmentState].filter(value => value && value !== "unknown").map(readable).join(" · ") || "Unknown";
            const deviceColumns = tab === "tablets" ? [[d.make, d.model].filter(Boolean).join(" · "), d.imei, line ? inventoryPhone(line.data.wirelessNumber) : "No service line", custody(record), condition] : tab === "samsara-gateways" || tab === "samsara-cameras" ? [d.serial, d.assetLabel, tab === "samsara-gateways" ? camera ? inventoryLabel(camera) : "—" : d.gatewayId ? inventoryLabel(data.assets.find(item => item.id === d.gatewayId)) : "—", custody(record), condition] : [[readable(d.deviceType), d.make, d.model].filter(Boolean).join(" · "), [d.imei, d.serial, d.assetLabel].filter(Boolean).join(" · "), custody(record), condition];
            const columns = trailers ? [d.unitNumber, d.vin, [d.year, d.make].filter(Boolean).join(" "), [d.plate, d.plateState].filter(Boolean).join(" "), d.storageLocation, `${readable(d.status)} · ${readable(d.condition)}`] : collection === "assets" ? deviceColumns : collection === "lines" ? [inventoryPhone(d.wirelessNumber), inventoryLabel(data.assets.find(item => item.id === d.assetId)), d.custodian, d.status, d.plan, d.contractEnd] : collection === "supplies" ? [[d.name, d.batchYear].filter(Boolean).join(" · "), d.unit, d.reusable ? "Reusable" : "Consumable", balances.length ? balances.map(balance => `${holderLabel(balance.holder, context)}: ${balance.quantity}`).join(" · ") : "Unknown", balances.length && balances.every(balance => balance.verified) ? "Verified" : "Unverified", ""] : collection === "equipment" ? [[d.name, d.batchYear].filter(Boolean).join(" · "), custody(record), readable(d.equipmentState), d.sent ? d.sentDate || "Sent · date unknown" : "Not recorded", d.attached ? d.attachedDate || "Attached · date unknown" : "Not recorded", d.verifiedAt ? `${date(d.verifiedAt)} · ${d.verifiedBy}` : "Unverified"] : [d.name, d.notes, "", "", "", ""];
            return <tr key={record.id} className="border-t border-zinc-800/60 hover:bg-zinc-800/20">{columns.map((value, index) => <td key={index} className={td}>{index === 0 ? <button className="text-left font-medium text-blue-400 hover:underline" onClick={() => showDetail(record)}>{value || inventoryLabel(record)}{!d.active && <span className="ml-2 text-zinc-500">Archived</span>}</button> : value || "—"}</td>)}<td className={td}>{canManage && <button className="text-blue-400" onClick={() => setEditor({ collection, record })}>Edit</button>}</td></tr>;
          })}
          {!loading && !filtered.length && <tr><td colSpan={headers.length} className="p-10 text-center text-sm text-zinc-500">{search || status !== "active" || assigned || location ? "No matching records." : `No ${trailers ? "trailers" : tab === "tablets" ? "tablets" : "records"} yet.`}</td></tr>}
        </tbody></table></TableShell>
        <TablePagination page={Math.min(page, Math.max(1, Math.ceil(filtered.length / pageSize)))} pageSize={pageSize} totalItems={filtered.length} totalPages={Math.max(1, Math.ceil(filtered.length / pageSize))} onPageChange={setPage} onPageSizeChange={value => { setPageSize(value); setPage(1); }} />
        {collection === "supplies" && <><p className="text-xs text-zinc-500">Stock stays unverified until physically counted. Legacy checkboxes do not establish quantities. Reusable items can be returned; used stickers remain in history.</p><StockHistory context={context} movements={data.movements} canManage={canManage} onReverse={setMoving} /></>}
      </>}
    </>}
    {editor && <InventoryEditor key={`${editor.collection}:${editor.record.id}`} collection={editor.collection} record={editor.record} context={context} onClose={() => setEditor(null)} onSaved={saved} />}
    {moving && <StockMovementDialog context={context} initial={moving === true ? undefined : moving} onClose={() => setMoving(null)} onSaved={saved} />}
    {detail && <Modal title={inventoryLabel(detail.record)} isSaving={false} submitLabel="Close" hideSubmit onSubmit={event => event.preventDefault()} onClose={() => setDetail(null)}><dl className="grid gap-3 text-xs sm:grid-cols-2">{Object.entries(detail.record.data).filter(([, value]) => value !== "").map(([key, value]) => <div key={key}><dt className="capitalize text-zinc-500">{key.replace(/Id$/, "").replace(/([A-Z])/g, " $1")}</dt><dd className="mt-1 break-all text-zinc-200">{key === "wirelessNumber" ? inventoryPhone(String(value)) : key === "truckId" ? custody(detail.record) : key === "gatewayId" || key === "assetId" ? inventoryLabel(data.assets.find(asset => asset.id === value)) : key === "locationId" ? inventoryLabel(data.locations.find(item => item.id === value)) : key === "supplyId" ? inventoryLabel(data.supplies.find(item => item.id === value)) : key === "registrationUrl" ? <a href={String(value)} className="text-blue-400" target="_blank" rel="noreferrer">Open registration</a> : typeof value === "boolean" ? value ? "Yes" : "No" : readable(String(value))}</dd></div>)}</dl><div className="mt-5"><InventoryHistory events={events} /></div></Modal>}
  </div>;
}
const conditionsForFilter = ["good", "damaged", "repair", "lost", "retired", "unknown", "reported_present", "verified_installed", "missing"];
function TruckInventorySummary({ truckId, context }: { truckId: string; context: InventoryContext }) {
  const canTolls = usePermissions().includes("tolls.read");
  const [prepass, setPrepass] = useState<PrePassFleetList | null>(null), [error, setError] = useState("");
  useEffect(() => { if (!canTolls) return; const controller = new AbortController(); void fetchPrePassFleet(controller.signal).then(value => { if (!controller.signal.aborted) setPrepass(value); }).catch(reason => { if (!controller.signal.aborted) setError(reason.message); }); return () => controller.abort(); }, [canTolls]);
  const assets = context.data.assets.filter(asset => asset.data.active && belongs(asset, truckId, context.data));
  const gateway = assets.find(asset => asset.data.deviceType === "samsara_gateway"), camera = assets.find(asset => asset.data.deviceType === "samsara_camera"), tablet = assets.find(asset => asset.data.deviceType === "tablet");
  const line = context.data.lines.find(item => item.data.active && item.data.assetId === tablet?.id);
  const tag = prepass?.vehicles.find(item => item.truckId === truckId)?.vehicle.transponderNumber;
  const items = [["Samsara gateway serial", gateway?.data.serial], ["Linked dashcam", camera?.data.serial], ["Tablet IMEI / number", [tablet?.data.imei, inventoryPhone(line?.data.wirelessNumber)].filter(Boolean).join(" · ")], ...(canTolls ? [["PrePass device", error ? "Roster unavailable" : tag || "Not linked"]] : [])];
  return <section className="space-y-3"><dl className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">{items.map(([label, value]) => <div key={label} className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-4"><dt className="text-xs text-zinc-500">{label}</dt><dd className="mt-2 break-all font-mono text-sm text-zinc-200">{value || "Not recorded"}</dd></div>)}</dl><p className="text-xs text-zinc-500">Equipment presence is operational inventory information. It does not establish compliance or expiry validity.</p>{context.data.balances.filter(balance => balance.holder === `truck:${truckId}` && balance.quantity > 0).map(balance => <p key={balance.supplyId} className="text-xs text-zinc-300">Issued: {inventoryLabel(context.data.supplies.find(item => item.id === balance.supplyId))} · {balance.quantity} {context.data.supplies.find(item => item.id === balance.supplyId)?.data.unit}</p>)}</section>;
}
export function InventoryHistory({ events }: { events: InventoryEvent[] }) { return <section className="space-y-3"><h2 className="text-sm font-semibold">Record history</h2>{!events.length && <p className="text-xs text-zinc-500">No recorded changes.</p>}{events.map(event => <details key={event.id} className="rounded-lg border border-zinc-800 p-3 text-xs"><summary className="cursor-pointer text-zinc-300">{date(event.createdAt)} · {event.actor} · {readable(event.action)} · {inventoryLabel(event.after)}</summary><div className="mt-3 grid gap-3 sm:grid-cols-2"><div><p className="mb-1 text-zinc-500">Before</p><pre className="overflow-auto whitespace-pre-wrap break-all">{JSON.stringify(event.before?.data || null, null, 2)}</pre></div><div><p className="mb-1 text-zinc-500">After</p><pre className="overflow-auto whitespace-pre-wrap break-all">{JSON.stringify(event.after.data, null, 2)}</pre></div></div></details>)}</section>; }
function StockHistory({ context, movements, canManage, onReverse }: { context: InventoryContext; movements: InventoryMovement[]; canManage: boolean; onReverse: (movement: InventoryMovement) => void }) { return <section className="space-y-3"><h2 className="text-sm font-semibold">Stock movements <span className="font-normal text-zinc-500">Latest 1,000</span></h2><TableShell><table className="w-full"><thead><tr>{["Date / actor", "Item", "Movement", "From → to", "Quantity", "Reason", ""].map(label => <th key={label} className={th}>{label}</th>)}</tr></thead><tbody>{movements.map(movement => <tr key={movement.id} className="border-t border-zinc-800"><td className={td}>{date(movement.createdAt)}<div className="text-zinc-500">{movement.actor}</div></td><td className={td}>{inventoryLabel(context.data.supplies.find(item => item.id === movement.supplyId))}</td><td className={td}>{readable(movement.kind)}</td><td className={td}>{holderLabel(movement.from, context)} → {holderLabel(movement.to, context)}</td><td className={td}>{movement.quantity}</td><td className={td}>{movement.reason}</td><td className={td}>{canManage && movement.kind !== "reversal" && movement.quantity > 0 && !context.data.movements.some(item => item.reversesId === movement.id) && <button className="text-blue-400" onClick={() => onReverse({ ...movement, id: crypto.randomUUID(), kind: "reversal", from: movement.to, to: movement.from, reason: "", physicalCount: false, reversesId: movement.id, actor: "", createdAt: "" })}>Reverse</button>}</td></tr>)}</tbody></table></TableShell>{!movements.length && <p className="text-xs text-zinc-500">No stock movements recorded.</p>}</section>; }
