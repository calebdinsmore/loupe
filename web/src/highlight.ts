import hljs from 'highlight.js/lib/common'

// Map a file path to a highlight.js language id. Unknown types fall back to
// auto-detection. Per-line highlighting loses cross-line context (e.g. block
// comments), which is an acceptable trade-off for a line-based diff view.
const EXT_LANG: Record<string, string> = {
  go: 'go', ts: 'typescript', tsx: 'typescript', js: 'javascript', jsx: 'javascript',
  py: 'python', rs: 'rust', rb: 'ruby', java: 'java', kt: 'kotlin', swift: 'swift',
  c: 'c', h: 'c', cc: 'cpp', cpp: 'cpp', hpp: 'cpp', cs: 'csharp', php: 'php',
  json: 'json', yml: 'yaml', yaml: 'yaml', toml: 'ini', md: 'markdown',
  css: 'css', scss: 'scss', html: 'xml', xml: 'xml', sql: 'sql', sh: 'bash', bash: 'bash',
}

export function langForPath(path: string): string | undefined {
  return EXT_LANG[path.slice(path.lastIndexOf('.') + 1).toLowerCase()]
}

// Returns HTML with hljs token spans. highlight.js escapes the input, so the
// result is safe to inject via dangerouslySetInnerHTML.
export function highlightLine(content: string, lang?: string): string {
  if (!content) return ''
  try {
    if (lang && hljs.getLanguage(lang)) {
      return hljs.highlight(content, { language: lang, ignoreIllegal: true }).value
    }
    return hljs.highlightAuto(content).value
  } catch {
    return content.replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]!))
  }
}
