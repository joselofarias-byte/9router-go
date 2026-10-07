#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import * as z from "zod/v4";

const rootArg = process.argv[2];
if (!rootArg) {
  process.stderr.write("missing repository root\n");
  process.exit(2);
}

const ROOT = fs.realpathSync(rootArg);
const ROOT_PREFIX = ROOT.endsWith(path.sep) ? ROOT : ROOT + path.sep;
const MAX_FILE_BYTES = 1024 * 1024;
const MAX_TOOL_TEXT = 220_000;

const blockedDirs = new Set([
  ".git", "node_modules", "vendor", "dist", "build", "target", ".cache",
  ".gradle", ".idea", ".vscode", ".venv", "venv", "__pycache__",
  "graphify-out", "artifacts", "coverage"
]);

const blockedNames = new Set([
  ".env", "auth.json", "kaggle.json", "credentials.json", "secrets.json",
  "id_rsa", "id_ed25519"
]);

const blockedExts = new Set([
  ".key", ".pem", ".p12", ".pfx", ".jks", ".keystore", ".db", ".sqlite",
  ".sqlite3", ".apk", ".aab", ".so", ".dll", ".exe", ".bin", ".zip", ".7z",
  ".tar", ".gz", ".jpg", ".jpeg", ".png", ".gif", ".webp", ".mp4", ".mov",
  ".pdf", ".woff", ".woff2", ".ttf", ".otf"
]);

function clipped(text, max = MAX_TOOL_TEXT) {
  if (text.length <= max) return text;
  return text.slice(0, max) + "\n...[truncated]...";
}

function isBlockedRelative(rel) {
  const norm = rel.replaceAll("\\", "/");
  const parts = norm.split("/").filter(Boolean);
  if (parts.some(p => blockedDirs.has(p))) return true;
  const base = (parts.at(-1) || "").toLowerCase();
  if (blockedNames.has(base) || base.startsWith(".env.")) return true;
  const ext = path.extname(base).toLowerCase();
  return blockedExts.has(ext);
}

function normalizeRel(input) {
  if (typeof input !== "string") throw new Error("path must be a string");
  if (path.isAbsolute(input)) throw new Error("absolute paths are not allowed");
  const rel = path.normalize(input).replace(/^([.][\/])+/, "");
  if (!rel || rel === ".") return "";
  if (rel === ".." || rel.startsWith("../") || rel.includes("/../")) {
    throw new Error("path traversal is not allowed");
  }
  if (isBlockedRelative(rel)) throw new Error("path is excluded from audit access");
  return rel;
}

function safeExistingPath(relInput) {
  const rel = normalizeRel(relInput);
  const candidate = path.join(ROOT, rel);
  const st = fs.lstatSync(candidate);
  if (st.isSymbolicLink()) throw new Error("symlinks are not followed");
  const real = fs.realpathSync(candidate);
  if (real !== ROOT && !real.startsWith(ROOT_PREFIX)) {
    throw new Error("path escapes repository root");
  }
  return { rel, real, st };
}

function runGit(args, maxBytes = 2 * 1024 * 1024) {
  const p = spawnSync("git", ["-C", ROOT, "--no-pager", ...args], {
    encoding: "utf8",
    maxBuffer: maxBytes,
    timeout: 20_000,
    env: { ...process.env, GIT_PAGER: "cat", PAGER: "cat" }
  });
  if (p.error) throw p.error;
  if (p.status !== 0) {
    throw new Error((p.stderr || p.stdout || "git command failed").trim());
  }
  return p.stdout;
}

function repoFiles() {
  const out = runGit(["ls-files", "-co", "--exclude-standard", "-z"], 8 * 1024 * 1024);
  return out
    .split("\0")
    .filter(Boolean)
    .map(p => p.replaceAll("\\", "/"))
    .filter(p => !isBlockedRelative(p));
}

function textResult(text) {
  return { content: [{ type: "text", text: clipped(String(text)) }] };
}

const server = new McpServer({
  name: "9router-readonly-audit",
  version: "1.0.0"
});

server.registerTool(
  "repo_info",
  {
    description: "Read repository identity, branch, HEAD and working-tree status. No writes.",
    inputSchema: {},
    annotations: { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false }
  },
  async () => {
    const branch = runGit(["rev-parse", "--abbrev-ref", "HEAD"]).trim();
    const head = runGit(["rev-parse", "HEAD"]).trim();
    const status = runGit(["status", "--short", "--untracked-files=all"]).trim();
    return textResult(JSON.stringify({
      repository: path.basename(ROOT),
      branch,
      head,
      status: status || "clean"
    }, null, 2));
  }
);

server.registerTool(
  "list_files",
  {
    description: "List auditable repository files. Secrets, binaries, build output and symlinks are excluded.",
    inputSchema: {
      prefix: z.string().optional().describe("Optional relative directory or filename prefix"),
      limit: z.number().int().min(1).max(2000).optional().describe("Maximum returned paths")
    },
    annotations: { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false }
  },
  async ({ prefix, limit }) => {
    const wanted = prefix ? normalizeRel(prefix).replaceAll("\\", "/") : "";
    const max = limit ?? 800;
    const files = repoFiles().filter(p => !wanted || p === wanted || p.startsWith(wanted.endsWith("/") ? wanted : wanted + "/") || p.startsWith(wanted));
    const shown = files.slice(0, max);
    const suffix = files.length > shown.length ? `\n... ${files.length - shown.length} more files not shown` : "";
    return textResult(shown.join("\n") + suffix);
  }
);

server.registerTool(
  "read_file",
  {
    description: "Read a bounded line range from one repository text file. No symlinks, secrets or binaries.",
    inputSchema: {
      path: z.string().describe("Relative repository path"),
      start_line: z.number().int().min(1).optional(),
      end_line: z.number().int().min(1).optional()
    },
    annotations: { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false }
  },
  async ({ path: relPath, start_line, end_line }) => {
    const { rel, real, st } = safeExistingPath(relPath);
    if (!st.isFile()) throw new Error("not a regular file");
    if (st.size > MAX_FILE_BYTES) throw new Error("file exceeds audit read limit");
    const buf = fs.readFileSync(real);
    if (buf.includes(0)) throw new Error("binary file is not readable by audit tool");
    const lines = buf.toString("utf8").split(/\r?\n/);
    const start = start_line ?? 1;
    const requestedEnd = end_line ?? Math.min(lines.length, start + 299);
    const end = Math.min(lines.length, requestedEnd, start + 499);
    if (end < start) throw new Error("end_line must be >= start_line");
    const body = lines.slice(start - 1, end).map((line, i) => `${start + i}: ${line}`).join("\n");
    return textResult(`FILE ${rel} lines ${start}-${end} of ${lines.length}\n${body}`);
  }
);

server.registerTool(
  "search_text",
  {
    description: "Search auditable text files in the repository and return path:line evidence. Read-only.",
    inputSchema: {
      query: z.string().min(1).max(300),
      regex: z.boolean().optional(),
      case_sensitive: z.boolean().optional(),
      path_prefix: z.string().optional(),
      limit: z.number().int().min(1).max(300).optional()
    },
    annotations: { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false }
  },
  async ({ query, regex, case_sensitive, path_prefix, limit }) => {
    const wanted = path_prefix ? normalizeRel(path_prefix).replaceAll("\\", "/") : "";
    const max = limit ?? 120;
    let matcher;
    if (regex) {
      matcher = new RegExp(query, case_sensitive ? "" : "i");
    }
    const needle = case_sensitive ? query : query.toLowerCase();
    const matches = [];
    for (const rel of repoFiles()) {
      if (wanted && !(rel === wanted || rel.startsWith(wanted.endsWith("/") ? wanted : wanted + "/") || rel.startsWith(wanted))) continue;
      let entry;
      try { entry = safeExistingPath(rel); } catch { continue; }
      if (!entry.st.isFile() || entry.st.size > MAX_FILE_BYTES) continue;
      const buf = fs.readFileSync(entry.real);
      if (buf.includes(0)) continue;
      const lines = buf.toString("utf8").split(/\r?\n/);
      for (let i = 0; i < lines.length; i++) {
        const hay = case_sensitive ? lines[i] : lines[i].toLowerCase();
        const ok = regex ? matcher.test(lines[i]) : hay.includes(needle);
        if (regex) matcher.lastIndex = 0;
        if (ok) {
          matches.push(`${rel}:${i + 1}: ${lines[i].slice(0, 500)}`);
          if (matches.length >= max) return textResult(matches.join("\n") + "\n...[match limit reached]...");
        }
      }
    }
    return textResult(matches.length ? matches.join("\n") : "No matches.");
  }
);

server.registerTool(
  "git_diff",
  {
    description: "Read a Git diff for the working tree, index, or current branch against a base ref. Never runs external diff/textconv.",
    inputSchema: {
      base: z.string().regex(/^[A-Za-z0-9._\/-]{1,160}$/).optional(),
      cached: z.boolean().optional()
    },
    annotations: { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false }
  },
  async ({ base, cached }) => {
    let args = ["diff", "--no-ext-diff", "--no-textconv", "--unified=3"];
    if (base) {
      const mergeBase = runGit(["merge-base", "HEAD", base]).trim();
      args.push(mergeBase, "HEAD");
    } else if (cached) {
      args.push("--cached");
    }
    const out = runGit(args, 8 * 1024 * 1024);
    return textResult(out || "No diff.");
  }
);

server.registerTool(
  "git_log",
  {
    description: "Read recent commit history. Read-only.",
    inputSchema: {
      limit: z.number().int().min(1).max(100).optional()
    },
    annotations: { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false }
  },
  async ({ limit }) => {
    const n = limit ?? 30;
    return textResult(runGit(["log", `-n${n}`, "--oneline", "--decorate=no", "--no-show-signature"]));
  }
);

const transport = new StdioServerTransport();
await server.connect(transport);
