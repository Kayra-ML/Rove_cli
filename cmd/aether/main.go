package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/Kayra-ML/rove/internal/daemon"
	"github.com/Kayra-ML/rove/pkg/client"
	"github.com/Kayra-ML/rove/pkg/protocol"
)

func main() {
	// No args or "desktop" → desktop app. "tui" is the terminal cockpit.
	if len(os.Args) < 2 || os.Args[1] == "desktop" {
		extra := []string{}
		if len(os.Args) > 2 {
			extra = os.Args[2:]
		}
		launchTUI(append([]string{"desktop"}, extra...))
		return
	}
	if os.Args[1] == "tui" {
		tuiArgs := []string{}
		if len(os.Args) > 2 {
			tuiArgs = os.Args[2:]
		}
		launchTUI(append([]string{"tui"}, tuiArgs...))
		return
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "version":
		fmt.Println("rovecode 0.1.2")
	case "daemon":
		runDaemon(args)
	case "ping":
		mustCall(protocol.MethodPing, nil)
	case "agent":
		handleAgent(args)
	case "session":
		handleSession(args)
	case "chat":
		handleChat(args)
	case "card":
		handleCard(args)
	case "goal":
		handleGoal(args)
	case "workspace":
		handleWorkspace(args)
	case "term":
		handleTerm(args)
	case "skill":
		handleSkill(args)
	case "market":
		handleMarket(args)
	case "provider":
		handleProvider(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %s\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Print(`rovecode — CLI for the Rove Code daemon

commands:
  (none) | desktop | tui
  version
  daemon start|stop
  ping
  agent list
  session list|new [title]
  chat <sessionId> <message...>
  card list|create <title>|move <id> <column>|dispatch <id>
  goal list|create <title>|drive <id>
  workspace list|open <path>
  term list|spawn
  skill list|install <dir>
  market list|publish <dir>|install <name> <version>
  provider list
`)
}

func c() *client.Client {
	cl, err := client.FromEnv()
	if err != nil {
		fatal(err)
	}
	return cl
}

func mustCall(method string, params any) json.RawMessage {
	res, err := c().Call(method, params)
	if err != nil {
		fatal(err)
	}
	fmt.Println(pretty(res))
	return res
}

func pretty(b json.RawMessage) string {
	if len(b) == 0 {
		return "null"
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		return string(b)
	}
	return buf.String()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func runDaemon(args []string) {
	if len(args) == 0 {
		args = []string{"start"}
	}
	cfg, err := config.Load("")
	if err != nil {
		fatal(err)
	}
	switch args[0] {
	case "start":
		cmd := daemon.Command()
		if cmd == nil {
			fatal(fmt.Errorf("rovecode daemon binary not found"))
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()
		if err := cmd.Start(); err != nil {
			fatal(err)
		}
		fmt.Printf("started rovecode daemon pid=%d\n", cmd.Process.Pid)
	case "stop":
		b, err := os.ReadFile(daemon.PIDPath(cfg.DataDir))
		if err != nil {
			fatal(err)
		}
		fmt.Printf("stop pid %s (send SIGTERM)\n", strings.TrimSpace(string(b)))
	default:
		fatal(fmt.Errorf("daemon start|stop"))
	}
}

func handleAgent(args []string) {
	mustCall(protocol.MethodAgentList, nil)
}

func handleSession(args []string) {
	if len(args) == 0 || args[0] == "list" {
		mustCall(protocol.MethodSessionList, map[string]any{})
		return
	}
	if args[0] == "new" {
		title := "session"
		if len(args) > 1 {
			title = strings.Join(args[1:], " ")
		}
		mustCall(protocol.MethodSessionCreate, map[string]any{"title": title})
	}
}

func handleChat(args []string) {
	if len(args) < 2 {
		fatal(fmt.Errorf("chat <sessionId> <message>"))
	}
	agentsRaw, err := c().Call(protocol.MethodAgentList, nil)
	if err != nil {
		fatal(err)
	}
	var agents []struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(agentsRaw, &agents)
	agentID := ""
	if len(agents) > 0 {
		agentID = agents[0].ID
	}
	mustCall(protocol.MethodSessionSend, map[string]any{
		"sessionId": args[0],
		"agentId":   agentID,
		"content":   strings.Join(args[1:], " "),
	})
}

func handleCard(args []string) {
	if len(args) == 0 || args[0] == "list" {
		mustCall(protocol.MethodCardList, map[string]any{})
		return
	}
	switch args[0] {
	case "create":
		title := "untitled"
		if len(args) > 1 {
			title = strings.Join(args[1:], " ")
		}
		mustCall(protocol.MethodCardCreate, map[string]any{"title": title, "column": "backlog"})
	case "move":
		if len(args) < 3 {
			fatal(fmt.Errorf("card move <id> <column>"))
		}
		mustCall(protocol.MethodCardMove, map[string]any{"id": args[1], "column": args[2]})
	case "dispatch":
		if len(args) < 2 {
			fatal(fmt.Errorf("card dispatch <id>"))
		}
		mustCall(protocol.MethodCardDispatch, map[string]any{"cardId": args[1]})
	}
}

func handleGoal(args []string) {
	if len(args) == 0 || args[0] == "list" {
		mustCall(protocol.MethodGoalList, nil)
		return
	}
	switch args[0] {
	case "create":
		title := "goal"
		if len(args) > 1 {
			title = strings.Join(args[1:], " ")
		}
		mustCall(protocol.MethodGoalCreate, map[string]any{
			"title": title,
			"completionContract": map[string]any{
				"criteria":      []string{title},
				"maxIterations": 8,
			},
		})
	case "drive":
		if len(args) < 2 {
			fatal(fmt.Errorf("goal drive <id>"))
		}
		mustCall(protocol.MethodGoalDrive, map[string]any{"id": args[1]})
	}
}

func handleWorkspace(args []string) {
	if len(args) == 0 || args[0] == "list" {
		mustCall(protocol.MethodWorkspaceList, nil)
		return
	}
	if args[0] == "open" && len(args) > 1 {
		mustCall(protocol.MethodWorkspaceOpen, map[string]any{"path": args[1]})
	}
}

func handleTerm(args []string) {
	if len(args) == 0 || args[0] == "list" {
		mustCall(protocol.MethodTerminalList, nil)
		return
	}
	if args[0] == "spawn" {
		mustCall(protocol.MethodTerminalSpawn, map[string]any{"kind": "user"})
	}
}

func handleSkill(args []string) {
	if len(args) == 0 || args[0] == "list" {
		mustCall(protocol.MethodSkillList, nil)
		return
	}
	if args[0] == "install" && len(args) > 1 {
		mustCall(protocol.MethodSkillInstall, map[string]any{"path": args[1]})
	}
}

func handleMarket(args []string) {
	if len(args) == 0 || args[0] == "list" {
		mustCall(protocol.MethodMarketList, map[string]any{})
		return
	}
	switch args[0] {
	case "publish":
		mustCall(protocol.MethodMarketPublish, map[string]any{"path": args[1]})
	case "install":
		mustCall(protocol.MethodMarketInstall, map[string]any{"name": args[1], "version": args[2]})
	}
}

func handleProvider(args []string) {
	mustCall(protocol.MethodProviderList, nil)
}
