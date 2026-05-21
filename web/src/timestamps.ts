// Pure helpers for the agent-pane timestamp dividers. Kept free of React/DOM so
// the bucketing logic can be unit-tested directly.

// minuteBucket maps a Unix-millis timestamp to the integer minute it falls in.
// Two timestamps in the same wall-clock minute share a bucket; the value
// increments when they cross a minute boundary.
export function minuteBucket(tsMillis: number): number {
  return Math.floor(tsMillis / 60000)
}

// formatClock renders a Unix-millis timestamp as a locale HH:MM string for the
// divider label.
export function formatClock(tsMillis: number): string {
  const d = new Date(tsMillis)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
