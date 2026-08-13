import { constants, type Stats } from "node:fs"
import { lstat, open, unlink } from "node:fs/promises"
import { dirname } from "node:path"

import { PROTOCOL_BASE_PATH, PROTOCOL_VERSION, type Bootstrap } from "./protocol/types.gen.ts"

const MAX_BOOTSTRAP_BYTES = 4 * 1024
const BOOTSTRAP_KEYS = ["baseUrl", "capability", "protocol", "sessionId"] as const

export async function consumeBootstrap(arguments_: readonly string[]): Promise<Bootstrap> {
  const path = bootstrapPath(arguments_)
  try {
    const flags = process.platform === "win32" ? constants.O_RDONLY : constants.O_RDONLY | constants.O_NOFOLLOW
    const file = await open(path, flags)
    try {
      const descriptor = await file.stat()
      if (!descriptor.isFile() || descriptor.size <= 0 || descriptor.size > MAX_BOOTSTRAP_BYTES) {
        throw new Error("unsafe bootstrap")
      }
      if (process.platform !== "win32") {
        await validateUnixBootstrap(path, descriptor)
      }
      const content = await file.readFile({ encoding: "utf8" })
      return validateBootstrap(JSON.parse(content) as unknown)
    } finally {
      await file.close()
    }
  } catch {
    throw new Error("Invalid private UI bootstrap")
  } finally {
    try {
      await unlink(path)
    } catch {
      // The Go parent also removes the owner-private directory on every exit.
      // Keep the user-facing error generic and never print bootstrap content.
    }
  }
}

function bootstrapPath(arguments_: readonly string[]): string {
  const positions = arguments_.flatMap((argument, index) => (argument === "--bootstrap" ? [index] : []))
  if (positions.length !== 1) {
    throw new Error("Invalid private UI bootstrap")
  }
  const path = arguments_[positions[0]! + 1]
  if (path === undefined || path.length === 0) {
    throw new Error("Invalid private UI bootstrap")
  }
  return path
}

async function validateUnixBootstrap(path: string, descriptor: Stats): Promise<void> {
  const effectiveUser = process.geteuid?.()
  const pathInfo = await lstat(path)
  const directoryInfo = await lstat(dirname(path))
  if (
    effectiveUser === undefined ||
    descriptor.uid !== effectiveUser ||
    pathInfo.uid !== effectiveUser ||
    directoryInfo.uid !== effectiveUser ||
    (descriptor.mode & 0o777) !== 0o600 ||
    (directoryInfo.mode & 0o777) !== 0o700 ||
    pathInfo.isSymbolicLink() ||
    !directoryInfo.isDirectory() ||
    descriptor.dev !== pathInfo.dev ||
    descriptor.ino !== pathInfo.ino
  ) {
    throw new Error("unsafe bootstrap")
  }
}

function validateBootstrap(value: unknown): Bootstrap {
  if (!isRecord(value) || Object.keys(value).sort().join(",") !== [...BOOTSTRAP_KEYS].sort().join(",")) {
    throw new Error("invalid bootstrap shape")
  }
  if (
    typeof value.baseUrl !== "string" ||
    typeof value.capability !== "string" ||
    typeof value.protocol !== "string" ||
    typeof value.sessionId !== "string" ||
    value.capability.length < 32 ||
    value.protocol !== PROTOCOL_VERSION ||
    value.sessionId.length === 0
  ) {
    throw new Error("invalid bootstrap values")
  }
  const endpoint = new URL(value.baseUrl)
  if (
    endpoint.protocol !== "http:" ||
    endpoint.hostname !== "127.0.0.1" ||
    endpoint.pathname !== PROTOCOL_BASE_PATH ||
    endpoint.username !== "" ||
    endpoint.password !== "" ||
    endpoint.search !== "" ||
    endpoint.hash !== ""
  ) {
    throw new Error("invalid bootstrap endpoint")
  }
  return {
    baseUrl: value.baseUrl,
    capability: value.capability,
    protocol: value.protocol,
    sessionId: value.sessionId,
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}
