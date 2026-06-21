// repocache:managed — installed by `repocache init`. Safe to delete; repocache
// will reinstall it on the next `init`, or run `repocache uninstall` to remove.
//
// opencode has no SessionStart shell-command hook like Claude/Codex/Gemini and
// no per-directory access allowlist, so repocache integrates as a plugin that
// shells back to the `repocache` binary. The plugin module body runs once when
// opencode loads it at startup — that is our "session start":
//
//   - kick off the background cache refresh (`repocache __bg-sync`), and
//   - snapshot the guide (`repocache __session-context --text`) to inject into
//     the model's system prompt via experimental.chat.system.transform.
//
// All logic lives in the binary, so this file never needs to change across
// repocache upgrades.

export const RepocachePlugin = async ({ $, directory }) => {
  // Non-blocking background refresh, like the SessionStart bg-sync hook the
  // other agents get. Errors are ignored so a stale/uninstalled binary can
  // never break the session.
  $`repocache __bg-sync`.catch(() => {})

  // Snapshot the guide once at startup (matches how the other agents snapshot
  // the repo-list at hook time). Run in the project directory so the
  // cwd-collision warning resolves against the right repo.
  let guide = ""
  try {
    guide = (await $`repocache __session-context --text`.cwd(directory).quiet().text()).trim()
  } catch {
    guide = ""
  }

  return {
    "experimental.chat.system.transform": async (_input, output) => {
      if (guide) output.system.push(guide)
    },
  }
}
