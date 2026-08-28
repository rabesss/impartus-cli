import { afterEach, describe, expect, test } from "bun:test"

import { SessionClient, SessionProblemError } from "../src/client.ts"
import {
  PROTOCOL_VERSION,
  type ArtifactList,
  type Bootstrap,
  type CourseList,
  type DiagnosticList,
  type Event,
  type Health,
  type LectureList,
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
      async fetch(request) {
        requests.push({
          capability: request.headers.get("X-Impartus-Capability"),
          protocol: request.headers.get("X-Impartus-Protocol"),
          url: request.url,
        })
        const path = new URL(request.url).pathname
        if (path.endsWith("/commands")) {
          await request.json()
          return Response.json({ id: "operation-id", kind: "playback", state: "running" } satisfies Operation)
        }
        switch (path) {
          case "/tui/v2/health":
            return Response.json({
        authStatus: "ready",
              protocol: PROTOCOL_VERSION,
              sessionId: "session-id",
              status: "ok",
              version: "test",
            } satisfies Health)
      case "/tui/v2/auth/retry":
      expect(request.method).toBe("POST")
      expect(await request.text()).toBe("")
      return Response.json({
        authStatus: "ready",
        protocol: PROTOCOL_VERSION,
        sessionId: "session-id",
        status: "ok",
        version: "test",
      } satisfies Health)
          case "/tui/v2/courses":
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
          case "/tui/v2/lectures":
            return Response.json({
              lectures: [{
                classroomName: "Room 7",
                durationSeconds: 3600,
                instituteId: 1,
                noAudio: false,
                professorName: "Dr. Rao",
                sequence: 4,
                sessionId: 2,
                sessionName: "Monsoon",
                startTime: "2026-08-13T10:00:00Z",
                subjectId: 3,
                subjectName: "Distributed Systems",
                topic: "Consensus",
                ttid: 5,
                views: 2,
              }],
            } satisfies LectureList)
          case "/tui/v2/library":
            return Response.json({
              artifacts: [{
                artifactId: "artifact-1",
                fileCount: 2,
                presentFileCount: 2,
                producedAt: "2026-08-13T12:30:00Z",
                sequence: 4,
                topic: "Consensus",
                totalBytes: 2048,
              }],
            } satisfies ArtifactList)
          case "/tui/v2/diagnostics":
            return Response.json({
              diagnostics: [{ detail: "available", name: "mpv", status: "pass" }],
            } satisfies DiagnosticList)
          case "/tui/v2/operations": {
            const body = await request.json() as { kind: "download" | "selftest" }
            return Response.json({ id: "operation-id", kind: body.kind, state: "running" } satisfies Operation, {
              status: 202,
            })
          }
          case "/tui/v2/events": {
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
            const stream = events.map((event) => `data: ${JSON.stringify(event)}\n\n`).join(": heartbeat\n\n")
            return new Response(stream, {
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
      baseUrl: `http://127.0.0.1:${server.port}/tui/v2`,
      capability: "c".repeat(43),
      protocol: PROTOCOL_VERSION,
      sessionId: "session-id",
    }
    const client = new SessionClient(bootstrap)

    expect((await client.health()).sessionId).toBe("session-id")
  expect((await client.retryAuthentication()).authStatus).toBe("ready")
    expect((await client.courses()).courses[0]?.subjectName).toBe("Distributed Systems")
    const course = (await client.courses()).courses[0]
    expect(course).toBeDefined()
    const lecture = (await client.lectures(course!)).lectures[0]
    expect(lecture?.topic).toBe("Consensus")
    expect((await client.artifacts()).artifacts[0]?.totalBytes).toBe(2048)
    expect((await client.diagnostics()).diagnostics[0]?.status).toBe("pass")
    expect((await client.startSelfTest()).id).toBe("operation-id")
    expect((await client.startDownload(lecture!)).kind).toBe("download")
    expect((await client.startPlayback(lecture!, true)).kind).toBe("playback")
    expect((await client.playbackCommand("operation-id", { action: "pause", flag: true })).kind).toBe("playback")
    const events: Event[] = []
    for await (const event of client.events()) {
      events.push(event)
    }
    expect(events.map((event) => event.sequence)).toEqual([1, 2, 3])

  expect(requests).toHaveLength(12)
    for (const request of requests) {
      expect(request.capability).toBe(bootstrap.capability)
      expect(request.protocol).toBe(PROTOCOL_VERSION)
      expect(request.url).not.toContain(bootstrap.capability)
    }
    expect(requests.find((request) => request.url.includes("/lectures?"))?.url).toContain("subjectId=3")
  })

  test("rejects retry health from a different private session", async () => {
    const server = Bun.serve({
      port: 0,
      fetch() {
        return Response.json({
          authStatus: "ready",
          protocol: PROTOCOL_VERSION,
          sessionId: "different-session",
          status: "ok",
          version: "test",
        } satisfies Health)
      },
    })
    servers.push(server)
    const client = new SessionClient({
      baseUrl: `http://127.0.0.1:${server.port}/tui/v2`,
      capability: "c".repeat(43),
      protocol: PROTOCOL_VERSION,
      sessionId: "session-id",
    })

    await expect(client.retryAuthentication()).rejects.toThrow("Invalid UI session response")
  })

  test("preserves the original fetch failure as the error cause", async () => {
    const server = Bun.serve({ port: 0, fetch: () => Response.json({ status: "ok" }) })
    const client = new SessionClient({
      baseUrl: `http://127.0.0.1:${server.port}/tui/v2`,
      capability: "c".repeat(43),
      protocol: PROTOCOL_VERSION,
      sessionId: "session-id",
    })
    server.stop(true)

    try {
      await client.health()
      expect.unreachable()
    } catch (error) {
      expect((error as Error).message).toBe("UI session is unavailable")
      expect((error as Error).cause).toBeDefined()
    }
  })

  test("surfaces only validated safe Problems and discards arbitrary failure bodies", async () => {
    const secret = "username=person@example.com password=body-secret"
    let retryFailures = 0
    const server = Bun.serve({
      port: 0,
      fetch(request) {
        const path = new URL(request.url).pathname
        if (path.endsWith("/auth/retry")) {
          retryFailures++
          if (retryFailures === 1) {
            return Response.json({ code: "auth_unavailable", error: "upstream authentication is unavailable" }, { status: 503 })
          }
          if (retryFailures === 2) {
            return Response.json({ code: "configuration_invalid", error: "configuration is invalid" }, { status: 503 })
          }
          if (retryFailures === 3) {
            return Response.json({ code: "auth_unavailable", error: secret }, { status: 503 })
          }
          return Response.json({ code: "auth_unavailable", error: "upstream authentication is unavailable", padding: "x".repeat(4096) }, { status: 503 })
        }
        return new Response(secret, { status: 502 })
      },
    })
    servers.push(server)
    const client = new SessionClient({
      baseUrl: `http://127.0.0.1:${server.port}/tui/v2`,
      capability: "c".repeat(43),
      protocol: PROTOCOL_VERSION,
      sessionId: "session-id",
    })

    await expect(client.courses()).rejects.toThrow("UI session request failed (502)")
    try {
      await client.courses()
    } catch (error) {
      expect(String(error)).not.toContain(secret)
    }
    try {
      await client.retryAuthentication()
      expect.unreachable()
    } catch (error) {
      expect(error).toBeInstanceOf(SessionProblemError)
      expect((error as SessionProblemError).code).toBe("auth_unavailable")
      expect((error as Error).message).toBe("upstream authentication is unavailable")
    }
    try {
      await client.retryAuthentication()
      expect.unreachable()
    } catch (error) {
      expect(error).toBeInstanceOf(SessionProblemError)
      expect((error as SessionProblemError).code).toBe("configuration_invalid")
      expect((error as Error).message).toBe("configuration is invalid")
    }
    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        await client.retryAuthentication()
        expect.unreachable()
      } catch (error) {
        expect(error).not.toBeInstanceOf(SessionProblemError)
        expect((error as Error).message).toBe("UI session request failed (503)")
        expect(String(error)).not.toContain(secret)
      }
    }
  })
})
