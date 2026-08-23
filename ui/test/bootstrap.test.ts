import { afterEach, describe, expect, test } from "bun:test"
import { chmod, mkdtemp, rm, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"

import { consumeBootstrap } from "../src/bootstrap.ts"
import { PROTOCOL_VERSION, type Bootstrap } from "../src/protocol/types.gen.ts"

const temporaryDirectories: string[] = []

afterEach(async () => {
  await Promise.all(temporaryDirectories.splice(0).map((directory) => rm(directory, { force: true, recursive: true })))
})

describe("consumeBootstrap", () => {
  test("reads one private bootstrap and removes it immediately", async () => {
    const expected: Bootstrap = {
      baseUrl: "http://127.0.0.1:43123/tui/v2",
      capability: "a".repeat(43),
      protocol: PROTOCOL_VERSION,
      sessionId: "session-id",
    }
    const path = await writeBootstrap(JSON.stringify(expected))

    const actual = await consumeBootstrap(["impartus-ui", "--bootstrap", path])

    expect(actual).toEqual(expected)
    expect(await Bun.file(path).exists()).toBe(false)
  })

  test("rejects malformed content without disclosing it and still removes the file", async () => {
    const path = await writeBootstrap('{"capability":"top-secret","protocol":"wrong"}')

    try {
      await consumeBootstrap(["impartus-ui", "--bootstrap", path])
      throw new Error("consumeBootstrap unexpectedly accepted malformed content")
    } catch (error) {
      expect(String(error)).toBe("Error: Invalid private UI bootstrap")
      expect(String(error)).not.toContain("top-secret")
    }
    expect(await Bun.file(path).exists()).toBe(false)
  })
})

async function writeBootstrap(content: string): Promise<string> {
  const directory = await mkdtemp(join(tmpdir(), "impartus-ui-bootstrap-test-"))
  temporaryDirectories.push(directory)
  await chmod(directory, 0o700)
  const path = join(directory, "bootstrap.json")
  await writeFile(path, content, { encoding: "utf8", flag: "wx", mode: 0o600 })
  await chmod(path, 0o600)
  return path
}
