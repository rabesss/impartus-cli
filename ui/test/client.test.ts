import { afterEach, describe, expect, test } from "bun:test"

import { SessionClient } from "../src/client.ts"
import {
  PROTOCOL_VERSION,
  type Bootstrap,
  type CourseList,
  type Event,
  type Health,
  type Operation,
} from "../src/protocol/types.gen.ts"

const servers: Bun.Server<undefined>[] = []

afterEach(() => {
  for (const server of servers.splice(0)) {
    server.stop(true)
  }
})

describe("SessionClient", () => {
  test("uses authenticated routes and parses the ordered event stream", async () => {
    const requests: Array<{ capability: string | null; protocol: string | null; url: string }> = []
    const server = Bun.serve({
      port: 0,
      fetch(request) {
        requests.push({
          capability: request.headers.get("X-Impartus-Capability"),
          protocol: request.headers.get("X-Impartus-Protocol"),
          url: request.url,
        })
        const path = new URL(request.url).pathname
        switch (path) {
          case "/tui/v1/health":
            return Response.json({
              protocol: PROTOCOL_VERSION,
              sessionId: "session-id",
              status: "ok",
              version: "test",
            } satisfies Health)
          case "/tui/v1/courses":
            return Response.json({
              courses: [
                {
                  instituteId: 1,
                  professorName: "Dr. Rao",
                  sessionId: 2,
                  sessionName: "Monsoon",
                  subjectId: 3,
                  subjectName: "Distributed Systems",
                  videoCount: 4,
                },
              ],
            } satisfies CourseList)
          case "/tui/v1/operations":
            return Response.json({ id: "operation-id", kind: "selftest", state: "running" } satisfies Operation, {
              status: 202,
            })
          case "/tui/v1/events": {
            const events: Event[] = [
              { sequence: 1, type: "session.ready" },
              {
                operationId: "operation-id",
                percent: 100,
                sequence: 2,
                state: "running",
                type: "operation.progress",
              },
              {
                operationId: "operation-id",
                sequence: 3,
                state: "completed",
                type: "operation.completed",
              },
            ]
            return new Response(events.map((event) => `data: ${JSON.stringify(event)}\n\n`).join(""), {
              headers: { "Content-Type": "text/event-stream" },
            })
          }
          default:
            return new Response("not found", { status: 404 })
        }
      },
    })
    servers.push(server)
    const bootstrap: Bootstrap = {
      baseUrl: `http://127.0.0.1:${server.port}/tui/v1`,
      capability: "c".repeat(43),
      protocol: PROTOCOL_VERSION,
      sessionId: "session-id",
    }
    const client = new SessionClient(bootstrap)

    expect((await client.health()).sessionId).toBe("session-id")
    expect((await client.courses()).courses[0]?.subjectName).toBe("Distributed Systems")
    expect((await client.startSelfTest()).id).toBe("operation-id")
    const events: Event[] = []
    for await (const event of client.events()) {
      events.push(event)
    }
    expect(events.map((event) => event.sequence)).toEqual([1, 2, 3])

    expect(requests).toHaveLength(4)
    for (const request of requests) {
      expect(request.capability).toBe(bootstrap.capability)
      expect(request.protocol).toBe(PROTOCOL_VERSION)
      expect(request.url).not.toContain(bootstrap.capability)
    }
  })
})
