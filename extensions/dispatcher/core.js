/* Shared settings, response validation and per-run new-load tracking. */
globalThis.DispatchWatch = (() => {
  const defaults = Object.freeze({ interval: 5, sound: "chime", volume: 65, highlight: "gold" });
  function normalize(input = {}) {
    // Ignore legacy match filters and price-change alert settings.
    return { interval: Math.max(4, Math.min(3600, Number(input.interval) || 5)),
      volume: Math.max(0, Math.min(100, input.volume === undefined ? 65 : Number(input.volume) || 0)),
      sound: ["chime", "bell", "radar", "silent"].includes(input.sound) ? input.sound : defaults.sound,
      highlight: ["gold", "green", "blue"].includes(input.highlight) ? input.highlight : defaults.highlight };
  }
  function fromJob(j) {
    const price = v => typeof v === "number" && Number.isFinite(v) && v > 0 ? v : null;
    const place = l => [l?.city, l?.state].filter(Boolean).join(", ") + (l?.zipcode ? " " + l.zipcode : "");
    if (!j || typeof j.jobIdentifier !== "string" || !/^[A-Z0-9]{5,20}$/.test(j.jobIdentifier)) throw new Error("Unknown load identifier format");
    return { id: j.jobIdentifier, origin: place(j.firstPickup?.location), destination: place(j.lastDelivery?.location),
      price: price(j.bookNowPrice), rpm: price(j.pricePerMile), available: j.loadboardStatus === "AVAILABLE" };
  }
  function fromResponse(data) {
    if (!data || !Array.isArray(data.jobs) || data.jobs.length > 1000 || !Number.isInteger(data.page) || data.page < 0 ||
      !Number.isInteger(data.totalPages) || data.totalPages < 0 || data.totalPages > 10000 ||
      !Number.isInteger(data.totalElements) || data.totalElements < 0 || !Number.isInteger(data.pageSize) || data.pageSize < 1 ||
      (data.totalPages > 0 && data.page >= data.totalPages)) throw new Error("Unknown search pagination format");
    const jobs = data.jobs.map(fromJob);
    if (new Set(jobs.map(j => j.id)).size !== jobs.length) throw new Error("Duplicate load identifiers in response");
    return { ...data, jobs };
  }
  function createTracker(limit = 100000) {
    const seen = new Set(), recent = new Map(), scan = new Set();
    let ready = false, expectedPage = 0, order = 0;
    return {
      get ready() { return ready; },
      get seenCount() { return seen.size; },
      get recent() { return [...recent.values()].sort((a, b) => b.order - a.order); },
      observe(response, now = Date.now()) {
        if (response.page !== expectedPage) throw new Error("Search pages changed unexpectedly. Press Play to establish a new baseline.");
        const fresh = [];
        for (const load of response.jobs) {
          scan.add(load.id);
          if (!seen.has(load.id)) {
            // Stop instead of forgetting IDs and accidentally alerting twice.
            if (seen.size >= limit) throw new Error("New-load history is full. Press Play to establish a fresh baseline.");
            seen.add(load.id);
            if (ready && load.available) {
              fresh.push(load);
              recent.set(load.id, { ...load, page: response.page + 1, order: ++order, foundAt: now });
            }
          }
          if (recent.has(load.id)) recent.set(load.id, { ...recent.get(load.id), ...load,
            page: response.page + 1, lastSeenAt: now, presence: load.available ? "present" : "unavailable" });
        }
        const complete = response.page + 1 >= response.totalPages;
        if (complete) {
          for (const [id, load] of recent) if (!scan.has(id)) recent.set(id, { ...load, presence: "missing" });
          scan.clear(); ready = true; expectedPage = 0;
        } else expectedPage++;
        return { fresh, complete };
      },
    };
  }
  return { defaults, normalize, fromJob, fromResponse, createTracker };
})();
