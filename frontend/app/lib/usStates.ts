export const STATE_CODES: Record<string, string> = {
  Alabama: "AL", Alaska: "AK", Arizona: "AZ", Arkansas: "AR", California: "CA",
  Colorado: "CO", Connecticut: "CT", Delaware: "DE", "District of Columbia": "DC",
  Florida: "FL", Georgia: "GA", Hawaii: "HI", Idaho: "ID", Illinois: "IL",
  Indiana: "IN", Iowa: "IA", Kansas: "KS", Kentucky: "KY", Louisiana: "LA",
  Maine: "ME", Maryland: "MD", Massachusetts: "MA", Michigan: "MI", Minnesota: "MN",
  Mississippi: "MS", Missouri: "MO", Montana: "MT", Nebraska: "NE", Nevada: "NV",
  "New Hampshire": "NH", "New Jersey": "NJ", "New Mexico": "NM", "New York": "NY",
  "North Carolina": "NC", "North Dakota": "ND", Ohio: "OH", Oklahoma: "OK", Oregon: "OR",
  Pennsylvania: "PA", "Rhode Island": "RI", "South Carolina": "SC", "South Dakota": "SD",
  Tennessee: "TN", Texas: "TX", Utah: "UT", Vermont: "VT", Virginia: "VA",
  Washington: "WA", "West Virginia": "WV", Wisconsin: "WI", Wyoming: "WY",
};

export const STATE_NAMES = Object.fromEntries(Object.entries(STATE_CODES).map(([name, code]) => [code, name]));

// Display imported stop locations compactly; callers keep the source value and
// stop identity intact. Do not apply this to manually entered dispatch text.
export function compactLoadLocation(value: string) {
  const parts = value.split(",").map(part => part.trim()).filter(Boolean);
  if (/^\d{5}(?:-\d{4})?$/.test(parts.at(-1) ?? "")) parts.pop();
  if (parts.length) {
    const last = parts.length - 1;
    const state = parts[last].replace(/\s+\d{5}(?:-\d{4})?$/, "").trim();
    const match = Object.entries(STATE_CODES).find(([name, code]) => name.toLowerCase() === state.toLowerCase() || code === state.toUpperCase());
    parts[last] = match?.[1] ?? state;
  }
  return parts.join(", ");
}
