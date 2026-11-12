export const truckHeadingPinSize = 30;

export function truckHeadingPin(heading: number | null | undefined) {
  const hasHeading = heading != null && Number.isFinite(heading) && heading >= 0 && heading < 360;
  const marker = document.createElement("div");
  marker.setAttribute("role", "img");
  marker.setAttribute("aria-label", hasHeading ? `Reported heading ${heading} degrees` : "Truck location; heading unavailable");
  marker.dataset.heading = hasHeading ? String(heading) : "unknown";
  marker.style.cssText = `width:${truckHeadingPinSize}px;height:${truckHeadingPinSize}px;border:2px solid white;border-radius:50%;box-shadow:0 1px 8px #0008;display:grid;place-items:center;transform:rotate(${hasHeading ? heading : 0}deg)`;
  marker.innerHTML = hasHeading
    ? '<svg width="20" height="20" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3 20 21 12 17 4 21Z" fill="white"/></svg>'
    : '<span style="width:9px;height:9px;background:white;border-radius:50%"></span>';
  return marker;
}
