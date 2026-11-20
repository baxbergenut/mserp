type Board = "status" | "gross";
type Operation = "open" | "save" | "load";
const samples: string[] = [];
let firstRoute = true;
let directBoard: Board | undefined;

// Measure a direct visit from the document's time origin. A route change,
// background tab or restored document must never inherit that start time.
export function trackBoardDocument(path: string) {
  if (!firstRoute) return;
  firstRoute = false;
  const board = path === "/driver-board" ? "status" : path === "/gross-board" ? "gross" : undefined;
  if (!board || document.hidden) return;
  const navigation = performance.getEntriesByType("navigation")[0] as PerformanceNavigationTiming | undefined;
  if (!navigation || navigation.name !== location.href ||
      (navigation.type !== "navigate" && navigation.type !== "reload")) return;
  directBoard = board;
  const clear = () => { directBoard = undefined; };
  document.addEventListener("visibilitychange", clear, { once: true });
  window.addEventListener("pagehide", clear, { once: true });
  return () => {
    clear();
    document.removeEventListener("visibilitychange", clear);
    window.removeEventListener("pagehide", clear);
  };
}

// Only pair an opening with a single completed snapshot fetch inside its window.
function openingNetworkTiming(board: Board, start: number, received: number) {
  const endpoint = board === "status" ? "/driver-board" : "/gross-board";
  const snapshots = (performance.getEntriesByType("resource") as PerformanceResourceTiming[]).filter(entry =>
    entry.initiatorType === "fetch" && entry.startTime >= start && entry.responseEnd > 0 &&
    entry.responseEnd <= received && entry.name.split("?", 1)[0].endsWith(endpoint) && entry.name.includes("weekStart="));
  if (snapshots.length !== 1) return "";
  const entry = snapshots[0];
  const app = entry.serverTiming?.find(metric => metric.name === "app")?.duration;
  if (app === undefined || !Number.isFinite(app) || entry.requestStart < entry.startTime ||
      entry.responseStart < entry.requestStart || entry.responseEnd < entry.responseStart) return "";
  const points = [entry.startTime, entry.requestStart, entry.responseStart, entry.responseEnd].map(t => Math.round(t - start));
  return "~" + [...points, Math.round(app), entry.encodedBodySize].join("/");
}

// Called after React commits the acknowledged rows. Allow a paint before
// measuring, and omit hidden tabs where animation frames can be suspended.
export function recordBoardPaint(board: Board, operation: Operation, start: number, sent: number, received: number, completedEdit = false) {
  if (!start || document.hidden) return;
  const commit = Math.round(performance.now() - start);
  requestAnimationFrame(() => {
    const frame = Math.round(performance.now() - start);
    requestAnimationFrame(() => {
      if (document.hidden) return;
      const ms = Math.round(performance.now() - start);
      if (ms < 0 || ms > 120000) return;
      const before = Math.round(sent - start), response = Math.round(received - start);
      const phases = before >= 0 && before <= response && response <= commit ? `/${before}/${response}/${commit}/${frame}` : "";
      let detail = completedEdit && operation === "save" ? "~complete" : "";
      if (phases && operation === "open") detail = openingNetworkTiming(board, start, received);
      samples.push(`${board}-${operation}=${ms}${phases}${detail}`);
      if (operation === "open" && directBoard === board) {
        const documentMS = Math.round(performance.now());
        directBoard = undefined;
        if (documentMS <= 120000) samples.push(`${board}-document-open=${documentMS}`);
      }
      if (samples.length > 12) samples.splice(0, samples.length - 12);
    });
  });
}

// The next normal API request carries the samples. No telemetry-only request.
export function takeBoardTimingHeader(): Record<string,string> {
  const build = process.env.NEXT_PUBLIC_BOARD_BUILD;
  const prefix = build ? `build=${build},` : "";
  while (samples.length > (build ? 11 : 12) || prefix.length + samples.join(",").length > 512) samples.shift();
  return samples.length ? { "X-Clustie-Board-Timing": prefix + samples.splice(0).join(",") } : {};
}
