package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/teacat/chaturbate-dvr/database"
)

func loadEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

// authedGet performs an authenticated GET against the Supabase REST API.
func authedGet(client *database.Client, path string) ([]byte, int, error) {
	req, err := http.NewRequest("GET", client.URL+"/rest/v1"+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("apikey", client.APIKey)
	req.Header.Set("Authorization", "Bearer "+client.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

func checkReachability(rawURL string) string {
	if rawURL == "" {
		return "n/a"
	}
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", rawURL, nil)
	resp, err := client.Do(req)
	if err != nil {
		return "UNREACHABLE (" + err.Error() + ")"
	}
	defer resp.Body.Close()
	return fmt.Sprintf("HTTP %d", resp.StatusCode)
}

// fetchNodeLogs retrieves the in-memory log buffer from a node's /api/logs endpoint.
func fetchNodeLogs(baseURL string, lines int) string {
	if baseURL == "" {
		return ""
	}
	req, err := http.NewRequest("GET", strings.TrimRight(baseURL, "/")+"/api/logs?lines="+fmt.Sprint(lines), nil)
	if err != nil {
		return ""
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "(unreachable: " + err.Error() + ")"
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("(HTTP %d)", resp.StatusCode)
	}
	var payload struct {
		Logs []struct {
			Time    string `json:"time"`
			Message string `json:"message"`
		} `json:"logs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "(decode error: " + err.Error() + ")"
	}
	if len(payload.Logs) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, l := range payload.Logs {
		sb.WriteString(fmt.Sprintf("%s  %s\n", l.Time, l.Message))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

func main() {
	loadEnv(".env")
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseAPIKey := os.Getenv("SUPABASE_API_KEY")
	if supabaseURL == "" || supabaseAPIKey == "" {
		fmt.Println("ERROR: SUPABASE_URL and SUPABASE_API_KEY must be set in .env")
		os.Exit(1)
	}

	client := database.NewClient(supabaseURL, supabaseAPIKey)

	// ---- 1. Nodes + their trycloudflare web URLs ----
	nodes, err := client.GetAllNodes()
	if err != nil {
		fmt.Printf("ERROR fetching nodes: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("=== NODES (%d) ===\n", len(nodes))
	for _, n := range nodes {
		isTC := strings.Contains(n.WebURL, "trycloudflare.com")
		tcTag := ""
		if n.WebURL != "" {
			if isTC {
				tcTag = " [trycloudflare]"
			} else {
				tcTag = " [other-url]"
			}
		}
		reach := ""
		if isTC {
			reach = " -> " + checkReachability(n.WebURL)
		}
		fmt.Printf("  %-28s status=%-9s load=%-3d heartbeat=%s%s%s\n",
			n.NodeID, n.Status, n.CurrentLoad, n.LastHeartbeat, tcTag, reach)
		if n.WebURL != "" {
			fmt.Printf("      web_url: %s\n", n.WebURL)
		}
		// Pull live logs from this node's tunnel endpoint. Only surface
		// lines that indicate a real problem (WARN/ERROR/panic/fatal) and
		// A/V-relevant lines, so the per-node dump stays readable.
		if isTC {
			logs := fetchNodeLogs(n.WebURL, 80)
			if logs == "" {
				fmt.Printf("      logs: (none / unreachable)\n")
			} else {
				var issue strings.Builder
				for _, l := range strings.Split(logs, "\n") {
					up := strings.ToUpper(l)
					if strings.Contains(up, "WARN") || strings.Contains(up, "ERROR") ||
						strings.Contains(up, "PANIC") || strings.Contains(up, "FATAL") ||
						strings.Contains(up, "REALIGN") || strings.Contains(up, "START OFFSET") ||
						strings.Contains(up, "MUX:") || strings.Contains(up, "SYNCED") {
						issue.WriteString(l)
						issue.WriteString("\n")
					}
				}
				if issue.Len() == 0 {
					fmt.Printf("      logs: (no WARN/ERROR/panic in last 80 lines)\n")
				} else {
					fmt.Printf("      logs (issues):\n%s", indent(strings.TrimRight(issue.String(), "\n"), "        "))
				}
			}
		}
	}

	// ---- 2. Tunnels (active) ----
	body, status, err := authedGet(client, "/tunnels?is_active=eq.true&order=created_at.desc&limit=100")
	if err != nil {
		fmt.Printf("\nERROR fetching tunnels: %v\n", err)
	} else if status >= 400 {
		fmt.Printf("\nERROR fetching tunnels: HTTP %d %s\n", status, string(body))
	} else {
		var tunnels []database.Tunnel
		_ = json.Unmarshal(body, &tunnels)
		fmt.Printf("\n=== ACTIVE TUNNELS (%d) ===\n", len(tunnels))
		for _, t := range tunnels {
			isTC := strings.Contains(t.URL, "trycloudflare.com")
			tag := ""
			if isTC {
				tag = " [trycloudflare]"
			}
			reach := ""
			if isTC {
				reach = " -> " + checkReachability(t.URL)
			}
			fmt.Printf("  instance=%-20s created=%s%s%s\n", t.InstanceID, t.CreatedAt, tag, reach)
			fmt.Printf("      url: %s\n", t.URL)
		}
	}

	// ---- 3. Persisted logs (now in Cloudflare, not Supabase) ----
	// The autopilot worker no longer writes logs to Supabase `channel_logs`
	// (that table grew to ~1.5M rows / 700MB and blew the usage quota). Logs
	// now live in the Cloudflare Worker KV ring buffer (binding AUTOPILOT,
	// key "autopilot_log") and in Workers Logs (`wrangler tail`). Fetch them
	// with:
	//   npx wrangler kv key get --binding=AUTOPILOT autopilot_log
	//   npx wrangler tail
	fmt.Println("\n=== LOGS ===")
	fmt.Println("  Node/persisted logs moved off Supabase to Cloudflare KV + Workers Logs.")
	fmt.Println("  View with: npx wrangler kv key get --binding=AUTOPILOT autopilot_log")
	fmt.Println("             npx wrangler tail")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
