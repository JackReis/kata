import type { KataIssue, KataIssueDetail, KataLink, TaskStatus } from "./types.js";

// Mirrors kata's internal/textsafe: `kata --json` deliberately returns raw
// daemon bytes, so escape stripping is this plugin's responsibility at its
// human-facing text boundary. CSI and OSC are the families agent-authored
// text can realistically reach a terminal with; other escape introducers
// are removed by the control-rune strip below.
const ANSI_ESCAPE_PATTERN = /\x1b\[[0-9;?]*[a-zA-Z]|\x1b\](?:[^\x07\x1b]|\x1b[^\\])*(?:\x07|\x1b\\)/g;
// Cc covers C0/C1 controls; Cf covers invisible format runes such as
// U+202E RIGHT-TO-LEFT OVERRIDE that can visually reorder rendered text.
const CONTROL_OR_FORMAT_PATTERN = /[\p{Cc}\p{Cf}]/gu;

export function sanitizeBlock(text: string): string {
  if (!text) return text;
  return text
    .replace(ANSI_ESCAPE_PATTERN, "")
    .replace(CONTROL_OR_FORMAT_PATTERN, (rune) => (rune === "\n" || rune === "\t" ? rune : ""));
}

export function sanitizeLine(text: string): string {
  if (!text) return text;
  return sanitizeBlock(text).replaceAll("\n", "\\n").replaceAll("\t", " ");
}

export function statusForIssue(issue: Pick<KataIssue, "status" | "labels">): TaskStatus {
  if (issue.status === "closed") return "completed";
  if ((issue.labels ?? []).includes("in_progress")) return "in_progress";
  return "pending";
}

export function issueRef(issue: Pick<KataIssue, "short_id" | "qualified_id" | "uid" | "number">): string {
  return issue.short_id ?? issue.qualified_id ?? issue.uid ?? (issue.number !== undefined ? `#${issue.number}` : "unknown");
}

export function formatTaskList(issues: KataIssue[]): string {
  if (issues.length === 0) return "No tasks found";
  return [...issues]
    .sort((a, b) => issueRef(a).localeCompare(issueRef(b)))
    .map((issue) => {
      let line = `${sanitizeLine(issueRef(issue))} [${statusForIssue(issue)}] ${sanitizeLine(issue.title)}`;
      if (issue.owner) line += ` (${sanitizeLine(issue.owner)})`;
      const blockedBy = issue.blockedBy ?? issue.blocked_by?.map((peer) => peer.short_id) ?? [];
      if (blockedBy.length > 0) {
        line += ` [blocked by ${blockedBy.map(sanitizeLine).join(", ")}]`;
      }
      return line;
    })
    .join("\n");
}

export function formatTaskDetail(detail: KataIssueDetail): string {
  const issue = { ...detail.issue, labels: detail.labels };
  const blockedBy = blockersFor(issue, detail.links);
  const blocks = blocksFor(issue, detail.links);
  const lines = [
    `Task ${sanitizeLine(issueRef(issue))}: ${sanitizeLine(issue.title)}`,
    `Status: ${statusForIssue(issue)}`,
  ];
  if (issue.owner) lines.push(`Owner: ${sanitizeLine(issue.owner)}`);
  if (issue.body) lines.push(`Description: ${sanitizeBlock(issue.body)}`);
  if (blockedBy.length > 0) lines.push(`Blocked by: ${blockedBy.map(sanitizeLine).join(", ")}`);
  if (blocks.length > 0) lines.push(`Blocks: ${blocks.map(sanitizeLine).join(", ")}`);
  if (detail.labels.length > 0) lines.push(`Labels: ${detail.labels.map(sanitizeLine).join(", ")}`);
  if (detail.comments.length > 0) {
    lines.push("Comments:");
    for (const comment of detail.comments) {
      lines.push(`- ${sanitizeLine(comment.author ?? "unknown")}: ${sanitizeLine(comment.body)}`);
    }
  }
  return lines.join("\n");
}

export function blockersFor(issue: Pick<KataIssue, "uid" | "short_id" | "number">, links: KataLink[]): string[] {
  return links
    .filter((link) => link.type === "blocks" && linkTargetsIssue(link, issue))
    .map((link) => link.from?.short_id ?? (link.from_number !== undefined ? `#${link.from_number}` : undefined))
    .filter((ref): ref is string => Boolean(ref))
    .sort();
}

export function blocksFor(issue: Pick<KataIssue, "uid" | "short_id" | "number">, links: KataLink[]): string[] {
  return links
    .filter((link) => link.type === "blocks" && linkStartsFromIssue(link, issue))
    .map((link) => link.to?.short_id ?? (link.to_number !== undefined ? `#${link.to_number}` : undefined))
    .filter((ref): ref is string => Boolean(ref))
    .sort();
}

function linkTargetsIssue(link: KataLink, issue: Pick<KataIssue, "uid" | "short_id" | "number">): boolean {
  if (issue.uid && link.to?.uid) return link.to.uid === issue.uid;
  if (issue.short_id && link.to?.short_id) return link.to.short_id === issue.short_id;
  return issue.number !== undefined && link.to_number === issue.number;
}

function linkStartsFromIssue(link: KataLink, issue: Pick<KataIssue, "uid" | "short_id" | "number">): boolean {
  if (issue.uid && link.from?.uid) return link.from.uid === issue.uid;
  if (issue.short_id && link.from?.short_id) return link.from.short_id === issue.short_id;
  return issue.number !== undefined && link.from_number === issue.number;
}
