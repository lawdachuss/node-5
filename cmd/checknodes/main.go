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
		// Pull live logs from this node's tunnel endpoint.
		if isTC {
			logs := fetchNodeLogs(n.WebURL, 25)
			if logs == "" {
				fmt.Printf("      logs: (none / unreachable)\n")
			} else {
				fmt.Printf("      logs:\n%s\n", indent(logs, "        "))
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

	// ---- 3. Persisted channel_logs (errors across all nodes) ----
	body, status, err = authedGet(client, "/channel_logs?select=count&order=created_at.desc")
	if err == nil && status < 400 {
		fmt.Printf("\n=== channel_logs total rows: %s ===\n", strings.TrimSpace(string(body)))
	}
	// Error-level logs across every node (the thing you actually want to watch).
	body, status, err = authedGet(client, "/channel_logs?log_level=eq.error&order=created_at.desc&limit=40")
	if err != nil {
		fmt.Printf("\nERROR fetching error logs: %v\n", err)
	} else if status >= 400 {
		fmt.Printf("\nERROR fetching error logs: HTTP %d %s\n", status, string(body))
	} else {
		var logs []database.ChannelLog
		_ = json.Unmarshal(body, &logs)
		fmt.Printf("\n=== PERSISTED ERROR LOGS (%d) ===\n", len(logs))
		for _, l := range logs {
			fmt.Printf("  %s node=%-12s user=%-20s %s\n", l.CreatedAt, l.NodeID, l.Username, truncate(l.Message, 110))
		}
		if len(logs) == 0 {
			fmt.Println("  (none)")
		}
	}
	// Most recent raw logs (any level) for context.
	body, status, err = authedGet(client, "/channel_logs?order=created_at.desc&limit=20")
	if err != nil {
		fmt.Printf("\nERROR fetching recent logs: %v\n", err)
	} else if status >= 400 {
		fmt.Printf("\nERROR fetching recent logs: HTTP %d %s\n", status, string(body))
	} else {
		var logs []database.ChannelLog
		_ = json.Unmarshal(body, &logs)
		fmt.Printf("\n=== RECENT PERSISTED LOGS (%d) ===\n", len(logs))
		for _, l := range logs {
			fmt.Printf("  %s %-7s node=%-12s user=%-18s %s\n", l.CreatedAt, l.LogLevel, l.NodeID, l.Username, truncate(l.Message, 90))
		}
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
