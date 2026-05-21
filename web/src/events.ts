// Agent events streamed over the review WebSocket.
export interface AgentEvent {
  type: string
  text?: string
  tool?: string
  ts?: number
}

// appendEvent adds an incoming agent event to the running log, dropping the
// terminal 'result' event when it merely repeats the last assistant text. The
// agent emits its final message both as the last assistant chunk and again in
// the closing 'result', so without this it would render twice. Pure so it can
// be unit-tested away from the WebSocket/DOM.
export function appendEvent(prev: AgentEvent[], ev: AgentEvent): AgentEvent[] {
  if (ev.type === 'result') {
    const text = (ev.text ?? '').trim()
    const prevText = (prev[prev.length - 1]?.text ?? '').trim()
    if (!text || text === prevText) return prev
  }
  return [...prev, ev]
}
