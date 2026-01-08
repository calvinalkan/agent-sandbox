//go:build ignore

package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var tmuxSession string

func main() {
	numAgents := flag.Int("n", 2, "number of parallel agents")
	showHelp := flag.Bool("h", false, "show help")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: go run agentloop.go [-n NUM] <prompt-file>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Options:")
		fmt.Fprintln(os.Stderr, "  -n NUM  number of parallel agents (default 2)")
		fmt.Fprintln(os.Stderr, "  -h      show help")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "The prompt file should contain $$ID which gets replaced with the ticket ID.")
	}
	flag.Parse()

	if *showHelp {
		flag.Usage()
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}
	promptFile := args[0]

	// Verify prompt file exists at startup
	if _, err := os.Stat(promptFile); err != nil {
		fmt.Fprintf(os.Stderr, "error: prompt file not found: %s\n", promptFile)
		os.Exit(1)
	}

	// Derive tmux session name from current directory
	cwd, _ := os.Getwd()
	tmuxSession = filepath.Base(cwd) + "-agents"

	log.SetFlags(log.Ltime)
	log.Printf("orchestrator: %d agents, session=%s", *numAgents, tmuxSession)

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Ensure tmux session exists
	ensureTmuxSession()

	for {
		// Check for shutdown signal (non-blocking)
		select {
		case <-sigCh:
			shutdown(sigCh)
			return
		default:
		}
		// Clean up finished agents
		cleanupDeadAgents()

		// Get ready tickets
		readyTickets := getReadyTickets()
		
		// Count running agents
		running := countRunningAgents()

		if len(readyTickets) == 0 {
			time.Sleep(1 * time.Second)
			continue
		}

		if running >= *numAgents {
			time.Sleep(1 * time.Second)
			continue
		}

		ticketID := readyTickets[0]

		promptBytes, err := os.ReadFile(promptFile)
		if err != nil {
			log.Printf("prompt file: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		wtPath, err := createWorktree(ticketID)
		if err != nil {
			log.Printf("worktree failed: %s: %v", ticketID, err)
			time.Sleep(1 * time.Second)
			continue
		}

		if err := startAgent(ticketID, wtPath, string(promptBytes)); err != nil {
			log.Printf("start failed: %s: %v", ticketID, err)
			time.Sleep(1 * time.Second)
			continue
		}

		log.Printf("started %s", ticketID)
		time.Sleep(1 * time.Second)
	}
}

func ensureTmuxSession() {
	cmd := exec.Command("tmux", "has-session", "-t", tmuxSession)
	if cmd.Run() != nil {
		cmd = exec.Command("tmux", "new-session", "-d", "-s", tmuxSession)
		if err := cmd.Run(); err != nil {
			log.Fatalf("tmux session failed: %v", err)
		}
		log.Printf("created tmux session: %s", tmuxSession)
	}
}

func getReadyTickets() []string {
	cmd := exec.Command("tk", "ready")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil
	}

	var tickets []string
	lines := strings.SplitSeq(strings.TrimSpace(out.String()), "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			tickets = append(tickets, fields[0])
		}
	}
	return tickets
}

func countRunningAgents() int {
	cmd := exec.Command("tmux", "list-windows", "-t", tmuxSession, "-F", "#{window_name}:#{pane_pid}")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return 0
	}

	count := 0
	lines := strings.SplitSeq(strings.TrimSpace(out.String()), "\n")
	for line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		
		windowName := parts[0]
		pid := parts[len(parts)-1]
		
		// Only count ticket windows
		if !strings.HasPrefix(windowName, "ticket-") {
			continue
		}

		// Check if pi is running as child of the shell
		pgrepCmd := exec.Command("pgrep", "-P", pid, "-x", "pi")
		if pgrepCmd.Run() == nil {
			count++
		}
	}
	return count
}

func createWorktree(ticketID string) (string, error) {
	wtName := "ticket-" + ticketID
	cmd := exec.Command("wt", "create", "-n", wtName)
	var errOut bytes.Buffer
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, errOut.String())
	}

	pathCmd := exec.Command("wt", "info", wtName, "--field", "path")
	var pathOut bytes.Buffer
	pathCmd.Stdout = &pathOut
	if err := pathCmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get worktree path: %v", err)
	}

	return strings.TrimSpace(pathOut.String()), nil
}

func startAgent(ticketID, wtPath, basePrompt string) error {
	// Write prompt with $$ID replaced into .wt/ (gitignored)
	prompt := strings.ReplaceAll(basePrompt, "$$ID", ticketID)
	promptPath := wtPath + "/.wt/prompt.md"
	if err := os.WriteFile(promptPath, []byte(prompt), 0644); err != nil {
		return fmt.Errorf("write prompt: %w", err)
	}

	windowName := "ticket-" + ticketID
	piCmd := fmt.Sprintf("cd %s && pi @.wt/prompt.md", wtPath)
	cmd := exec.Command("tmux", "new-window", "-t", tmuxSession, "-n", windowName, piCmd)
	return cmd.Run()
}

func cleanupDeadAgents() {
	cmd := exec.Command("tmux", "list-windows", "-t", tmuxSession, "-F", "#{window_name}:#{pane_pid}")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return
	}

	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}

		windowName := parts[0]
		pid := parts[len(parts)-1]

		if !strings.HasPrefix(windowName, "ticket-") {
			continue
		}

		// Check if pi is still running
		pgrepCmd := exec.Command("pgrep", "-P", pid, "-x", "pi")
		if pgrepCmd.Run() == nil {
			continue // still running
		}

		// Agent finished - check ticket status
		ticketID := strings.TrimPrefix(windowName, "ticket-")
		status := getTicketStatus(ticketID)

		if status == "closed" {
			log.Printf("finished %s", ticketID)
		} else {
			log.Printf("WARNING: agent exited but ticket %s is %s", ticketID, status)
		}

		// Kill the tmux window
		exec.Command("tmux", "kill-window", "-t", tmuxSession+":"+windowName).Run()
	}
}

func getTicketStatus(ticketID string) string {
	cmd := exec.Command("tk", "show", ticketID)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "unknown"
	}

	// Parse YAML front matter for status
	content := out.String()
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "status:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "status:"))
		}
	}
	return "unknown"
}

func shutdown(sigCh <-chan os.Signal) {
	log.Printf("shutting down, sending SIGTERM to agents...")

	// Get all agent pids
	pids := getAgentPids()
	if len(pids) == 0 {
		log.Printf("no agents running")
		return
	}

	// Send SIGTERM to all
	for _, pid := range pids {
		syscall.Kill(pid, syscall.SIGTERM)
	}

	// Wait for completion, timeout, or second signal
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			log.Printf("forced exit, sending SIGKILL...")
			for _, pid := range getAgentPids() {
				syscall.Kill(pid, syscall.SIGKILL)
			}
			return
		case <-deadline:
			log.Printf("timeout, sending SIGKILL...")
			for _, pid := range getAgentPids() {
				syscall.Kill(pid, syscall.SIGKILL)
			}
			return
		case <-ticker.C:
			if len(getAgentPids()) == 0 {
				log.Printf("all agents stopped")
				return
			}
		}
	}
}

func getAgentPids() []int {
	cmd := exec.Command("tmux", "list-windows", "-t", tmuxSession, "-F", "#{window_name}:#{pane_pid}")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil
	}

	var pids []int
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}

		windowName := parts[0]
		shellPid := parts[len(parts)-1]

		if !strings.HasPrefix(windowName, "ticket-") {
			continue
		}

		// Find pi process as child of shell
		pgrepCmd := exec.Command("pgrep", "-P", shellPid, "-x", "pi")
		var pgrepOut bytes.Buffer
		pgrepCmd.Stdout = &pgrepOut
		if pgrepCmd.Run() == nil {
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(pgrepOut.String()), "%d", &pid); err == nil {
				pids = append(pids, pid)
			}
		}
	}
	return pids
}
