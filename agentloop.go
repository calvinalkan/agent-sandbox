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
	log.Printf("starting: max %d agent(s), tmux=%s", *numAgents, tmuxSession)

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)



	lastStatus := time.Now()
	backoff := 1 * time.Second
	const maxBackoff = 10 * time.Second

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
		activeTickets := getActiveTickets()

		// Periodic status (every 10s)
		if time.Since(lastStatus) >= 10*time.Second {
			if len(activeTickets) > 0 {
				log.Printf("running: %s | %d ready for pickup", 
					strings.Join(activeTickets, ", "), len(readyTickets))
			} else {
				log.Printf("idle | %d ready for pickup", len(readyTickets))
			}
			lastStatus = time.Now()
		}

		if len(readyTickets) == 0 || running >= *numAgents {
			time.Sleep(backoff)
			backoff = min(backoff*2, maxBackoff) // exponential backoff when idle
			continue
		}

		ticketID := readyTickets[0]

		promptBytes, err := os.ReadFile(promptFile)
		if err != nil {
			log.Printf("prompt file: %v", err)
			time.Sleep(backoff)
			continue
		}

		wtPath, err := createWorktree(ticketID)
		if err != nil {
			log.Printf("worktree failed: %s: %v", ticketID, err)
			time.Sleep(backoff)
			continue
		}

		if err := startAgent(ticketID, wtPath, string(promptBytes)); err != nil {
			log.Printf("start failed: %s: %v", ticketID, err)
			time.Sleep(backoff)
			continue
		}

		log.Printf("agent started: %s", ticketID)
		backoff = 1 * time.Second // reset backoff on activity
		time.Sleep(backoff)
	}
}

func tmuxSessionExists() bool {
	return exec.Command("tmux", "has-session", "-t", tmuxSession).Run() == nil
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
	return len(getActiveTickets())
}

func getActiveTickets() []string {
	// Get ticket IDs from tmux windows - tmux is source of truth
	cmd := exec.Command("tmux", "list-windows", "-t", tmuxSession, "-F", "#{window_name}")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil
	}

	var tickets []string
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		if strings.HasPrefix(line, "ticket-") {
			tickets = append(tickets, strings.TrimPrefix(line, "ticket-"))
		}
	}
	return tickets
}

func createWorktree(ticketID string) (string, error) {
	wtName := "ticket-" + ticketID

	// Check if worktree already exists
	pathCmd := exec.Command("wt", "info", wtName, "--field", "path")
	var pathOut bytes.Buffer
	pathCmd.Stdout = &pathOut
	if pathCmd.Run() == nil {
		return strings.TrimSpace(pathOut.String()), nil
	}

	// Create new worktree
	cmd := exec.Command("wt", "create", "-n", wtName)
	var errOut bytes.Buffer
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, errOut.String())
	}

	// Get path of newly created worktree
	pathCmd = exec.Command("wt", "info", wtName, "--field", "path")
	pathOut.Reset()
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

	var cmd *exec.Cmd
	if tmuxSessionExists() {
		// Add window to existing session
		cmd = exec.Command("tmux", "new-window", "-t", tmuxSession, "-n", windowName, piCmd)
	} else {
		// Create session with this agent as first window
		cmd = exec.Command("tmux", "new-session", "-d", "-s", tmuxSession, "-n", windowName, piCmd)
		log.Printf("created tmux session: %s", tmuxSession)
	}

	if err := cmd.Run(); err != nil {
		return err
	}

	// Set remain-on-exit so we can detect completion status
	exec.Command("tmux", "set-option", "-t", tmuxSession, "remain-on-exit", "on").Run()
	return nil
}

func cleanupDeadAgents() {
	cmd := exec.Command("tmux", "list-windows", "-t", tmuxSession, "-F", "#{window_name}:#{pane_pid}:#{pane_dead}")
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
		if len(parts) < 3 {
			continue
		}

		windowName := parts[0]
		pid := parts[1]
		paneDead := parts[2] == "1"

		if !strings.HasPrefix(windowName, "ticket-") {
			continue
		}

		ticketID := strings.TrimPrefix(windowName, "ticket-")
		branchName := "ticket-" + ticketID

		// Case 1: Pane is dead (pi exited)
		if paneDead {
			status := getTicketStatus(ticketID)
			if status == "closed" {
				log.Printf("agent done: %s", ticketID)
			} else {
				log.Printf("WARN: agent exited, ticket %s still %s", ticketID, status)
			}
			exec.Command("tmux", "kill-window", "-t", tmuxSession+":"+windowName).Run()
			continue
		}

		// Case 2: Ticket closed AND branch gone (worktree merged) → agent done, kill pi
		status := getTicketStatus(ticketID)
		if status == "closed" && !branchExists(branchName) {
			log.Printf("agent done: %s", ticketID)
			// SIGTERM the pi process
			killPiInPane(pid)
			exec.Command("tmux", "kill-window", "-t", tmuxSession+":"+windowName).Run()
			continue
		}
	}
}

func branchExists(branchName string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", branchName)
	return cmd.Run() == nil
}

func killPiInPane(shellPid string) {
	// Find pi process as child of shell and kill it
	pgrepCmd := exec.Command("pgrep", "-P", shellPid, "-x", "pi")
	var out bytes.Buffer
	pgrepCmd.Stdout = &out
	if pgrepCmd.Run() == nil {
		var pid int
		if _, err := fmt.Sscanf(strings.TrimSpace(out.String()), "%d", &pid); err == nil {
			syscall.Kill(pid, syscall.SIGTERM)
		}
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
	log.Printf("shutting down...")
	exec.Command("tmux", "kill-session", "-t", tmuxSession).Run()
	log.Printf("stopped")
}
