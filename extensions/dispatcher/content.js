(() => {
  const A = DispatchWatchAdapter;
  let host, root, running = false, generation = 0, timer, settings = DispatchWatch.defaults;
  let original = [], promotedBody, seen = new Set(), matches = new Map(), cycleIDs = new Set();
  let firstPass = true, page = 1, lastRequest = 0, abortWait;
  let latestResponse, checkWait;
  window.addEventListener("message", event => {
    if (event.source !== window || event.origin !== location.origin || event.data?.source !== "mserp-dispatch-response-v1") return;
    const value = event.data;
    if (!Number.isSafeInteger(value.id) || !Number.isFinite(value.startedAt) || (latestResponse && value.id <= latestResponse.id)) return;
    latestResponse = value;
    checkWait?.();
  });
  async function send(message) { const r = await chrome.runtime.sendMessage(message); if (!r?.ok) throw new Error(r?.error || "Extension connection lost. Reload the page."); return r; }
  const status = text => { if (root) root.querySelector("#status").textContent = text; };
  function restore() {
    if (promotedBody?.isConnected) for (const row of original) if (row.parentElement === promotedBody) promotedBody.append(row);
    original = []; promotedBody = null;
    document.querySelectorAll(A.rowSelector).forEach(row => row.classList.remove("mserp-watch-match", "mserp-watch-gold", "mserp-watch-green", "mserp-watch-blue", "mserp-watch-hidden"));
  }
  function stop(reason = "Paused", preserveHighlights = false) {
    running = false; generation++; clearTimeout(timer); abortWait?.(); if (!preserveHighlights) restore();
    if (root) root.querySelector("#play").textContent = "▶ Play";
    status(reason); void send({ type: "stop" }).catch(() => {});
  }
  function renderMatches() {
    const list = root.querySelector("#matches"); list.replaceChildren();
    for (const l of [...matches.values()].slice(0, 100)) {
      const item = document.createElement("div"); item.className = "match";
      item.textContent = `${l.id} · ${l.origin} → ${l.destination} · ${l.price === null ? "No price" : "$" + l.price.toLocaleString()} · ${l.rpm === null ? "—" : "$" + l.rpm + "/mi"} · page ${l.page}`;
      list.append(item);
    }
    root.querySelector("#count").textContent = `${matches.size} matching loads`;
  }
  async function inspect() {
    restore(); const entries = A.rows();
    const response = DispatchWatch.fromResponse(latestResponse?.data);
    page = response.page + 1;
    const loads = new Map(response.jobs.map(j => [j.id, j]));
    if (entries.length !== loads.size || entries.some(e => !loads.has(e.id))) throw new Error("The load response and displayed rows disagree. Refresh the search before restarting.");
    let fresh = 0;
    for (const { row, id } of entries) {
      const load = loads.get(id);
      cycleIDs.add(load.id);
      const match = DispatchWatch.matches(load, settings);
      if (match) {
        matches.set(load.id, { ...load, page });
        row.classList.add("mserp-watch-match", `mserp-watch-${settings.highlight}`);
        const key = DispatchWatch.signature(load, settings);
        if (!seen.has(key)) { fresh++; seen.add(key); }
      } else { matches.delete(load.id); if (settings.hideOthers) row.classList.add("mserp-watch-hidden"); }
    }
    // Keep Angular's original nodes/event listeners; restore order before any site action.
    if (settings.promote && entries.length) {
      promotedBody = entries[0].row.parentElement; original = [...promotedBody.children];
      for (const row of [...original].sort((a,b) => Number(b.classList.contains("mserp-watch-match")) - Number(a.classList.contains("mserp-watch-match")))) promotedBody.append(row);
    }
    if (seen.size > 10000) seen = new Set([...seen].slice(-5000));
    renderMatches();
    if (fresh && (!firstPass || settings.alertExisting)) {
      try { await DispatchWatchSound.play(settings.sound, settings.volume); } catch(e) { status(e.message); }
      if (settings.pauseOnMatch) { stop(`${fresh} new match${fresh === 1 ? "" : "es"} found — paused`, true); return; }
    }
  }
  function waitForResults(action) {
    return new Promise((resolve, reject) => {
      let quietTimer;
      const since = Date.now(), baseline = latestResponse?.id || 0;
      const cleanup = () => { observer.disconnect(); clearTimeout(timeout); clearTimeout(quietTimer); abortWait = undefined; checkWait = undefined; };
      const finish = () => {
        if (!latestResponse || latestResponse.id <= baseline || latestResponse.startedAt < since) return;
        if (latestResponse.error) { cleanup(); reject(new Error(latestResponse.error)); return; }
        if (!A.loading()) {
          try {
            const data = DispatchWatch.fromResponse(latestResponse.data);
            const ids = new Set(data.jobs.map(j => j.id)), entries = A.rows();
            if (entries.length !== ids.size || entries.some(e => !ids.has(e.id))) return;
            cleanup(); resolve();
          } catch (e) { cleanup(); reject(e); }
        }
      };
      checkWait = () => { clearTimeout(quietTimer); quietTimer = setTimeout(finish, 350); };
      const observer = new MutationObserver(checkWait);
      observer.observe(document.querySelector("main") || document.body, { childList: true, subtree: true, characterData: true, attributes: true });
      const timeout = setTimeout(() => { cleanup(); reject(new Error("No matching load-search response within 30 seconds. Reload J.B. Hunt after installing the extension, then retry.")); }, 30000);
      abortWait = () => { cleanup(); reject(new Error("Paused")); };
      action.click();
    });
  }
  async function tick(g) {
    if (!running || g !== generation) return;
    const started = Date.now();
    try {
      if (!location.pathname.startsWith("/carrier/ngx/load-board/grid")) throw new Error("Open the Find Loads grid to monitor.");
      if (A.blocked()) throw new Error("Dialog or site error open — paused. Close it, then press Play.");
      const access = await send({ type: "poll" });
      if (!running || g !== generation) return;
      settings = access.settings;
      if (Date.now() - lastRequest < settings.interval * 1000) { timer = setTimeout(() => void tick(g), settings.interval * 1000 - (Date.now() - lastRequest)); return; }
      if (A.loading()) throw new Error("The site is still loading. Wait for it to finish, then press Play.");
      restore();
      let action;
      const pagination = DispatchWatch.fromResponse(latestResponse?.data);
      if (settings.allPages && pagination.page + 1 < pagination.totalPages) {
        action = A.next(); page++;
        if (!action) throw new Error("More result pages exist but Next page is unavailable. Monitoring stopped.");
      }
      else {
        // Completed scans remove unavailable loads. No old match survives forever.
        for (const id of matches.keys()) if (!cycleIDs.has(id)) matches.delete(id);
        cycleIDs = new Set(); firstPass = false; page = 1;
        action = settings.allPages && pagination.page > 0 ? A.first() : A.refresh();
      }
      if (!action) throw new Error("Run a J.B. Hunt search first. No refresh control was found.");
      lastRequest = Date.now(); status(`Checking page ${page}…`);
      await waitForResults(action);
      if (!running || g !== generation) return;
      if (A.blocked()) throw new Error("The site reported an error or opened a dialog. Monitoring paused.");
      await inspect();
      if (!running || g !== generation) return;
      status(`Running · page ${page} · checked ${new Date().toLocaleTimeString()}${document.hidden ? " · background timing may be delayed" : ""}`);
      timer = setTimeout(() => void tick(g), Math.max(0, settings.interval * 1000 - (Date.now() - started)));
    } catch (e) { if (g === generation) stop(e.message || "Monitoring stopped"); }
  }
  async function start() {
    if (running) { stop(); return; }
    const g = ++generation; running = true; root.querySelector("#play").textContent = "Ⅱ Pause"; status("Checking dispatcher access…");
    // Called directly by a real page click, which unlocks Web Audio.
    void DispatchWatchSound.unlock().catch(() => {});
    try {
      const access = await send({ type: "start" });
      if (g !== generation) return;
      if (!A.refresh()) throw new Error("Run a search in the grid first, then press Play.");
      if (A.loading() || A.blocked()) throw new Error("Wait for loading to finish and close open dialogs, then press Play.");
      settings = access.settings; seen = new Set(); matches = new Map(); cycleIDs = new Set(); firstPass = true;
      // Always require a fresh successful GraphQL response after authorization.
      await new Promise(resolve => setTimeout(resolve, Math.max(0, settings.interval * 1000 - (Date.now() - lastRequest))));
      if (g !== generation) return;
      lastRequest = Date.now();
      await waitForResults(settings.allPages && A.first() ? A.first() : A.refresh());
      if (g !== generation) return;
      page = 1;
      await inspect();
      if (!running || g !== generation) return;
      status(`Running for ${access.dispatcherName} · ${settings.interval}s between checks`);
      lastRequest = Date.now(); timer = setTimeout(() => void tick(g), settings.interval * 1000);
    } catch(e) { if (g === generation) stop(e.message); }
  }
  function mount() {
    if (!location.pathname.startsWith("/carrier/ngx/load-board/grid")) { if (running) stop("Paused outside the load grid"); host?.remove(); return; }
    if (host?.isConnected) return;
    const table = document.querySelector('main table'); if (!table) return;
    host = document.createElement("div"); host.id = "mserp-dispatch-watch"; root = host.attachShadow({ mode: "open" });
    root.innerHTML = `<style>:host{display:block;margin:12px 0;font:13px/1.5 system-ui;color:#e4e4e7}.bar{background:#111113;border:1px solid #3f3f46;border-radius:8px;padding:12px 16px}.top{display:flex;align-items:center;gap:12px;flex-wrap:wrap}strong{font-size:13px}button{font:inherit;min-height:32px;border-radius:8px;padding:4px 12px;border:1px solid #52525b;background:#27272a;color:white;cursor:pointer}#play{background:#2563eb;border-color:#2563eb}button:focus-visible,summary:focus-visible{outline:2px solid #93c5fd;outline-offset:2px}#status,.hint{font-size:12px;color:#a1a1aa;margin:6px 0 0}details{margin-top:8px}summary{cursor:pointer}.match{padding:8px;border-top:1px solid #3f3f46;color:#fef3c7}#matches{max-height:220px;overflow:auto}.hint{margin-top:8px}</style><div class="bar"><div class="top"><strong>MS Express · Dispatch Watch</strong><button id="play">▶ Play</button><button id="settings">Settings</button><span id="count">0 matching loads</span></div><p id="status" role="status" aria-live="polite">Paused · run a search, then press Play</p><details><summary>Matching loads from this scan</summary><div id="matches"></div><p class="hint">Matches from other pages reflect their last check. Pause and use J.B. Hunt’s page controls to review or book. Booking stays manual.</p></details></div>`;
    table.closest('p-table')?.before(host);
    if (!host.isConnected) table.before(host);
    root.querySelector("#play").onclick = () => void start();
    root.querySelector("#settings").onclick = () => void send({ type: "options" }).catch(e => status(e.message));
  }
  // Restore nodes before native Angular handlers execute. User changes pause
  // monitoring so refresh cannot race a booking, sort, filter edit or pagination.
  document.addEventListener("click", e => { if (e.isTrusted && !e.composedPath().includes(host)) { if (running) stop("Paused for site interaction. Press Play when ready."); else restore(); } }, true);
  document.addEventListener("keydown", e => { if (e.isTrusted && running && !e.composedPath().includes(host)) stop("Paused for site interaction."); }, true);
  window.addEventListener("pagehide", () => stop());
  new MutationObserver(mount).observe(document.body, { childList: true, subtree: true });
  mount();
})();
