import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { validBranch } from "./git-policy.mjs";

export function taskPath(root, branch) {
  if (branch === "main" || !validBranch(branch)) throw new Error("Expected a named task branch");
  const parent = path.resolve(root, ".worktrees");
  const target = path.resolve(parent, branch.replace("/", "--"));
  if (path.dirname(target) !== parent) throw new Error("Worktree path escapes task directory");
  return target;
}

export function manage(args) {
  const git = more => execFileSync("git", more, { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
  const root = git(["rev-parse", "--show-toplevel"]);
  const [action, branch, base = "main"] = args;
  if (action === "list") { console.log(git(["worktree", "list"])); return; }
  const target = taskPath(root, branch ?? "");
  if (action === "create") {
    if (existsSync(target)) throw new Error(`Target already exists: ${target}`);
    git(["rev-parse", "--verify", `${base}^{commit}`]);
    mkdirSync(path.dirname(target), { recursive: true });
    console.log(git(["worktree", "add", "-b", branch, target, base]));
    console.log(`Assigned directory: ${target}\nRecord owner and allowed paths in docs/development/TASK-BOARD.md before starting an Agent.`);
  } else if (action === "remove") {
    if (!existsSync(target)) throw new Error("Worktree directory does not exist");
    if (git(["-C", target, "status", "--porcelain", "--untracked-files=all", "--ignored"])) throw new Error("Worktree contains changed, untracked or ignored files. Preserve/remove task artifacts before cleanup.");
    const actual = git(["-C", target, "branch", "--show-current"]);
    if (actual !== branch) throw new Error("Worktree branch mismatch");
    git(["merge-base", "--is-ancestor", branch, base]);
    // Git handles removal; there is no shell-built recursive delete and no force.
    console.log(git(["worktree", "remove", target]));
    console.log("Removed clean, merged worktree. Branch retained for review.");
  } else throw new Error("Usage: node scripts/worktree.mjs create|remove <branch> [base] OR list");
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) manage(process.argv.slice(2));
