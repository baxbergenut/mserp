/* Observe only the confirmed load-search endpoint. Never read or retain request
   headers, cookies, request bodies, tokens, or unrelated GraphQL response data. */
(() => {
  const path = "/carrier/secure/loadboard/graphql/native";
  let sequence = 0;
  const target = value => { try { const u = new URL(value, location.href); return u.origin === location.origin && u.pathname === path; } catch { return false; } };
  const emit = payload => window.postMessage({ source: "mserp-dispatch-response-v1", ...payload }, location.origin);
  const scalar = v => typeof v === "string" ? v.slice(0, 200) : typeof v === "number" && Number.isFinite(v) ? v : typeof v === "boolean" ? v : null;
  const place = l => ({ city: scalar(l?.city), state: scalar(l?.state), zipcode: scalar(l?.zipcode), country: scalar(l?.country) });
  const stop = s => ({ appointmentBegin: scalar(s?.appointmentBegin), appointmentEnd: scalar(s?.appointmentEnd), isAppointmentConfirmed: scalar(s?.isAppointmentConfirmed), location: place(s?.location) });
  function publish(body, status, id, startedAt) {
    const result = body?.data?.searchJobsElastic;
    if (status < 200 || status >= 300) { emit({ id, startedAt, error: status === 429 ? "J.B. Hunt rate limited this search. Pause and try a slower interval later." : `J.B. Hunt returned HTTP ${status}. Check your login and search.` }); return; }
    // Other operations share this GraphQL endpoint and are deliberately ignored.
    if (!result) { if (body?.errors) emit({ id, startedAt, error: "J.B. Hunt returned a GraphQL error. Check the search before restarting." }); return; }
    if (!result.success || result.status !== "OK" || !Array.isArray(result.data?.jobs) || result.data.jobs.length > 1000) {
      emit({ id, startedAt, error: "J.B. Hunt search failed or its response format changed." }); return;
    }
    const data = result.data;
    const jobs = data.jobs.map(j => ({ jobIdentifier: scalar(j.jobIdentifier), jobType: scalar(j.jobType), stopCount: scalar(j.stopCount),
      equipment: { type: scalar(j.equipment?.type), subType: scalar(j.equipment?.subType) },
      loadedMiles: scalar(j.loadedMiles), totalMiles: scalar(j.totalMiles), deadheadOrigin: scalar(j.deadheadOrigin), deadheadDestination: scalar(j.deadheadDestination),
      loadedWeight: scalar(j.loadedWeight), firstPickup: stop(j.firstPickup), lastDelivery: stop(j.lastDelivery),
      bookNowEligibility: scalar(j.bookNowEligibility), bookNowPrice: scalar(j.bookNowPrice), pricePerMile: scalar(j.pricePerMile),
      isJobInDeferredStatus: scalar(j.isJobInDeferredStatus), loadboardStatus: scalar(j.loadboardStatus) }));
    emit({ id, startedAt, receivedAt: Date.now(), data: { jobs, page: scalar(data.page), pageSize: scalar(data.pageSize), totalPages: scalar(data.totalPages), totalElements: scalar(data.totalElements) } });
  }
  const originalFetch = window.fetch;
  window.fetch = async function(...args) {
    const watch = target(typeof args[0] === "string" || args[0] instanceof URL ? args[0] : args[0]?.url);
    const id = ++sequence, startedAt = Date.now();
    const response = await originalFetch.apply(this, args);
    if (watch) void response.clone().json().then(body => publish(body, response.status, id, startedAt)).catch(() => emit({ id, startedAt, error: "Load search response was not JSON. Check the site and sign in again if needed." }));
    return response;
  };
  const originalOpen = XMLHttpRequest.prototype.open, originalSend = XMLHttpRequest.prototype.send;
  const tracked = new WeakMap();
  XMLHttpRequest.prototype.open = function(method, url, ...rest) { tracked.set(this, target(url)); return originalOpen.call(this, method, url, ...rest); };
  XMLHttpRequest.prototype.send = function(...args) {
    if (tracked.get(this)) {
      const id = ++sequence, startedAt = Date.now();
      this.addEventListener("loadend", () => {
        try { publish(this.responseType === "json" ? this.response : JSON.parse(this.responseText), this.status, id, startedAt); }
        catch { emit({ id, startedAt, error: "Load search response was unreadable. Check the site and sign in again if needed." }); }
      }, { once: true });
    }
    return originalSend.apply(this, args);
  };
})();
