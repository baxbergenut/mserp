/* Shared pure parsing/filtering functions, also exercised by node tests. */
globalThis.DispatchWatch = (() => {
  const defaults = Object.freeze({ interval: 5, sound: "chime", volume: 65, highlight: "gold", promote: true,
    hideOthers: false, allPages: true, pauseOnMatch: false, alertExisting: true, alertPriceChanges: true,
    minPrice: "", minRPM: "", minMiles: "", maxMiles: "", maxWeight: "", maxDeadhead: "", maxStops: "",
    origin: "", destination: "", equipment: "", pickupFrom: "", pickupTo: "", bookableOnly: false });
  const numericKeys = ["minPrice", "minRPM", "minMiles", "maxMiles", "maxWeight", "maxDeadhead", "maxStops"];
  function normalize(input = {}) {
    const s = { ...defaults };
    for (const key of Object.keys(defaults)) if (input[key] !== undefined) s[key] = input[key];
    s.interval = Math.max(4, Math.min(3600, Number(s.interval) || 5));
    s.volume = Math.max(0, Math.min(100, Number(s.volume) || 0));
    for (const key of numericKeys) s[key] = s[key] === "" ? "" : Number.isFinite(Number(s[key])) && Number(s[key]) >= 0 ? String(Number(s[key])) : "";
    for (const key of ["origin", "destination", "equipment", "pickupFrom", "pickupTo"]) s[key] = String(s[key]).slice(0, 250).trim();
    for (const key of Object.keys(defaults).filter(k => typeof defaults[k] === "boolean")) s[key] = s[key] === true;
    if (!["chime", "bell", "radar", "silent"].includes(s.sound)) s.sound = "chime";
    if (!["gold", "green", "blue"].includes(s.highlight)) s.highlight = "gold";
    return s;
  }
  function matches(l, s) {
    if (!l.available) return false;
    for (const [key, field, min] of [["minPrice","price",true],["minRPM","rpm",true],["minMiles","miles",true],["maxMiles","miles",false],["maxWeight","weight",false],["maxDeadhead","deadhead",false],["maxStops","stops",false]]) {
      if (s[key] !== "" && (l[field] === null || (min ? l[field] < Number(s[key]) : l[field] > Number(s[key])))) return false;
    }
    for (const key of ["origin", "destination", "equipment"]) {
      const choices = s[key].split(";").map(v => v.trim().toLowerCase()).filter(Boolean);
      if (choices.length && !choices.some(v => l[key].toLowerCase().includes(v))) return false;
    }
    if (s.pickupFrom && (!l.pickupDate || l.pickupDate < s.pickupFrom)) return false;
    if (s.pickupTo && (!l.pickupDate || l.pickupDate > s.pickupTo)) return false;
    return !s.bookableOnly || l.bookable;
  }
  function signature(l, s) { return s.alertPriceChanges ? `${l.id}:${l.price}:${l.rpm}` : l.id; }
  function fromJob(j) {
    const numeric = v => typeof v === "number" && Number.isFinite(v) && v >= 0 ? v : null;
    const place = l => [l?.city, l?.state].filter(Boolean).join(", ") + (l?.zipcode ? " " + l.zipcode : "");
    if (!j || typeof j.jobIdentifier !== "string" || !/^[A-Z0-9]{5,20}$/.test(j.jobIdentifier)) throw new Error("Unknown load identifier format");
    return { id: j.jobIdentifier, origin: place(j.firstPickup?.location), destination: place(j.lastDelivery?.location),
      equipment: [j.equipment?.type, j.equipment?.subType].filter(Boolean).join(" "),
      pickupDate: /^\d{4}-\d{2}-\d{2} /.test(j.firstPickup?.appointmentBegin || "") ? j.firstPickup.appointmentBegin.slice(0, 10) : null,
      price: numeric(j.bookNowPrice) > 0 ? j.bookNowPrice : null, rpm: numeric(j.pricePerMile) > 0 ? j.pricePerMile : null,
      miles: numeric(j.loadedMiles), weight: numeric(j.loadedWeight), deadhead: numeric(j.deadheadOrigin), stops: numeric(j.stopCount),
      bookable: j.bookNowEligibility === true && j.isJobInDeferredStatus === false,
      available: j.loadboardStatus === "AVAILABLE" };
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
  return { defaults, normalize, numericKeys, matches, signature, fromJob, fromResponse };
})();
