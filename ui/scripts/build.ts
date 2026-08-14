import { mkdir } from "node:fs/promises"
import { dirname, resolve } from "node:path"

const STANDALONE_TARGETS = [
  "bun-linux-x64",
  "bun-linux-arm64",
  "bun-windows-x64",
  "bun-windows-arm64",
  "bun-darwin-x64",
  "bun-darwin-arm64",
] as const

type StandaloneTarget = (typeof STANDALONE_TARGETS)[number]

const target = standaloneTarget(argumentValue(process.argv, "--target"))
const output = resolve(argumentValue(process.argv, "--outfile") ?? "dist/impartus-ui")
await mkdir(dirname(output), { recursive: true })

const result = await Bun.build({
  compile: target === undefined ? { outfile: output } : { outfile: output, target },
  define: {
    "process.env.NODE_ENV": JSON.stringify("production"),
  },
  entrypoints: [resolve("src/main.ts")],
  minify: true,
  sourcemap: "none",
})

if (!result.success) {
  for (const log of result.logs) {
    console.error(log)
  }
  process.exit(1)
}

console.log(output)

function argumentValue(arguments_: readonly string[], name: string): string | undefined {
  const inline = arguments_.find((argument) => argument.startsWith(name + "="))
  if (inline !== undefined) return inline.slice(name.length + 1)
  const position = arguments_.indexOf(name)
  return position >= 0 ? arguments_[position + 1] : undefined
}

function standaloneTarget(value: string | undefined): StandaloneTarget | undefined {
  if (value === undefined) return undefined
  if ((STANDALONE_TARGETS as readonly string[]).includes(value)) {
    return value as StandaloneTarget
  }
  throw new Error(`unsupported standalone target: ${value}`)
}
