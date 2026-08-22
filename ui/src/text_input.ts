const segmenter = new Intl.Segmenter(undefined, { granularity: "grapheme" })

export function graphemes(value: string): string[] {
  return Array.from(segmenter.segment(value), ({ segment }) => segment)
}

export function truncateGraphemes(value: string, maximum: number): string {
  return graphemes(value).slice(0, Math.max(0, maximum)).join("")
}
