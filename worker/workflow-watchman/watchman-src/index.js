/**
 * workflow-watchman — Cloudflare Worker
 * ---------------------------------------
 * Monitors GitHub Actions workflows across node-1..node-10 repos.
 * Restarts any workflow that has been inactive/dead longer than its grace period.
 *
 * Repo-specific behaviour:
 *   - node-1..node-10 (secure-rdp.yml):  20 min grace,  8 restarts / 24h max
 */

const OWNER = "lawdachuss";

// ===== Per-repo configuration ================================================
const REPO_CONFIGS = [
  // Node repos — standard behaviour
  { repo: "node-1",   workflow: "secure-rdp.yml", branch: "main", graceMs: 20 * 60 * 1000, throttleMax: 8 },
  { repo: "node-2",   workflow: "secure-rdp.yml", branch: "main", graceMs: 20 * 60 * 1000, throttleMax: 8 },
  { repo: "node-3",   workflow: "secure-rdp.yml", branch: "main", graceMs: 20 * 60 * 1000, throttleMax: 8 },
  { repo: "node-4",   workflow: "secure-rdp.yml", branch: "main", graceMs: 20 * 60 * 1000, throttleMax: 8 },
  { repo: "node-5",   workflow: "secure-rdp.yml", branch: "main", graceMs: 20 * 60 * 1000, throttleMax: 8 },
  { repo: "node-6",   workflow: "secure-rdp.yml", branch: "main", graceMs: 20 * 60 * 1000, throttleMax: 8 },
  { repo: "node-7",   workflow: "secure-rdp.yml", branch: "main", graceMs: 20 * 60 * 1000, throttleMax: 8 },
  { repo: "node-8",   workflow: "secure-rdp.yml", branch: "main", graceMs: 20 * 60 * 1000, throttleMax: 8 },
  { repo: "node-9",   workflow: "secure-rdp.yml", branch: "main", graceMs: 20 * 60 * 1000, throttleMax: 8 },
  { repo: "node-10",  workflow: "secure-rdp.yml", branch: "main", graceMs: 20 * 60 * 1000, throttleMax: 8 },
];

const THROTTLE_WINDOW = 24 * 60 * 60 * 1000; // 24 hours
const HEALTH_CHECK_RETRIES = 6;
const HEALTH_CHECK_INTERVAL = 30 * 1000; // 30 seconds

// ===== Helpers ===============================================================

function ghHeaders(token) {
  return {
    Authorization: `Bearer ${token}`,
    "User-Agent": "workflow-watchman/2.0",
    Accept: "application/vnd.github.v3+json",
  };
}

function fmtDuration(ms) {
  const h = Math.floor(ms / 3600000);
  const m = Math.floor((ms % 3600000) / 60000);
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

// ===== Discord notifications =================================================

async function sendDiscordAlert(env, repo, summary) {
  const url = env.DISCORD_WEBHOOK_URL;
  if (!url) return;
  const fields = [];
  if (summary.deadDuration) fields.push({ name: "Dead for", value: summary.deadDuration, inline: true });
  if (summary.lastConclusion) fields.push({ name: "Last conclusion", value: summary.lastConclusion, inline: true });
  fields.push({ name: "Restarts today", value: String(summary.restartsToday || 0), inline: true });
  const resp = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      embeds: [{
        title: `🔄 ${repo} restarted`,
        color: 15105570,
        description: summary.reason || "",
        fields,
        footer: { text: "workflow-watchman" },
        timestamp: new Date().toISOString(),
      }],
    }),
  });
  if (!resp.ok) console.error(`[notify] Discord webhook returned ${resp.status}`);
}

async function sendDiscordError(env, repo, message) {
  const url = env.DISCORD_WEBHOOK_URL;
  if (!url) return;
  await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      embeds: [{
        title: `⚠️ ${repo} error`,
        color: 15158332,
        description: message,
        footer: { text: "workflow-watchman" },
        timestamp: new Date().toISOString(),
      }],
    }),
  });
}

// ===== Dashboard / Metrics ===================================================

const STATUS_ICON = {
  in_progress: "🟢",
  queued: "🟡",
  pending: "🟡",
  completed: "⚪",
};

function renderDashboard(repos) {
  const rows = repos
    .map((r) => {
      const icon = STATUS_ICON[r.status] || "⚪";
      let age = "—";
      if (r.lastRun) {
        const ms = Date.now() - new Date(r.lastRun).getTime();
        if (ms < 60000) age = "<1m";
        else if (ms < 3600000) age = `${Math.floor(ms / 60000)}m`;
        else age = `${Math.floor(ms / 3600000)}h ${Math.floor((ms % 3600000) / 60000)}m`;
      }
      return `<tr>
        <td><strong>${r.name}</strong></td>
        <td>${icon} ${r.status || "no runs"}</td>
        <td>${r.lastRun ? new Date(r.lastRun).toISOString().replace("T", " ").slice(0, 16) + " UTC" : "—"}</td>
        <td>${age}</td>
        <td>${r.restarts ?? 0}</td>
      </tr>`;
    })
    .join("\n");
  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>workflow-watchman</title>
<style>
* { margin:0; padding:0; box-sizing:border-box; }
body { font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif; background:#0b0d11; color:#cdd0d4; padding:40px 20px; }
h1 { font-size:24px; font-weight:600; margin-bottom:4px; color:#e5ebf0; }
.subtitle { color:#6b7280; font-size:14px; margin-bottom:24px; }
table { width:100%; border-collapse:collapse; background:#13161b; border-radius:8px; overflow:hidden; }
th { text-align:left; padding:12px 16px; font-size:12px; text-transform:uppercase; letter-spacing:0.05em; color:#9ca3af; border-bottom:1px solid #1f2937; }
td { padding:12px 16px; border-bottom:1px solid #1a1f26; font-size:14px; }
tr:last-child td { border-bottom:none; }
tr:hover td { background:#1a1f26; }
.badge { display:inline-block; padding:2px 8px; border-radius:4px; font-size:11px; font-weight:600; }
.badge-ok { background:#065f46; color:#6ee7b7; }
.badge-warn { background:#78350f; color:#fcd34d; }
.badge-err { background:#7f1d1d; color:#fca5a5; }
.meta { margin-top:16px; font-size:12px; color:#6b7280; }
.meta span { margin-right:16px; }
a { color:#3b82f6; }
</style>
</head>
<body>
<h1>🛡️ workflow-watchman</h1>
<p class="subtitle">Monitoring ${repos.length} repos • cron */10 * * * *</p>
<table>
<thead><tr><th>Repo</th><th>Status</th><th>Last Run</th><th>Age</th><th>Restarts</th></tr></thead>
<tbody>${rows}</tbody>
</table>
<p class="meta">
<span>Last check: ${new Date().toISOString().replace("T", " ").slice(0, 19)} UTC</span>
<span><a href="/__metrics">JSON metrics</a></span>
</p>
</body>
</html>`;
}

function renderMetrics(repos) {
  return {
    checkedAt: new Date().toISOString(),
    totalRepos: repos.length,
    running: repos.filter((r) => r.status === "in_progress").length,
    idle: repos.filter((r) => !r.status || r.status === "completed").length,
    repos: Object.fromEntries(
      repos.map((r) => [
        r.name,
        {
          status: r.status || "no_runs",
          lastRun: r.lastRun,
          lastConclusion: r.lastConclusion,
          restarts: r.restarts ?? 0,
        },
      ])
    ),
  };
}

// ===== Core logic ============================================================

async function evaluateRepo(config, headers) {
  const { repo, workflow, graceMs, throttleMax } = config;
  const resp = await fetch(
    `https://api.github.com/repos/${OWNER}/${repo}/actions/workflows/${workflow}/runs?per_page=5`,
    { headers }
  );
  if (!resp.ok) throw new Error(`runs API: ${resp.status}`);

  const { workflow_runs: runs } = await resp.json();
  const state = {
    name: repo,
    needsRestart: false,
    status: null,
    lastRun: null,
    lastConclusion: null,
    deadDuration: null,
    restarts: 0,
  };

  // No runs ever → needs restart
  if (!runs || runs.length === 0) {
    state.needsRestart = true;
    state.reason = "No runs ever";
    return state;
  }

  // Active run → no restart needed
  const activeRun = runs.find(
    (r) => r.status === "in_progress" || r.status === "queued" || r.status === "pending"
  );
  if (activeRun) {
    state.status = activeRun.status;
    state.lastRun = activeRun.run_started_at;
    state.lastConclusion = null;
    return state;
  }

  // Latest completed run
  const latest = runs[0];
  state.status = latest.status;
  state.lastRun = latest.run_started_at;
  state.lastConclusion = latest.conclusion;
  const age = Date.now() - new Date(latest.run_started_at).getTime();

  // Grace period check
  if (age < graceMs) return state;

  // Throttle check — count completions within the 24h window from fetched runs
  const recentCompletions = runs.filter((r) => {
    if (r.status !== "completed") return false;
    return new Date(r.created_at).getTime() > Date.now() - THROTTLE_WINDOW;
  });
  state.restarts = recentCompletions.length;
  if (recentCompletions.length >= throttleMax) {
    state.reason = `Throttled (${recentCompletions.length} runs in 24h)`;
    state.throttled = true;
    return state;
  }

  // Needs restart
  state.needsRestart = true;
  state.deadDuration = fmtDuration(age);
  state.reason = latest.conclusion
    ? `Last run ${latest.conclusion} at ${latest.run_started_at}`
    : "No recent activity";
  return state;
}

async function executeRestart(config, state, headers, env) {
  const { repo, workflow, branch } = config;
  console.log(`[${repo}] restarting — ${state.reason}`);

  // Backfill dispatch needs just the ref; node repos pass extra inputs
  const body =
    false
      ? JSON.stringify({ ref: branch })
      : JSON.stringify({ ref: branch, inputs: { triggered_by: "workflow-watchman" } });

  const dispatchResp = await fetch(
    `https://api.github.com/repos/${OWNER}/${repo}/actions/workflows/${workflow}/dispatches`,
    {
      method: "POST",
      headers: { ...headers, "Content-Type": "application/json" },
      body,
    }
  );
  if (!dispatchResp.ok) throw new Error(`dispatch API: ${dispatchResp.status}`);

  console.log(`[${repo}] dispatched`);
  await sendDiscordAlert(env, repo, {
    reason: state.reason,
    deadDuration: state.deadDuration,
    lastConclusion: state.lastConclusion,
    restartsToday: state.restarts + 1,
  });

  // Health check — poll GitHub until the new run appears
  const healthOk = await healthCheck(config, headers);
  if (!healthOk) {
    await sendDiscordError(env, repo, "Health check failed — new run did not start within 3 min");
  } else {
    console.log(`[${repo}] health check passed`);
  }
}

async function healthCheck(config, headers) {
  const { repo, workflow } = config;
  for (let i = 0; i < HEALTH_CHECK_RETRIES; i++) {
    await new Promise((r) => setTimeout(r, HEALTH_CHECK_INTERVAL));
    const resp = await fetch(
      `https://api.github.com/repos/${OWNER}/${repo}/actions/workflows/${workflow}/runs?per_page=1`,
      { headers }
    );
    if (!resp.ok) continue;
    const { workflow_runs: runs } = await resp.json();
    if (!runs || runs.length === 0) continue;
    const latest = runs[0];
    const runAge = Date.now() - new Date(latest.created_at).getTime();
    if (runAge < HEALTH_CHECK_INTERVAL * (i + 1) + 10000) {
      if (latest.status === "in_progress" || latest.status === "queued" || latest.status === "pending") {
        console.log(`[${repo}] health check OK — run ${latest.id} ${latest.status}`);
        return true;
      }
    }
  }
  return false;
}

async function fetchAllStatus(env) {
  const token = env.GITHUB_TOKEN;
  if (!token) return [];
  const headers = ghHeaders(token);
  const results = [];
  for (const config of REPO_CONFIGS) {
    try {
      const state = await evaluateRepo(config, headers);
      results.push(state);
    } catch {
      results.push({ name: config.repo, status: "error" });
    }
  }
  return results;
}

// ===== Worker handlers =======================================================

export default {
  async scheduled(event, env, ctx) {
    const token = env.GITHUB_TOKEN;
    if (!token) {
      console.error("GITHUB_TOKEN not set");
      return;
    }
    const headers = ghHeaders(token);
    for (const config of REPO_CONFIGS) {
      try {
        const state = await evaluateRepo(config, headers);
        if (state.needsRestart) {
          await executeRestart(config, state, headers, env);
        }
      } catch (err) {
        console.error(`[${config.repo}] error: ${err.message}`);
        await sendDiscordError(env, config.repo, err.message);
      }
    }
  },

  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    if (url.pathname === "/__health") return new Response("OK", { status: 200 });
    if (url.pathname === "/__metrics") {
      const data = await fetchAllStatus(env);
      return new Response(JSON.stringify(renderMetrics(data), null, 2), {
        headers: { "Content-Type": "application/json" },
      });
    }
    if (url.pathname === "/" || url.pathname === "") {
      const data = await fetchAllStatus(env);
      return new Response(renderDashboard(data), {
        headers: { "Content-Type": "text/html" },
      });
    }
    return new Response("Not found", { status: 404 });
  },
};
