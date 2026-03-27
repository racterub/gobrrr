#!/usr/bin/env bun
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { createConnection } from "net";
import { homedir } from "os";
import { join } from "path";

const SOCKET_PATH = join(homedir(), ".gobrrr", "gobrrr.sock");
const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 30000;

const mcp = new Server(
  { name: "gobrrr", version: "0.0.1" },
  {
    capabilities: {
      experimental: { "claude/channel": {} },
    },
    instructions:
      'Task results from gobrrr workers arrive as <channel source="gobrrr" task_id="..." status="..." prompt_summary="...">. ' +
      "These are results from tasks you previously dispatched via `gobrrr submit`. " +
      "Read the result and decide whether to act on it, relay it to the user, or absorb it silently.",
  }
);

await mcp.connect(new StdioServerTransport());

function connectToStream(attempt: number = 0) {
  const socket = createConnection(SOCKET_PATH, () => {
    // Send HTTP request over Unix socket
    socket.write(
      "GET /tasks/results/stream HTTP/1.1\r\n" +
        "Host: gobrrr\r\n" +
        "Accept: text/event-stream\r\n" +
        "\r\n"
    );
    attempt = 0; // Reset on successful connection
  });

  let buffer = "";
  let headersParsed = false;

  socket.on("data", (chunk: Buffer) => {
    buffer += chunk.toString();

    // Skip HTTP headers on first data
    if (!headersParsed) {
      const headerEnd = buffer.indexOf("\r\n\r\n");
      if (headerEnd === -1) return;
      buffer = buffer.slice(headerEnd + 4);
      headersParsed = true;
    }

    // Parse SSE events from buffer
    const lines = buffer.split("\n");
    buffer = lines.pop() ?? ""; // Keep incomplete line in buffer

    for (const line of lines) {
      if (line.startsWith("data: ")) {
        const jsonStr = line.slice(6).trim();
        if (!jsonStr) continue;
        try {
          const event = JSON.parse(jsonStr);
          pushToSession(event);
        } catch {
          // Ignore malformed JSON
        }
      }
    }
  });

  socket.on("error", () => {
    scheduleReconnect(attempt);
  });

  socket.on("close", () => {
    scheduleReconnect(attempt);
  });
}

function scheduleReconnect(attempt: number) {
  const delay = Math.min(
    RECONNECT_BASE_MS * Math.pow(2, attempt) + Math.random() * 1000,
    RECONNECT_MAX_MS
  );
  setTimeout(() => connectToStream(attempt + 1), delay);
}

async function pushToSession(event: {
  task_id: string;
  status: string;
  prompt_summary: string;
  result: string;
  error: string;
  submitted_at: string;
}) {
  const content = event.error
    ? `Error: ${event.error}\n\n${event.result}`
    : event.result;

  await mcp.notification({
    method: "notifications/claude/channel",
    params: {
      content,
      meta: {
        task_id: event.task_id,
        status: event.status,
        prompt_summary: event.prompt_summary,
      },
    },
  });
}

// Start the SSE connection
connectToStream();
