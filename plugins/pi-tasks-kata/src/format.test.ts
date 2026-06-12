import { describe, expect, it } from "vitest";
import { formatTaskDetail, formatTaskList, statusForIssue } from "./format.js";
import type { KataIssueDetail } from "./types.js";

describe("format helpers", () => {
  it("maps Kata issue status and in_progress labels to pi task statuses", () => {
    expect(statusForIssue({ status: "open", labels: [] })).toBe("pending");
    expect(statusForIssue({ status: "open", labels: ["in_progress"] })).toBe("in_progress");
    expect(statusForIssue({ status: "closed", labels: ["in_progress"] })).toBe("completed");
  });

  it("formats task lists with owner and open blockers", () => {
    const lines = formatTaskList([
      { short_id: "cd34", title: "Blocked task", status: "open", owner: "agent-a", labels: [], blockedBy: ["ab12"] },
      { short_id: "ab12", title: "Active task", status: "open", labels: ["in_progress"], blockedBy: [] },
    ]);

    expect(lines).toBe([
      "ab12 [in_progress] Active task",
      "cd34 [pending] Blocked task (agent-a) [blocked by ab12]",
    ].join("\n"));
  });

  it("strips terminal escape sequences from list rows", () => {
    const lines = formatTaskList([
      {
        short_id: "ab12",
        title: "before\x1b[2Jafter",
        status: "open",
        owner: "click \x1b]8;;https://evil.example/\x1b\\here\x1b]8;;\x1b\\!",
        labels: [],
        blockedBy: [],
      },
    ]);

    expect(lines).toBe("ab12 [pending] beforeafter (click here!)");
  });

  it("keeps list rows on a single line when fields embed newlines", () => {
    const lines = formatTaskList([
      { short_id: "ab12", title: "first\nef56 [pending] forged row", status: "open", labels: [], blockedBy: [] },
    ]);

    expect(lines).toBe("ab12 [pending] first\\nef56 [pending] forged row");
  });

  it("sanitizes detail fields while preserving body newlines", () => {
    const detail: KataIssueDetail = {
      issue: {
        short_id: "ef56",
        title: "safe\u202eevil",
        body: "line one\x1b[31m\nline two",
        status: "open",
        owner: "own\x1b[2Jer",
      },
      labels: ["agent:work\x1b[31mer"],
      comments: [{ author: "a\rgent", body: "done\nLabels: forged" }],
      links: [],
    };

    const text = formatTaskDetail(detail);
    expect(text).toContain("Task ef56: safeevil");
    expect(text).toContain("Owner: owner");
    expect(text).toContain("Description: line one\nline two");
    expect(text).toContain("Labels: agent:worker");
    expect(text).toContain("- agent: done\\nLabels: forged");
  });

  it("formats task details from Kata show output", () => {
    const detail: KataIssueDetail = {
      issue: { uid: "01HZNQ7VFPK1XGD8R5MABCD4EX", short_id: "ef56", title: "Ship plugin", body: "Make it useful", status: "open", owner: "codex" },
      labels: ["agent:worker", "in_progress"],
      comments: [{ author: "codex", body: "Started" }],
      links: [{ type: "blocks", from: { uid: "01HZNQ7VFPK1XGD8R5MABCD4AA", short_id: "ab12" }, to: { uid: "01HZNQ7VFPK1XGD8R5MABCD4EX", short_id: "ef56" } }],
    };

    expect(formatTaskDetail(detail)).toContain("Task ef56: Ship plugin");
    expect(formatTaskDetail(detail)).toContain("Status: in_progress");
    expect(formatTaskDetail(detail)).toContain("Blocked by: ab12");
    expect(formatTaskDetail(detail)).toContain("Labels: agent:worker, in_progress");
  });
});
