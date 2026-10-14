(() => {
  const A = DispatchWatchAdapter;
  let host, root, running = false, generation = 0, timer, settings = DispatchWatch.defaults;
  let original = [], promotedBody, tracker = DispatchWatch.createTracker();
  let page = 1, lastRequest = 0, abortWait, latestResponse, checkWait, decorateTimer, soundWarning = "";
  window.addEventListener("message", event => {
    if (event.source !== window || event.origin !== location.origin || event.data?.source !== "mserp-dispatch-response-v1") return;
    const value = event.data;
    if (!Number.isSafeInteger(value.id) || !Number.isFinite(value.startedAt) || (latestResponse && value.id <= latestResponse.id)) return;
    latestResponse = value;
    checkWait?.();
    scheduleDecorate();
  });
  async function send(message) {
    const r = await chrome.runtime.sendMessage(message);
    if (!r?.ok) throw new Error(r?.error || "Extension connection lost. Reload the page.");
    return r;
  }
  function status(text) {
    if (!root) return;
    root.querySelector("#status").textContent = text;
    root.querySelector("#status").title = text;
  }
  function restore() {
    if (promotedBody?.isConnected) for (const row of original) if (row.parentElement === promotedBody) promotedBody.append(row);
    original = []; promotedBody = null;
    document.querySelectorAll(A.rowSelector).forEach(row => {
      for (const name of ["mserp-watch-match", "mserp-watch-gold", "mserp-watch-green", "mserp-watch-blue"]) if (row.classList.contains(name)) row.classList.remove(name);
    });
  }
  function stop(reason = "Paused") {
    running = false; generation++; clearTimeout(timer); abortWait?.();
    if (root) root.querySelector("#play").textContent = "▶ Play";
    status(reason); void send({ type: "stop" }).catch(() => {});
  }
  function renderNewLoads() {
    if (!root) return;
    const recent = tracker.recent, list = root.querySelector("#loads");
    list.replaceChildren();
    for (const l of recent.slice(0, 100)) {
      const item = document.createElement("div"); item.className = "load";
      item.textContent = `${l.id} · ${l.origin} → ${l.destination} · ${l.price === null ? "No price" : "$" + l.price.toLocaleString()} · ${l.rpm === null ? "—" : "$" + l.rpm.toFixed(2) + "/mi"}`;
      const detail = document.createElement("p"); detail.className = "hint";
      const presence = l.presence === "missing" ? "No longer in results — booking status unknown" : l.presence === "unavailable" ? "Reported unavailable in the last response" : `Seen on page ${l.page}`;
      detail.textContent = `${presence} · last seen ${new Date(l.lastSeenAt).toLocaleTimeString()}`;
      item.append(detail);
      list.append(item);
    }
    if (!recent.length) list.textContent = tracker.ready ? "No new loads since the baseline." : "The first complete scan is a silent baseline.";
    root.querySelector("#new").textContent = `New loads (${recent.length})`;
    root.querySelector("#history").textContent = `${tracker.ready ? "Baseline ready" : "Building baseline"} · ${tracker.seenCount} IDs seen this run${recent.length > 100 ? " · showing the latest 100" : ""}`;
  }
  // Framework rerenders may replace rows after the request has settled, and
  // users may pause or change pages to inspect a discovery. Decoration is based
  // on retained IDs, independent of whether this response contains fresh alerts.
  function decorate() {
    if (abortWait || A.loading() || !location.pathname.startsWith("/carrier/ngx/load-board/grid")) return;
    let response;
    try { response = DispatchWatch.fromResponse(latestResponse?.data); } catch { return; }
    const entries = A.rows(), loads = new Map(response.jobs.map(l => [l.id, l]));
    if (entries.length !== loads.size || entries.some(e => !loads.has(e.id))) return;
    const ranks = new Map(tracker.recent.map(l => [l.id, l.order]));
    for (const { row, id } of entries) {
      const active = ranks.has(id) && loads.get(id).available;
      for (const name of ["mserp-watch-match", "mserp-watch-gold", "mserp-watch-green", "mserp-watch-blue"]) {
        const wanted = active && (name === "mserp-watch-match" || name === `mserp-watch-${settings.highlight}`);
        if (row.classList.contains(name) !== wanted) row.classList.toggle(name, wanted);
      }
    }
    if (!entries.some(e => ranks.has(e.id) && loads.get(e.id).available)) return;
    const body = entries[0].row.parentElement;
    if (promotedBody !== body || original.length !== body.children.length || original.some(row => row.parentElement !== body)) {
      promotedBody = body; original = [...body.children];
    }
    const rowRanks = new Map(entries.map(e => [e.row, loads.get(e.id).available ? ranks.get(e.id) || 0 : 0]));
    const desired = [...original].sort((a,b) => (rowRanks.get(b) || 0) - (rowRanks.get(a) || 0));
    // Avoid changing the DOM when already correct, so observing our own changes
    // cannot cause a mutation loop. Only original native row nodes are moved.
    desired.forEach((row, index) => { if (body.children[index] !== row) body.insertBefore(row, body.children[index] || null); });
  }
  function scheduleDecorate() {
    clearTimeout(decorateTimer);
    decorateTimer = setTimeout(decorate, 50);
  }
  async function inspect() {
    restore();
    const entries = A.rows(), response = DispatchWatch.fromResponse(latestResponse?.data);
    page = response.page + 1;
    const loads = new Map(response.jobs.map(j => [j.id, j]));
    if (entries.length !== loads.size || entries.some(e => !loads.has(e.id))) throw new Error("The load response and displayed rows disagree. Refresh the search before restarting.");
    const { fresh } = tracker.observe(response);
    decorate();
    renderNewLoads();
    if (fresh.length) {
      root.querySelector("#last-new").textContent = `Last notification: ${fresh.length} new load${fresh.length === 1 ? "" : "s"} at ${new Date().toLocaleTimeString()}`;
      // Chrome may suspend audio in a hidden tab. Audio must never hold up scanning.
      const g = generation;
      void DispatchWatchSound.play(settings.sound, settings.volume).then(() => {
        if (g === generation) soundWarning = "";
      }, e => {
        if (g === generation) soundWarning = e.message || "Sound unavailable. Check browser audio.";
      });
    }
  }
  function checkedStatus() {
    const response = DispatchWatch.fromResponse(latestResponse?.data);
    status(`${tracker.ready ? "Running" : "Baseline"} · page ${page}/${Math.max(1, response.totalPages)} · ${settings.interval}s${document.hidden ? " · background timing may be delayed" : ""}${soundWarning ? " · " + soundWarning : ""}`);
  }
  function waitForResults(action) {
    return new Promise((resolve, reject) => {
      let timeout, deadline, settled = false;
      const since = Date.now(), baseline = latestResponse?.id || 0;
      const cleanup = () => {
        settled = true; observer.disconnect(); clearTimeout(timeout);
        document.removeEventListener("visibilitychange", resume);
        document.removeEventListener("resume", resume);
        abortWait = undefined; checkWait = undefined;
      };
      const finish = () => {
        if (settled) return;
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
      // Inspect on response/DOM events, not a debounce timer that Chrome can
      // postpone beyond the timeout in a background tab.
      checkWait = finish;
      const observer = new MutationObserver(checkWait);
      observer.observe(document.querySelector("main") || document.body, { childList: true, subtree: true, characterData: true, attributes: true });
      const arm = () => {
        clearTimeout(timeout); deadline = Date.now() + 30000;
        timeout = setTimeout(expire, 30000);
      };
      const expire = () => {
        finish();
        if (settled) return;
        if (document.hidden) {
          status("Waiting for background search — Chrome may delay this tab");
          return; // Keep this request; never issue an overlapping refresh.
        }
        // A frozen renderer/computer sleep can deliver an overdue timer before
        // queued network and DOM tasks. Allow those tasks a fresh foreground window.
        if (Date.now() > deadline + 2000) { arm(); return; }
        cleanup(); reject(new Error("No matching load-search response after 30 seconds in the foreground. Refresh J.B. Hunt, then press Play."));
      };
      function resume() {
        finish();
        if (!settled && !document.hidden) arm();
      }
      document.addEventListener("visibilitychange", resume);
      document.addEventListener("resume", resume);
      arm();
      abortWait = () => { cleanup(); reject(new Error("Paused")); };
      try { action.click(); } catch(e) { cleanup(); reject(e); }
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
      const pagination = DispatchWatch.fromResponse(latestResponse?.data);
      const more = pagination.page + 1 < pagination.totalPages;
      const action = more ? A.next() : pagination.page > 0 ? A.first() : A.refresh();
      if (!action) throw new Error("The search page controls changed. Run a search and press Play again.");
      page = more ? pagination.page + 2 : 1;
      lastRequest = Date.now(); status(`${tracker.ready ? "Checking" : "Baseline"} page ${page}…`);
      await waitForResults(action);
      if (!running || g !== generation) return;
      if (A.blocked()) throw new Error("The site reported an error or opened a dialog. Monitoring paused.");
      await inspect();
      if (!running || g !== generation) return;
      checkedStatus();
      timer = setTimeout(() => void tick(g), Math.max(0, settings.interval * 1000 - (Date.now() - started)));
    } catch (e) { if (g === generation) stop(e.message || "Monitoring stopped"); }
  }
  async function start() {
    if (running) { stop(); return; }
    const g = ++generation; running = true; root.querySelector("#play").textContent = "Ⅱ Pause"; status("Checking team access…");
    void DispatchWatchSound.unlock().catch(() => {});
    try {
      const access = await send({ type: "start" });
      if (g !== generation) return;
      if (!A.refresh()) throw new Error("Run a search in the grid first, then press Play.");
      if (A.loading() || A.blocked()) throw new Error("Wait for loading to finish and close open dialogs, then press Play.");
      settings = access.settings; tracker = DispatchWatch.createTracker(); soundWarning = "";
      root.querySelector("#last-new").textContent = ""; renderNewLoads();
      await new Promise(resolve => setTimeout(resolve, Math.max(0, settings.interval * 1000 - (Date.now() - lastRequest))));
      if (g !== generation) return;
      restore(); lastRequest = Date.now(); status("Baseline page 1…");
      await waitForResults(A.first() || A.refresh());
      if (g !== generation) return;
      await inspect();
      if (!running || g !== generation) return;
      checkedStatus();
      timer = setTimeout(() => void tick(g), settings.interval * 1000);
    } catch(e) { if (g === generation) stop(e.message); }
  }
  function mount() {
    if (!location.pathname.startsWith("/carrier/ngx/load-board/grid")) { if (running) stop("Paused outside the load grid"); restore(); host?.remove(); return; }
    if (host?.isConnected) return;
    const controls = A.controls();
    if (!controls) return;
    // Angular rebuilds the paginator during ordinary refreshes. Move the same
    // host back into it so handlers, scan history and in-flight waits survive.
    if (host) { controls.append(host); return; }
    host = document.createElement("div"); host.id = "mserp-dispatch-watch"; root = host.attachShadow({ mode: "open" });
    root.innerHTML = `<style>
      :host{display:inline-flex;max-width:100%;margin-left:8px;font:12px/1.4 system-ui;color:#27272a;vertical-align:middle}
      .toolbar{display:flex;align-items:center;gap:6px;flex-wrap:wrap;max-width:100%}
      button{font:inherit;min-height:32px;border-radius:6px;padding:4px 10px;border:1px solid #d4d4d8;background:#fff;color:#27272a;cursor:pointer;white-space:nowrap}
      #play{background:#2563eb;border-color:#2563eb;color:white}button:focus-visible{outline:2px solid #60a5fa;outline-offset:2px}
      #status{font-size:11px;color:#52525b;max-width:180px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
      #new-loads{inset:auto;box-sizing:border-box;width:min(560px,calc(100vw - 24px));max-height:min(400px,calc(100dvh - 24px));overflow:auto;border:1px solid #52525b;border-radius:8px;padding:16px;background:#18181b;color:#f4f4f5;box-shadow:0 8px 32px #0005}
      #new-loads h2{font-size:14px;margin:0 0 8px}.hint{font-size:12px;color:#a1a1aa;margin:8px 0}.load{padding:8px 0;border-top:1px solid #3f3f46}
    </style><div class="toolbar" aria-label="Dispatch Watch auto-refresh"><button type="button" id="play">▶ Play</button><button type="button" id="settings">Settings</button><button type="button" id="new" aria-controls="new-loads" aria-expanded="false">New loads (0)</button><span id="status" role="status" aria-live="polite">Paused</span></div>
    <div id="new-loads" popover="auto" role="region" aria-label="New loads found during this run"><h2>New loads since Play</h2><p class="hint" id="history"></p><p class="hint" id="last-new"></p><div id="loads"></div><p class="hint">Discoveries stay here until the next Play or reload. Missing from results does not confirm booking. Highlighted rows stay at the top of their page, including while paused.</p></div>`;
    controls.append(host);
    root.querySelector("#play").onclick = () => void start();
    root.querySelector("#settings").onclick = () => void send({ type: "options" }).catch(e => status(e.message));
    const panel = root.querySelector("#new-loads"), button = root.querySelector("#new");
    button.onclick = () => {
      if (panel.matches(":popover-open")) panel.hidePopover();
      else {
        const bounds = button.getBoundingClientRect();
        panel.style.bottom = Math.max(12, innerHeight - bounds.top + 8) + "px";
        panel.style.right = Math.max(12, innerWidth - bounds.right) + "px";
        panel.showPopover();
      }
    };
    panel.addEventListener("toggle", () => button.setAttribute("aria-expanded", String(panel.matches(":popover-open"))));
    renderNewLoads();
  }
  // Restore nodes before native site handlers and pause on user edits.
  for (const type of ["click", "keydown", "input", "change"]) document.addEventListener(type, e => {
    // Modifier keydown arrives before Chrome consumes Ctrl+Tab / Alt+Tab.
    // Switching tabs is not an edit to the site's search.
    if (type === "keydown" && (["Control", "Alt", "Meta", "Shift"].includes(e.key) ||
      (e.key === "Tab" && (e.ctrlKey || e.altKey || e.metaKey)))) return;
    if (e.isTrusted && !e.composedPath().includes(host)) {
      if (running) stop("Paused for site interaction. Play starts a new baseline.");
      restore();
      scheduleDecorate();
    }
  }, true);
  window.addEventListener("pagehide", () => stop());
  new MutationObserver(() => { mount(); scheduleDecorate(); }).observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["class"] });
  mount();
})();
