--a40bba4fa5b5614efd64b2f5feec69f27a3e67db24521b3d71357b2aa5c4
Content-Disposition: form-data; name="index.js"

var __defProp = Object.defineProperty;
var __name = (target, value) => __defProp(target, "name", { value, configurable: true });

// src/notify.js
async function sendDiscordAlert(env, repo, summary) {
  const url = env.DISCORD_WEBHOOK_URL;
  if (!url) return;
  const color = 15105570;
  const fields = [];
  if (summary.deadDuration) {
    fields.push({ name: "Dead for", value: summary.deadDuration, inline: true });
  }
  if (summary.lastConclusion) {
    fields.push({ name: "Last conclusion", value: summary.lastConclusion, inline: true });
  }
  fields.push({ name: "Restarts today", value: String(summary.restartsToday || 0), inline: true });
  const embed = {
    title: `\u{1F501} ${repo} restarted`,
    color,
    description: summary.reason || "",
    fields,
    footer: { text: "workflow-watchman" },
    timestamp: (/* @__PURE__ */ new Date()).toISOString()
  };
  const payload = {
    embeds: [embed]
  };
  const resp = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload)
  });
  if (!resp.ok) {
    console.error(`[notify] Discord webhook returned ${resp.status}`);
  }
}
__name(sendDiscordAlert, "sendDiscordAlert");
async function sendDiscordError(env, repo, message) {
  const url = env.DISCORD_WEBHOOK_URL;
  if (!url) return;
  const embed = {
    title: `\u26A0\uFE0F ${repo} error`,
    color: 15158332,
    description: message,
    footer: { text: "workflow-watchman" },
    timestamp: (/* @__PURE__ */ new Date()).toISOString()
  };
  await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ embeds: [embed] })
  });
}
__name(sendDiscordError, "sendDiscordError");

// src/dashboard.js
var STATUS_ICON = {
  in_progress: "\u{1F7E2}",
  queued: "\u{1F7E1}",
  pending: "\u{1F7E1}",
  completed: "\u26AA"
};
function renderDashboard(repos) {
  const rows = repos.map((r) => {
    const icon = STATUS_ICON[r.status] || "\u26AA";
    let age = "\u2014";
    if (r.lastRun) {
      const ms = Date.now() - new Date(r.lastRun).getTime();
      if (ms < 6e4) age = "<1m";
      else if (ms < 36e5) age = `${Math.floor(ms / 6e4)}m`;
      else age = `${Math.floor(ms / 36e5)}h ${Math.floor(ms % 36e5 / 6e4)}m`;
    }
    return `<tr>
			<td><strong>${r.name}</strong></td>
			<td>${icon} ${r.status || "no runs"}</td>
			<td>${r.lastRun ? new Date(r.lastRun).toISOString().replace("T", " ").slice(0, 16) + " UTC" : "\u2014"}</td>
			<td>${age}</td>
			<td>${r.restarts ?? 0}</td>
		</tr>`;
  }).join("\n");
  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>workflow-watchman</title>
<style>
* { margin:0; padding:0; box-sizing:border-box; }
body { font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif; background:#0b0d11; color:#cdd0d4; padding:40px 20px; }
h1 { font-size:24px; font-weight:600; margin-bottom:4px; color:#e5e7eb; }
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
</style>
</head>
<body>
<h1>\u{1F6E1} workflow-watchman</h1>
<p class="subtitle">Monitoring ${repos.length} repos \u2022 cron */10 * * * *</p>
<table>
<thead><tr><th>Repo</th><th>Status</th><th>Last Run</th><th>Age</th><th>Restarts</th></tr></thead>
<tbody>${rows}</tbody>
</table>
<p class="meta">
<span>Last check: ${(/* @__PURE__ */ new Date()).toISOString().replace("T", " ").slice(0, 19)} UTC</span>
<span><a href="/__metrics" style="color:#3b82f6;">JSON metrics</a></span>
</p>
</body>
</html>`;
}
__name(renderDashboard, "renderDashboard");
function renderMetrics(repos) {
  return {
    checkedAt: (/* @__PURE__ */ new Date()).toISOString(),
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
          restarts: r.restarts ?? 0
        }
      ])
    )
  };
}
__name(renderMetrics, "renderMetrics");

// src/index.js
var REPO_CONFIGS = [
  { repo: "node-1", workflow: "secure-rdp.yml", branch: "main" },
  { repo: "node-2", workflow: "secure-rdp.yml", branch: "main" },
  { repo: "node-3", workflow: "secure-rdp.yml", branch: "main" },
  { repo: "node-4", workflow: "secure-rdp.yml", branch: "main" },
  { repo: "node-5", workflow: "secure-rdp.yml", branch: "main" },
  { repo: "node-6", workflow: "secure-rdp.yml", branch: "main" },
  { repo: "node-7", workflow: "secure-rdp.yml", branch: "main" },
  { repo: "node-8", workflow: "secure-rdp.yml", branch: "main" },
  { repo: "node-9", workflow: "secure-rdp.yml", branch: "main" },
  { repo: "node-10", workflow: "secure-rdp.yml", branch: "main" },
];
var OWNER = "lawdachuss";
var GRACE_PERIOD = 20 * 60 * 1e3;
var THROTTLE_MAX = 8;
var THROTTLE_WINDOW = 24 * 60 * 60 * 1e3;
var HEALTH_CHECK_RETRIES = 6;
var HEALTH_CHECK_INTERVAL = 3e4;
function ghHeaders(token) {
  return {
    Authorization: `Bearer ${token}`,
    "User-Agent": "workflow-watchman/2.0",
    Accept: "application/vnd.github.v3+json"
  };
}
__name(ghHeaders, "ghHeaders");
function fmtDuration(ms) {
  const h = Math.floor(ms / 36e5);
  const m = Math.floor(ms % 36e5 / 6e4);
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}
__name(fmtDuration, "fmtDuration");
var index_default = {
  async scheduled(event, env, ctx) {
    const token = env.GITHUB_TOKEN;
    if (!token) {
      console.error("GITHUB_TOKEN not set");
      return;
    }
    const headers = ghHeaders(token);
    for (const config of REPO_CONFIGS) {
      try {
        const state = await evaluateRepo(config, headers, env);
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
        headers: { "Content-Type": "application/json" }
      });
    }
    if (url.pathname === "/" || url.pathname === "") {
      const data = await fetchAllStatus(env);
      return new Response(renderDashboard(data), {
        headers: { "Content-Type": "text/html" }
      });
    }
    return new Response("Not found", { status: 404 });
  }
};
async function evaluateRepo(config, headers, env) {
  const { repo, workflow } = config;
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
    restarts: 0
  };
  if (!runs || runs.length === 0) {
    state.needsRestart = true;
    state.reason = "No runs ever";
    return state;
  }
  const activeRun = runs.find((r) => r.status === "in_progress" || r.status === "queued" || r.status === "pending");
  if (activeRun) {
    state.status = activeRun.status;
    state.lastRun = activeRun.run_started_at;
    state.lastConclusion = null;
    return state;
  }
  const latest = runs[0];
  state.status = latest.status;
  state.lastRun = latest.run_started_at;
  state.lastConclusion = latest.conclusion;
  const age = Date.now() - new Date(latest.run_started_at).getTime();
  if (age < GRACE_PERIOD) return state;
  const recentCompletions = runs.filter((r) => {
    if (r.status !== "completed") return false;
    const t = new Date(r.created_at).getTime();
    return t > Date.now() - THROTTLE_WINDOW;
  });
  state.restarts = recentCompletions.length;
  if (recentCompletions.length >= THROTTLE_MAX) {
    state.reason = `Throttled (${recentCompletions.length} runs in 24h)`;
    state.throttled = true;
    return state;
  }
  state.needsRestart = true;
  state.deadDuration = fmtDuration(age);
  state.reason = latest.conclusion ? `Last run ${latest.conclusion} at ${latest.run_started_at}` : "No recent activity";
  return state;
}
__name(evaluateRepo, "evaluateRepo");
async function executeRestart(config, state, headers, env) {
  const { repo, workflow, branch } = config;
  console.log(`[${repo}] restarting \u2014 ${state.reason}`);
  const body = JSON.stringify({ ref: branch, inputs: { triggered_by: "workflow-watchman" } });
  const dispatchResp = await fetch(
    `https://api.github.com/repos/${OWNER}/${repo}/actions/workflows/${workflow}/dispatches`,
    {
      method: "POST",
      headers: { ...headers, "Content-Type": "application/json" },
      body
    }
  );
  if (!dispatchResp.ok) throw new Error(`dispatch API: ${dispatchResp.status}`);
  console.log(`[${repo}] dispatched`);
  await sendDiscordAlert(env, repo, {
    reason: state.reason,
    deadDuration: state.deadDuration,
    lastConclusion: state.lastConclusion,
    restartsToday: state.restarts + 1
  });
  const healthOk = await healthCheck(config, headers);
  if (!healthOk) {
    await sendDiscordError(env, repo, "Health check failed \u2014 new run did not start within 3 min");
  } else {
    console.log(`[${repo}] health check passed`);
  }
}
__name(executeRestart, "executeRestart");
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
    const age = Date.now() - new Date(latest.created_at).getTime();
    if (age < HEALTH_CHECK_INTERVAL * (i + 1) + 1e4) {
      if (latest.status === "in_progress" || latest.status === "queued" || latest.status === "pending") {
        console.log(`[${repo}] health check OK \u2014 run ${latest.id} ${latest.status}`);
        return true;
      }
    }
  }
  return false;
}
__name(healthCheck, "healthCheck");
async function fetchAllStatus(env) {
  const token = env.GITHUB_TOKEN;
  if (!token) return [];
  const headers = ghHeaders(token);
  const results = [];
  for (const config of REPO_CONFIGS) {
    try {
      const state = await evaluateRepo(config, headers, env);
      results.push(state);
    } catch {
      results.push({ name: config.repo, status: "error" });
    }
  }
  return results;
}
__name(fetchAllStatus, "fetchAllStatus");
export {
  index_default as default
};
//# sourceMappingURL=index.js.map

--a40bba4fa5b5614efd64b2f5feec69f27a3e67db24521b3d71357b2aa5c4--
