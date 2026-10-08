// Workbench colors adapted from Microsoft VS Code's bundled MIT-licensed themes:
// https://github.com/microsoft/vscode/tree/main/extensions/theme-defaults/themes
// and extensions/theme-{solarized-light,solarized-dark,monokai,monokai-dimmed}.
// Semantic ERP status colors remain independent of the editor syntax palettes.
export const themes = [
  { id: "default", name: "MSERP (default)", description: "Original zinc and blue", background: "#09090b", sidebar: "#0c0c0e", foreground: "#d4d4d8", accent: "#3b82f6" },
  { id: "solarized-light", name: "Solarized Light", description: "Warm ivory and muted teal", background: "#fdf6e3", sidebar: "#eee8d5", foreground: "#657b83", accent: "#268bd2" },
  { id: "solarized-dark", name: "Solarized Dark", description: "Deep blue-green and soft accents", background: "#002b36", sidebar: "#00212b", foreground: "#93a1a1", accent: "#2aa198" },
  { id: "monokai", name: "Monokai", description: "Charcoal with vivid accents", background: "#272822", sidebar: "#1e1f1c", foreground: "#f8f8f2", accent: "#a6e22e" },
  { id: "monokai-dimmed", name: "Monokai Dimmed", description: "Soft charcoal and subdued accents", background: "#1e1e1e", sidebar: "#272727", foreground: "#c5c8c6", accent: "#c4b28d" },
  { id: "dark-modern", name: "Dark Modern", description: "Neutral dark gray and blue", background: "#1f1f1f", sidebar: "#181818", foreground: "#cccccc", accent: "#4daafc" },
  { id: "default-light", name: "Default Light", description: "Clean white and classic blue", background: "#ffffff", sidebar: "#f3f3f3", foreground: "#333333", accent: "#006ab1" },
] as const;

export type ThemeId = typeof themes[number]["id"];
export function normalizeTheme(value: string): ThemeId {
  return themes.find(theme => theme.id === value)?.id ?? "default";
}
