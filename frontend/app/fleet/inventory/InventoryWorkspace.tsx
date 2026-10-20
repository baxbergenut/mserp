"use client";

import { useCallback, useEffect, useState, type ReactNode } from "react";
import Link from "@/app/components/AccessLink";
import { Package, Plus, RefreshCw, Truck } from "lucide-react";
import { usePermissions } from "@/app/lib/access";
import {
  fetchInventory,
  fetchInventoryDirectory,
  fetchPrePassFleet,
  fetchTrailers,
  fetchTruckOptions,
} from "@/app/lib/api";
import type {
  InventoryCollection,
  InventoryData,
  InventoryFields,
  InventoryRecord,
  PrePassFleetList,
  TruckOption,
} from "@/app/lib/types";
import {
  ErrorBanner,
  Field,
  ManagementHeader,
  ManagementSearch,
  Modal,
  TablePagination,
  TableShell,
  controlClass,
} from "@/app/components/management/ManagementUI";
import { PrePassFleetTab } from "@/app/tolls/PrePassFleetTab";
import {
  InventoryEditor,
  inventoryLabel,
  inventoryPhone,
  newInventoryRecord,
  readable,
  type InventoryContext,
} from "./InventoryForms";
import {
  effectiveTruck,
  installationState,
  StatusBadge,
  visibleDevice,
} from "./InventoryStatus";
import { SuppliesMatrix } from "./SuppliesMatrix";
import { InventoryImports } from "./InventoryImports";
import { ColumnFilter } from "@/app/components/management/ColumnFilter";
import { InventoryDetail } from "./InventoryDetail";
import { InventoryReview } from "./InventoryReview";
import { InventoryOperation, type DeviceAction } from "./InventoryOperation";
import {
  availableToAssign,
  custodyKind,
  custodyLabel,
  deviceDescription,
  holder,
  lifecycleStates,
  unavailable,
} from "./inventoryView";

const tabs = [
  ["tablets", "Tablets"],
  ["phones", "Phones"],
  ["dashcams", "Dashcams"],
  ["samsara-gateways", "Samsara Gateways"],
  ["supplies", "Supplies"],
  ["lines", "AT&T Lines"],
  ["transponders", "Transponders"],
  ["equipment", "Truck Equipment"],
  ["locations", "Locations & People"],
  ["attention", "Needs Attention"],
] as const;
type Tab = (typeof tabs)[number][0] | "imports";
export const emptyInventory: InventoryData = {
  assets: [],
  lines: [],
  supplies: [],
  locations: [],
  custodians: [],
  equipment: [],
  "transponder-notes": [],
  balances: [],
  movements: [],
};
const button =
  "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-lg border border-zinc-800 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-50";
const primary =
  "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-lg bg-blue-600 px-3.5 py-2 text-[13px] font-medium text-white hover:bg-blue-500 disabled:opacity-50";
const filterClass = controlClass.replace("w-full", "w-auto max-w-52 shrink-0");
const th =
    "whitespace-nowrap px-4 py-3 text-left text-xs font-medium text-zinc-500",
  td = "px-4 py-3 text-xs text-zinc-300";

function startingTab(selected: string | null): Tab {
  if (selected === "samsara") return "samsara-gateways";
  if (selected === "samsara-cameras" || selected === "devices")
    return "dashcams";
  if (selected === "history") return "equipment";
  return selected === "imports" || tabs.some(([key]) => key === selected)
    ? (selected as Tab)
    : "tablets";
}

export function InventoryWorkspace({
  trailers = false,
  truckId = "",
}: {
  trailers?: boolean;
  truckId?: string;
}) {
  const permissions = usePermissions();
  const canManage = permissions.includes(
    trailers ? "trailers.manage" : "inventory.manage",
  );
  const canTrucks = permissions.includes("trucks.read"),
    canTolls = permissions.includes("tolls.read");
  const [data, setData] = useState<InventoryData>(emptyInventory),
    [trailerRows, setTrailerRows] = useState<InventoryRecord[]>([]),
    [trucks, setTrucks] = useState<TruckOption[]>([]);
  const [tab, setTab] = useState<Tab>(truckId ? "equipment" : "tablets"),
    [search, setSearch] = useState(""),
    [status, setStatus] = useState("record:active"),
    [assigned, setAssigned] = useState(""),
    [location, setLocation] = useState(""),
    [brand, setBrand] = useState("");
  const [page, setPage] = useState(1),
    [pageSize, setPageSize] = useState(25),
    [loading, setLoading] = useState(true),
    [error, setError] = useState("");
  const [editor, setEditor] = useState<{
      collection: InventoryCollection;
      record: InventoryRecord;
    } | null>(null),
    [detail, setDetail] = useState<{
      collection: InventoryCollection;
      record: InventoryRecord;
    } | null>(null);
  const [assigning, setAssigning] = useState(false),
    [chosenAsset, setChosenAsset] = useState(""),
    [chosenTruck, setChosenTruck] = useState(truckId);
  const [condition, setCondition] = useState(""),
    [lifecycle, setLifecycle] = useState(""),
    [installation, setInstallation] = useState(""),
    [sort, setSort] = useState({ column: 0, direction: "asc" }),
    [directory, setDirectory] = useState<"locations" | "custodians">(
      "locations",
    );
  const [operation, setOperation] = useState<{
      record: InventoryRecord;
      action: DeviceAction;
    } | null>(null),
    [preferencesReady, setPreferencesReady] = useState(false);
  const preferenceKey =
    "inventory-view:" + (trailers ? "trailers" : truckId || "fleet") + ":";
  const restore = useCallback(
    (next: Tab) => {
      let value: Record<
        string,
        string | number | { column: number; direction: string }
      > = {};
      try {
        value = JSON.parse(
          sessionStorage.getItem(preferenceKey + next) || "{}",
        );
      } catch {
        /* Storage is optional. */
      }
      if (!value || typeof value !== "object") value = {};
      setSearch(String(value.search || ""));
      setStatus(String(value.status || "record:active"));
      setAssigned(String(value.assigned || ""));
      setLocation(String(value.location || ""));
      setBrand(String(value.brand || ""));
      setCondition(String(value.condition || ""));
      setLifecycle(String(value.lifecycle || ""));
      setInstallation(String(value.installation || ""));
      setSort(
        value.sort && typeof value.sort === "object"
          ? value.sort
          : { column: 0, direction: "asc" },
      );
      setPage(1);
      setPageSize(
        [25, 50, 100].includes(Number(value.pageSize))
          ? Number(value.pageSize)
          : 25,
      );
    },
    [preferenceKey],
  );
  useEffect(() => {
    if (!preferencesReady) return;
    try {
      sessionStorage.setItem(
        preferenceKey + tab,
        JSON.stringify({
          search,
          status,
          assigned,
          location,
          brand,
          condition,
          lifecycle,
          installation,
          sort,
          pageSize,
        }),
      );
    } catch {
      /* Storage is optional. */
    }
  }, [
    preferencesReady,
    preferenceKey,
    tab,
    search,
    status,
    assigned,
    location,
    brand,
    condition,
    lifecycle,
    installation,
    sort,
    pageSize,
  ]);
  const collection: InventoryCollection = trailers
    ? "trailers"
    : tab === "lines"
      ? "lines"
      : tab === "supplies"
        ? "supplies"
        : tab === "locations"
          ? directory
          : "assets";
  const deviceType =
    tab === "phones"
      ? "phone"
      : tab === "dashcams"
        ? "dashcam"
        : tab === "samsara-gateways"
          ? "samsara_gateway"
          : "tablet";
  const headers = trailers
    ? [
        "Unit",
        "VIN",
        "Year / Make",
        "Plate",
        "Storage",
        "Availability",
        "Condition",
        "",
      ]
    : collection === "lines"
      ? [
          "Wireless Number",
          "Device IMEI",
          "Carrier User",
          "Carrier Status",
          "Plan",
          "Contract End",
          "",
        ]
      : collection === "locations" || collection === "custodians"
        ? [directory === "custodians" ? "Custodian" : "Location", "Notes", ""]
        : tab === "samsara-gateways"
          ? [
              "Gateway Serial",
              "Linked Dashcam",
              "Truck / Custody",
              "Condition",
              "Installation",
              "",
            ]
          : tab === "dashcams"
            ? [
                "Brand",
                "IMEI / Serial",
                "Model",
                "Truck / Custody",
                "Linked Gateway",
                "Condition",
                "Installation",
                "",
              ]
            : tab === "equipment"
              ? [
                  "Device",
                  "IMEI / Serial",
                  "Phone Number",
                  "Truck / Custody",
                  "Condition",
                  "Installation",
                  "",
                ]
              : [
                  "Make / Model",
                  "IMEI",
                  tab === "phones" ? "Phone Number" : "Tablet Number",
                  "Truck / Custody",
                  "Condition",
                  "Installation",
                  "",
                ];
  const directoryMode =
    !trailers &&
    !["supplies", "attention", "imports", "transponders"].includes(tab);
  const [pageRecords, setPageRecords] = useState<InventoryRecord[]>([]),
    [directoryTotal, setDirectoryTotal] = useState(0),
    [directoryPage, setDirectoryPage] = useState(1);
  const queryKey = new URLSearchParams({
    collection,
    deviceType: tab === "equipment" ? "equipment" : deviceType,
    truckId,
    search,
    active: status,
    condition,
    lifecycle,
    presence: installation,
    custody: assigned,
    location,
    brand,
    sort: headers[sort.column] || headers[0],
    direction: sort.direction,
    page: String(page),
    pageSize: String(pageSize),
  }).toString();
  const context: InventoryContext = { data, trucks };
  const [dialogContext, setDialogContext] = useState<InventoryContext | null>(
    null,
  );
  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      try {
        if (trailers) {
          const records = await fetchTrailers(signal);
          if (!signal?.aborted) setTrailerRows(records);
        } else if (directoryMode) {
          const result = await fetchInventoryDirectory(
            new URLSearchParams(queryKey),
            signal,
          );
          if (!signal?.aborted) {
            setData({ ...emptyInventory, ...result.context });
            setPageRecords(result.records);
            setDirectoryTotal(result.total);
            setDirectoryPage(result.page);
          }
        } else if (tab !== "transponders") {
          const result = await fetchInventory(signal);
          if (!signal?.aborted) setData({ ...emptyInventory, ...result });
        }
        if (canTrucks && !trailers) {
          const choices = await fetchTruckOptions();
          if (!signal?.aborted) setTrucks(choices);
        }
        if (!signal?.aborted) setError("");
      } catch (reason) {
        if (!signal?.aborted)
          setError(
            reason instanceof Error
              ? reason.message
              : "Unable to load inventory",
          );
      } finally {
        if (!signal?.aborted) setLoading(false);
      }
    },
    [
      trailers,
      canTrucks,
      directoryMode,
      queryKey,
      tab,
      setPageRecords,
      setDirectoryPage,
      setDirectoryTotal,
    ],
  );
  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      if (!trailers && !truckId) {
        const selected = new URLSearchParams(window.location.search).get("tab");
        const next = startingTab(selected);
        setTab(next);
        restore(next);
        if (selected === "samsara-cameras") setBrand("Samsara");
      } else restore(truckId ? "equipment" : "tablets");
      setPreferencesReady(true);
    }, 0);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [trailers, truckId, restore]);
  useEffect(() => {
    if (!preferencesReady) return;
    const controller = new AbortController();
    const timer = setTimeout(() => void load(controller.signal), 150);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [load, preferencesReady]);
  const changeTab = (next: Tab) => {
    setTab(next);
    restore(next);
    if (!truckId)
      window.history.replaceState(
        null,
        "",
        window.location.pathname + "?tab=" + next,
      );
  };
  const openFull = async (
    selected: InventoryCollection,
    record: InventoryRecord,
    action?: DeviceAction,
  ) => {
    if (selected === "trailers") {
      setDialogContext(context);
      setEditor({ collection: selected, record });
      return;
    }
    try {
      const full = await fetchInventory();
      const next = { ...emptyInventory, ...full };
      setDialogContext({ data: next, trucks });
      const current = next[selected].find((r) => r.id === record.id) || record;
      if (action) setOperation({ record: current, action });
      else setEditor({ collection: selected, record: current });
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to load record");
    }
  };
  const saved = () => {
    setDialogContext(null);
    setEditor(null);
    setDetail(null);
    setOperation(null);
    void load();
  };
  let records = trailers
    ? trailerRows
    : data[collection as Exclude<InventoryCollection, "trailers">];
  if (collection === "assets")
    records = records.filter(
      (record) =>
        visibleDevice(record.data) &&
        (tab === "equipment"
          ? !!effectiveTruck(record, data)
          : record.data.deviceType === deviceType),
    );
  if (truckId)
    records = records.filter(
      (record) => effectiveTruck(record, data) === truckId,
    );
  const filtered = directoryMode
    ? [...pageRecords]
    : records.filter((record) => {
        const d = record.data,
          truck = trucks.find(
            (item) => item.id === effectiveTruck(record, data),
          );
        const line = data.lines.find(
          (item) => item.data.assetId === record.id && item.data.active,
        );
        const linkedAsset = data.assets.find((item) => item.id === d.assetId),
          gateway = data.assets.find((item) => item.id === d.gatewayId);
        const camera = data.assets.find(
          (item) => item.data.active && item.data.gatewayId === record.id,
        );
        const haystack = JSON.stringify([
          d,
          truck?.unitNumber,
          inventoryPhone(line?.data.wirelessNumber),
          line?.data.wirelessNumber,
          inventoryPhone(d.wirelessNumber),
          linkedAsset?.data.imei,
          gateway?.data.serial,
          camera?.data.serial,
        ]).toLowerCase();
        const [kind, code] = status.split(":");
        const matchesStatus =
          status === "all" ||
          (kind === "record" && (code === "active" ? d.active : !d.active));
        return (
          (!search || haystack.includes(search.toLowerCase())) &&
          matchesStatus &&
          (!brand || d.brand === brand) &&
          (!condition || (d.condition || "unknown") === condition) &&
          (!lifecycle || (d.status || "unknown") === lifecycle) &&
          (!installation || installationState(record, data) === installation) &&
          (!location ||
            holder(record, data).locationId === location ||
            d.storageLocation === location) &&
          (!assigned ||
            (assigned === "available"
              ? availableToAssign(record, data)
              : custodyKind(record, data) === assigned))
        );
      });
  const custody = (record: InventoryRecord) => {
    const linkedTruck = effectiveTruck(record, data),
      truck = trucks.find((item) => item.id === linkedTruck);
    if (linkedTruck)
      return canTrucks ? (
        <Link
          href={"/trucks/detail?id=" + linkedTruck}
          className="text-blue-400"
        >
          {truck?.unitNumber || "Truck"}
        </Link>
      ) : (
        "Assigned Truck"
      );
    return custodyLabel(record, data, trucks);
  };
  const add = (selected: InventoryCollection = collection) => {
    setDialogContext(null);
    const defaults: Partial<InventoryFields> =
      selected === "assets"
        ? {
            deviceType,
            ...(deviceType === "dashcam" ? { brand: brand || "Samsara" } : {}),
            condition: "unknown",
            status: "unknown",
            equipmentState: "unknown",
            ...(truckId ? { truckId } : {}),
          }
        : selected === "trailers"
          ? { status: "stored_unused", condition: "unknown" }
          : selected === "supplies"
            ? { unit: "units" }
            : selected === "lines"
              ? { status: "unknown" }
              : {};
    setEditor({ collection: selected, record: newInventoryRecord(defaults) });
  };
  const title = trailers
    ? "Trailers"
    : truckId
      ? "Truck Equipment"
      : "Inventory";
  const addLabel = trailers
    ? "Add Trailer"
    : collection === "assets"
      ? tab === "equipment" || truckId
        ? "Assign Device"
        : "Add " +
          readable(deviceType === "samsara_gateway" ? "gateway" : deviceType)
      : tab === "lines"
        ? "Add Line"
        : directory === "custodians"
          ? "Add Custodian"
          : "Add Location";
  const columns = (record: InventoryRecord): ReactNode[] => {
    const d = record.data,
      line = data.lines.find(
        (item) => item.data.active && item.data.assetId === record.id,
      );
    const condition = <StatusBadge value={d.condition} />,
      installation = <StatusBadge value={installationState(record, data)} />;
    if (trailers)
      return [
        d.unitNumber,
        d.vin,
        [d.year, d.make].filter(Boolean).join(" "),
        [d.plate, d.plateState].filter(Boolean).join(" "),
        d.storageLocation,
        <StatusBadge key="availability" value={d.status} />,
        condition,
      ];
    if (collection === "lines")
      return [
        inventoryPhone(d.wirelessNumber),
        inventoryLabel(data.assets.find((item) => item.id === d.assetId)),
        d.custodian,
        <StatusBadge key="carrier" value={d.status} />,
        d.plan,
        d.contractEnd,
      ];
    if (collection === "locations" || collection === "custodians")
      return [d.name, d.notes];
    if (tab === "samsara-gateways")
      return [
        d.serial,
        inventoryLabel(
          data.assets.find(
            (item) =>
              item.data.active &&
              item.data.deviceType === "dashcam" &&
              item.data.gatewayId === record.id,
          ),
        ),
        custody(record),
        condition,
        installation,
      ];
    if (tab === "dashcams")
      return [
        d.brand,
        d.imei || d.serial,
        d.model,
        custody(record),
        d.gatewayId
          ? inventoryLabel(data.assets.find((item) => item.id === d.gatewayId))
          : "—",
        condition,
        installation,
      ];
    if (tab === "equipment")
      return [
        [readable(d.deviceType), deviceDescription(d)]
          .filter(Boolean)
          .join(" · "),
        d.imei || d.serial,
        inventoryPhone(line?.data.wirelessNumber),
        custody(record),
        condition,
        installation,
      ];
    return [
      deviceDescription(d) || inventoryLabel(record),
      d.imei || d.serial || d.assetLabel,
      line ? inventoryPhone(line.data.wirelessNumber) : "No Service Line",
      custody(record),
      condition,
      installation,
    ];
  };
  const sortValue = (record: InventoryRecord) => {
    const label = headers[sort.column],
      d = record.data;
    if (label === "Truck / Custody") return custodyLabel(record, data, trucks);
    if (label === "Condition") return d.condition || "unknown";
    if (label === "Installation") return installationState(record, data);
    const value = columns(record)[sort.column];
    return typeof value === "string" ? value : "";
  };
  if (!directoryMode)
    filtered.sort(
      (a, b) =>
        (sortValue(a).localeCompare(sortValue(b), undefined, {
          numeric: true,
        }) || a.id.localeCompare(b.id)) * (sort.direction === "desc" ? -1 : 1),
    );
  const totalItems = directoryMode ? directoryTotal : filtered.length;
  const totalPages = Math.max(1, Math.ceil(totalItems / pageSize)),
    currentPage = directoryMode ? directoryPage : Math.min(page, totalPages);
  const assignmentData = dialogContext?.data || data;
  const assignable = assignmentData.assets.filter(
    (asset) =>
      visibleDevice(asset.data) &&
      availableToAssign(asset, assignmentData) &&
      (tab === "equipment" || asset.data.deviceType === deviceType),
  );
  return (
    <div className="space-y-5 animate-fade-in">
      <ManagementHeader
        icon={trailers ? Truck : Package}
        title={title}
        count={trailers ? trailerRows.length : undefined}
      />
      {error && <ErrorBanner message={error} />}
      {truckId && <TruckInventorySummary truckId={truckId} context={context} />}
      {trailers ? (
        <p className="text-xs text-zinc-500">
          Stored company trailers. Condition requires review. Operational
          Schneider/JB Hunt trailers remain on Status Board.
        </p>
      ) : (
        <nav
          aria-label="Inventory Sections"
          className="flex flex-wrap gap-1 border-b border-zinc-800"
        >
          {tabs
            .filter(
              ([key]) =>
                (key !== "transponders" || (canTolls && !truckId)) &&
                (!truckId ||
                  !["locations", "supplies", "attention"].includes(key)),
            )
            .map(([key, label]) => (
              <button
                key={key}
                role="tab"
                aria-selected={tab === key}
                className={
                  "border-b-2 px-3 py-2.5 text-xs " +
                  (tab === key
                    ? "border-blue-500 text-blue-400"
                    : "border-transparent text-zinc-500 hover:text-zinc-300")
                }
                onClick={() => changeTab(key)}
              >
                {label}
              </button>
            ))}
        </nav>
      )}
      {tab === "attention" && !trailers ? (
        <InventoryReview
          context={context}
          onOpen={(collection, record) => setDetail({ collection, record })}
          onEdit={
            canManage
              ? (collection, record) => setEditor({ collection, record })
              : undefined
          }
        />
      ) : tab === "imports" && !trailers ? (
        <InventoryImports context={context} onCommitted={saved} />
      ) : tab === "transponders" && !trailers ? (
        canTolls ? (
          <PrePassFleetTab view="transponders" />
        ) : (
          <p className="text-sm text-zinc-500">
            Tolls access is required to view PrePass transponders.
          </p>
        )
      ) : tab === "supplies" && !trailers ? (
        <SuppliesMatrix
          context={context}
          canManage={canManage}
          canTrucks={canTrucks}
          loading={loading}
          onRefresh={() => void load()}
          onAdd={() => add("supplies")}
          onEdit={(record) => setEditor({ collection: "supplies", record })}
          onError={setError}
          onSaved={(record) =>
            setData((previous) => ({
              ...previous,
              equipment: [
                ...previous.equipment.filter((item) => item.id !== record.id),
                record,
              ],
            }))
          }
        />
      ) : (
        <>
          <div
            role="group"
            aria-label="Inventory controls"
            className="flex flex-wrap items-center gap-2"
          >
            <div className="min-w-40 flex-1 basis-full sm:basis-48 [&>div]:max-w-none">
              <ManagementSearch
                value={search}
                onChange={(value) => {
                  setSearch(value);
                  setPage(1);
                }}
                placeholder={
                  trailers
                    ? "Search unit, VIN, plate or storage…"
                    : "Search identifier, number, truck or notes…"
                }
              />
            </div>
            {tab === "locations" && !trailers && (
              <select
                aria-label="Directory"
                className={filterClass}
                value={directory}
                onChange={(e) => {
                  setDirectory(e.target.value as "locations" | "custodians");
                  setPage(1);
                }}
              >
                <option value="locations">Locations</option>
                <option value="custodians">Custodians</option>
              </select>
            )}
            <button
              className={button}
              disabled={loading}
              onClick={() => void load()}
            >
              <RefreshCw className="h-4 w-4" />
              Refresh
            </button>
            {canManage && (!(tab === "equipment" || truckId) || canTrucks) && (
              <button
                className={primary}
                onClick={() => {
                  if (!trailers && (tab === "equipment" || truckId)) {
                    void fetchInventory()
                      .then((full) => {
                        setDialogContext({
                          data: { ...emptyInventory, ...full },
                          trucks,
                        });
                        setChosenAsset("");
                        setChosenTruck(truckId);
                        setAssigning(true);
                      })
                      .catch((e) => setError(e.message));
                  } else add();
                }}
              >
                <Plus className="h-4 w-4" />
                {addLabel}
              </button>
            )}
          </div>
          {loading && (
            <p role="status" className="text-sm text-zinc-500">
              Loading…
            </p>
          )}
          <TableShell>
            <table className="w-full">
              <thead>
                <tr>
                  {headers.map((label, index) => (
                    <th
                      key={index}
                      className={th}
                      aria-sort={
                        sort.column === index
                          ? sort.direction === "asc"
                            ? "ascending"
                            : "descending"
                          : undefined
                      }
                    >
                      {label && (
                        <ColumnFilter
                          label={label}
                          active={
                            (index === 0 && status !== "record:active") ||
                            (label === "Brand" && !!brand) ||
                            (label === "Truck / Custody" &&
                              !!(assigned || location)) ||
                            (label === "Condition" &&
                              !!(condition || lifecycle)) ||
                            (label === "Installation" && !!installation) ||
                            (label === "Carrier Status" && !!lifecycle)
                          }
                        >
                          <div className="flex gap-3 text-xs">
                            <button
                              type="button"
                              onClick={() =>
                                setSort({ column: index, direction: "asc" })
                              }
                            >
                              Sort A–Z
                            </button>
                            <button
                              type="button"
                              onClick={() =>
                                setSort({ column: index, direction: "desc" })
                              }
                            >
                              Sort Z–A
                            </button>
                          </div>
                          {index === 0 && (
                            <select
                              aria-label="Status Filter"
                              className={controlClass}
                              value={status}
                              onChange={(e) => {
                                setStatus(e.target.value);
                                setPage(1);
                              }}
                            >
                              <option value="record:active">
                                Active Records
                              </option>
                              <option value="all">All Records</option>
                              <option value="record:archived">
                                Archived Records
                              </option>
                            </select>
                          )}
                          {label === "Brand" && (
                            <select
                              aria-label="Brand Filter"
                              className={controlClass}
                              value={brand}
                              onChange={(e) => {
                                setBrand(e.target.value);
                                setPage(1);
                              }}
                            >
                              <option value="">All Brands</option>
                              <option value="Samsara">Samsara</option>
                              <option value="FiveELD">FiveELD</option>
                            </select>
                          )}
                          {label === "Truck / Custody" && (
                            <>
                              <select
                                aria-label="Assignment Filter"
                                className={controlClass}
                                value={assigned}
                                onChange={(e) => {
                                  setAssigned(e.target.value);
                                  setPage(1);
                                }}
                              >
                                <option value="">All Custody</option>
                                <option value="truck">On Truck</option>
                                <option value="storage">In Storage</option>
                                <option value="person">With Person</option>
                                <option value="unknown">Unknown Custody</option>
                                <option value="available">
                                  Available to Assign
                                </option>
                              </select>
                              <select
                                aria-label="Location Filter"
                                className={controlClass}
                                value={location}
                                onChange={(e) => {
                                  setLocation(e.target.value);
                                  setPage(1);
                                }}
                              >
                                <option value="">All Locations</option>
                                {data.locations.map((l) => (
                                  <option key={l.id} value={l.id}>
                                    {inventoryLabel(l)}
                                  </option>
                                ))}
                              </select>
                            </>
                          )}
                          {label === "Condition" && (
                            <select
                              aria-label="Condition Filter"
                              className={controlClass}
                              value={condition}
                              onChange={(e) => {
                                setCondition(e.target.value);
                                setPage(1);
                              }}
                            >
                              <option value="">All Conditions</option>
                              {[
                                "good",
                                "damaged",
                                "repair",
                                "lost",
                                "retired",
                                "unknown",
                              ].map((c) => (
                                <option key={c} value={c}>
                                  {readable(c)}
                                </option>
                              ))}
                            </select>
                          )}
                          {((label === "Condition" &&
                            collection === "assets") ||
                            label === "Carrier Status") && (
                            <select
                              aria-label="Lifecycle Filter"
                              className={controlClass}
                              value={lifecycle}
                              onChange={(e) => {
                                setLifecycle(e.target.value);
                                setPage(1);
                              }}
                            >
                              <option value="">
                                {collection === "lines"
                                  ? "All Carrier Statuses"
                                  : "All Lifecycles"}
                              </option>
                              {(collection === "lines"
                                ? [
                                    "active",
                                    "suspended",
                                    "cancelled",
                                    "pending",
                                    "unknown",
                                  ]
                                : lifecycleStates
                              ).map((c) => (
                                <option key={c} value={c}>
                                  {readable(c)}
                                </option>
                              ))}
                            </select>
                          )}
                          {label === "Installation" && (
                            <select
                              aria-label="Installation Filter"
                              className={controlClass}
                              value={installation}
                              onChange={(e) => {
                                setInstallation(e.target.value);
                                setPage(1);
                              }}
                            >
                              <option value="">All Installation States</option>
                              {[
                                "reported_present",
                                "verified_installed",
                                "missing",
                                "unknown",
                                "not_installed",
                                "unassigned",
                              ].map((c) => (
                                <option key={c} value={c}>
                                  {readable(c)}
                                </option>
                              ))}
                            </select>
                          )}
                          {label === "Storage" && (
                            <input
                              aria-label="Storage Location Filter"
                              className={controlClass}
                              value={location}
                              onChange={(e) => {
                                setLocation(e.target.value);
                                setPage(1);
                              }}
                            />
                          )}
                        </ColumnFilter>
                      )}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {(directoryMode
                  ? filtered
                  : filtered.slice(
                      (currentPage - 1) * pageSize,
                      currentPage * pageSize,
                    )
                ).map((record) => (
                  <tr
                    key={record.id}
                    className="border-t border-zinc-800/60 hover:bg-zinc-800/20"
                  >
                    {columns(record).map((value, index) => (
                      <td key={index} className={td}>
                        {index === 0 ? (
                          <button
                            className="text-left font-medium text-blue-400 hover:underline"
                            onClick={() => setDetail({ collection, record })}
                          >
                            {value || inventoryLabel(record)}
                            {!record.data.active && (
                              <span className="ml-2 text-zinc-500">
                                Archived
                              </span>
                            )}
                          </button>
                        ) : (
                          value || "—"
                        )}
                      </td>
                    ))}
                    <td className={td}>
                      {canManage && (
                        <div className="flex items-center gap-2">
                          <button
                            className="text-blue-400"
                            onClick={() => void openFull(collection, record)}
                          >
                            Edit
                          </button>
                          {collection === "assets" && canTrucks && (
                            <select
                              aria-label={
                                "Actions for " + inventoryLabel(record)
                              }
                              className="w-20 rounded bg-transparent text-xs text-zinc-500"
                              value=""
                              onChange={(e) => {
                                if (e.target.value)
                                  void openFull(
                                    collection,
                                    record,
                                    e.target.value as DeviceAction,
                                  );
                              }}
                            >
                              <option value="">More</option>
                              <option value="transfer">Transfer</option>
                              <option value="return">Return to storage</option>
                              <option value="replace">Replace</option>
                              {effectiveTruck(record, data) &&
                                !unavailable(record.data) && (
                                  <option value="verify">
                                    Verify installation
                                  </option>
                                )}
                            </select>
                          )}
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
                {!loading && !filtered.length && (
                  <tr>
                    <td
                      colSpan={headers.length}
                      className="p-10 text-center text-sm text-zinc-500"
                    >
                      {search ||
                      status !== "record:active" ||
                      assigned ||
                      location ||
                      brand
                        ? "No matching records."
                        : "No records yet."}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </TableShell>
          <TablePagination
            page={currentPage}
            pageSize={pageSize}
            totalItems={totalItems}
            totalPages={totalPages}
            onPageChange={setPage}
            onPageSizeChange={(value) => {
              setPageSize(value);
              setPage(1);
            }}
          />
        </>
      )}
      {editor && (
        <InventoryEditor
          key={editor.collection + ":" + editor.record.id}
          collection={editor.collection}
          record={editor.record}
          context={dialogContext || context}
          onClose={() => {
            setEditor(null);
            setDialogContext(null);
          }}
          onSaved={saved}
        />
      )}
      {assigning && (
        <Modal
          title="Assign Existing Device"
          isSaving={false}
          submitLabel="Continue"
          submitDisabled={!chosenAsset || !chosenTruck}
          onClose={() => {
            setAssigning(false);
            setDialogContext(null);
          }}
          onSubmit={(event) => {
            event.preventDefault();
            const asset = assignmentData.assets.find(
              (item) => item.id === chosenAsset,
            );
            if (!asset || !chosenTruck) return;
            setEditor({
              collection: "assets",
              record: {
                ...asset,
                data: {
                  ...asset.data,
                  truckId: chosenTruck,
                  locationId: "",
                  custodian: "",
                  custodianId: "",
                  gatewayId: "",
                  equipmentState: "unknown",
                },
              },
            });
            setAssigning(false);
          }}
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Device">
              <select
                aria-label="Device"
                className={controlClass}
                value={chosenAsset}
                onChange={(event) => setChosenAsset(event.target.value)}
              >
                <option value="">Select a device…</option>
                {assignable.map((asset) => (
                  <option key={asset.id} value={asset.id}>
                    {[
                      readable(asset.data.deviceType),
                      asset.data.brand || asset.data.make,
                      inventoryLabel(asset),
                    ]
                      .filter(Boolean)
                      .join(" · ")}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Truck">
              <select
                aria-label="Truck"
                className={controlClass}
                value={chosenTruck}
                onChange={(event) => setChosenTruck(event.target.value)}
              >
                <option value="">Select a truck…</option>
                {trucks.map((truck) => (
                  <option key={truck.id} value={truck.id}>
                    {truck.unitNumber}
                  </option>
                ))}
              </select>
            </Field>
          </div>
        </Modal>
      )}
      {detail && (
        <InventoryDetail
          key={detail.collection + detail.record.id}
          collection={detail.collection}
          record={detail.record}
          context={context}
          canTrucks={canTrucks}
          onClose={() => setDetail(null)}
          onEdit={
            canManage
              ? () => {
                  void openFull(detail.collection, detail.record);
                  setDetail(null);
                }
              : undefined
          }
        />
      )}
      {operation && (
        <InventoryOperation
          key={operation.record.id + operation.action}
          record={operation.record}
          action={operation.action}
          context={dialogContext || context}
          onClose={() => {
            setOperation(null);
            setDialogContext(null);
          }}
          onSaved={saved}
        />
      )}
    </div>
  );
}

function TruckInventorySummary({
  truckId,
  context,
}: {
  truckId: string;
  context: InventoryContext;
}) {
  const canTolls = usePermissions().includes("tolls.read");
  const [prepass, setPrepass] = useState<PrePassFleetList | null>(null),
    [error, setError] = useState("");
  useEffect(() => {
    if (!canTolls) return;
    const controller = new AbortController();
    void fetchPrePassFleet(controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) setPrepass(value);
      })
      .catch((reason) => {
        if (!controller.signal.aborted) setError(reason.message);
      });
    return () => controller.abort();
  }, [canTolls]);
  const assets = context.data.assets.filter(
    (asset) =>
      asset.data.active &&
      visibleDevice(asset.data) &&
      effectiveTruck(asset, context.data) === truckId,
  );
  const gateway = assets.find(
      (asset) => asset.data.deviceType === "samsara_gateway",
    ),
    tablet = assets.find((asset) => asset.data.deviceType === "tablet");
  const line = context.data.lines.find(
    (item) => item.data.active && item.data.assetId === tablet?.id,
  );
  const cameras = assets.filter((asset) => asset.data.deviceType === "dashcam");
  const tag = prepass?.vehicles.find((item) => item.truckId === truckId)
    ?.vehicle.transponderNumber;
  const items = [
    ["Samsara Gateway", gateway?.data.serial],
    [
      "Dashcams",
      cameras
        .map((camera) =>
          [camera.data.brand, camera.data.serial || camera.data.imei]
            .filter(Boolean)
            .join(" · "),
        )
        .join(", "),
    ],
    [
      "Tablet IMEI / Number",
      [tablet?.data.imei, inventoryPhone(line?.data.wirelessNumber)]
        .filter(Boolean)
        .join(" · "),
    ],
    ...(canTolls
      ? [["PrePass Device", error ? "Roster Unavailable" : tag || "Not Linked"]]
      : []),
  ];
  return (
    <section className="space-y-3">
      <dl className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        {items.map(([label, value]) => (
          <div
            key={label}
            className="rounded-xl border border-zinc-800 bg-zinc-900/30 p-4"
          >
            <dt className="text-xs text-zinc-500">{label}</dt>
            <dd className="mt-2 break-words text-sm text-zinc-200">
              {value || "Not Recorded"}
            </dd>
          </div>
        ))}
      </dl>
    </section>
  );
}
