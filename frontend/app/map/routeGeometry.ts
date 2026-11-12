import type { BoardMapHistoryPoint, BoardMapRoute, DriverBoardLocation } from "@/app/lib/types";

export type Coordinates = [latitude: number, longitude: number];

export function drivenTrail(history: BoardMapHistoryPoint[], location: DriverBoardLocation, now: number): Coordinates[][] {
  const currentAt = Date.parse(location.reportedAt);
  const cutoff = now - 24 * 60 * 60_000;
  if (!Number.isFinite(currentAt) || currentAt < cutoff) return [];

  const segments: Coordinates[][] = [];
  let segment: Coordinates[] = [];
  let previousAt = -Infinity;
  for (const point of [...history, location]) {
    const at = Date.parse(point.reportedAt);
    if (!(at >= cutoff && at <= currentAt && at > previousAt)) continue;
    if (at - previousAt > 10 * 60_000) {
      segment = [];
      segments.push(segment);
    }
    const previous = segment.at(-1);
    if (previous?.[0] !== point.latitude || previous?.[1] !== point.longitude) {
      segment.push([point.latitude, point.longitude]);
    }
    previousAt = at;
  }
  return segments.filter(points => points.length > 1);
}

function decodeRoute({ polyline, precision }: BoardMapRoute["geometry"]): Coordinates[] {
  if (precision !== 5 && precision !== 7) return [];
  const scale = 10 ** precision;
  const points: Coordinates[] = [];
  let offset = 0;
  let latitude = 0;
  let longitude = 0;
  const readDelta = () => {
    let value = 0;
    for (let factor = 1; offset < polyline.length && factor <= 32 ** 6; factor *= 32) {
      const chunk = polyline.charCodeAt(offset++) - 63;
      if (chunk < 0 || chunk > 63) break;
      value += (chunk % 32) * factor;
      if (chunk < 32) return value % 2 ? -(value + 1) / 2 : value / 2;
    }
    return NaN;
  };
  while (offset < polyline.length) {
    latitude += readDelta();
    longitude += readDelta();
    if (!(Math.abs(latitude) <= 90 * scale && Math.abs(longitude) <= 180 * scale)) return [];
    points.push([latitude / scale, longitude / scale]);
  }
  return points;
}

export function remainingRoute(route: BoardMapRoute, location: DriverBoardLocation) {
  const points = decodeRoute(route.geometry);
  if (points.length < 2) return null;
  if (Date.parse(location.reportedAt) <= Date.parse(route.reportedAt)) return { points, stale: route.stale };

  // Match a newer GPS fix to the road before dropping the already driven portion.
  const longitudeScale = Math.cos(location.latitude * Math.PI / 180);
  let closest: { end: number; point: Coordinates; distance: number } | undefined;
  for (let end = 1; end < points.length; end++) {
    const [startLat, startLng] = points[end - 1];
    const [endLat, endLng] = points[end];
    const dy = endLat - startLat;
    const dx = (endLng - startLng) * longitudeScale;
    const lengthSquared = dx * dx + dy * dy;
    if (!lengthSquared) continue;
    const y = location.latitude - startLat;
    const x = (location.longitude - startLng) * longitudeScale;
    const fraction = Math.min(1, Math.max(0, (x * dx + y * dy) / lengthSquared));
    const distance = Math.hypot(x - dx * fraction, y - dy * fraction) * 111_195;
    if (!closest || distance < closest.distance) {
      closest = { end, point: [startLat + dy * fraction, startLng + (endLng - startLng) * fraction], distance };
    }
  }
  if (!closest || closest.distance > 250) return { points, stale: true };
  return { points: [closest.point, ...points.slice(closest.end)], stale: route.stale };
}
