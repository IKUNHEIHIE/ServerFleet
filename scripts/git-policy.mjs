import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

export const validTitle = title => /^(feat|fix|docs|chore|test|ci|refactor|build|perf|revert)(\([a-z0-9-]+\))?!?: .+/.test(title) && !/[\r\n]/.test(title);
export const validBranch = branch => /^(main|(feature|fix|contract|integration|docs|chore)\/[a-z0-9][a-z0-9._-]*)$/.test(branch);

export function checkGit() {
  const git = args => execFileSync("git", args, { encoding: "utf8" }).trim();
  const event = process.env.GITHUB_EVENT_PATH ? JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, "utf8")) : {};
  const branch = event.pull_request?.head.ref ?? git(["branch", "--show-current"]);
  if (!validBranch(branch)) throw new Error(`Invalid branch name: ${branch}`);
  if (event.pull_request && !validTitle(event.pull_request.title)) throw new Error("PR title must use Conventional Commits");
  const base = event.pull_request?.base.sha ?? process.env.CHECK_BASE;
  const head = event.pull_request?.head.sha ?? "HEAD";
  const args = base ? ["log", "--format=%s", `${base}..${head}`, "--no-merges"] : ["log", "-1", "--format=%s"];
  const titles = git(args).split("\n").filter(Boolean);
  for (const title of titles) if (!validTitle(title)) throw new Error(`Invalid commit title: ${title}`);
  console.log(`PASS: branch ${branch}, ${titles.length} commit title(s)${event.pull_request ? ", PR title" : "; remote PR not available locally"}`);
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) checkGit();
