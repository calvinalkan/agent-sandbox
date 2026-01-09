//go:build ignore

package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// --- Global vars ---
var (
	tmuxSession  string
	startTime    time.Time
	numAgents    int
	numAgentsMu  sync.RWMutex
	draining     bool
	drainingMu   sync.RWMutex
	agentRunner  string // "pi" or "claude"
	dispatched   = make(map[string]bool)
	dispatchedMu sync.RWMutex
)

func init() {
	// Derive tmux session name from current directory (needed for socket/pid paths)
	cwd, _ := os.Getwd()
	tmuxSession = filepath.Base(cwd) + "-agents"
}

// --- Main & Command Routing ---
func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "start":
		cmdStart(os.Args[2:])
	case "stop":
		cmdStop()
	case "status":
		cmdStatus(os.Args[2:])
	case "run":
		cmdRun(os.Args[2:])
	case "scale":
		cmdScale(os.Args[2:])
	case "drain":
		cmdDrain()
	case "daemon":
		cmdDaemon(os.Args[2:])
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: go run agentloop.go <command> [options]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  start  -n NUM <prompt-file>  Start daemon in background")
	fmt.Fprintln(os.Stderr, "  stop                         Stop the daemon (kills agents)")
	fmt.Fprintln(os.Stderr, "  drain                        Stop after current agents finish")
	fmt.Fprintln(os.Stderr, "  status [-f]                  Show status (-f to follow logs)")
	fmt.Fprintln(os.Stderr, "  scale  NUM                   Change max parallel agents")
	fmt.Fprintln(os.Stderr, "  run    -n NUM <prompt-file>  Run in foreground (for debugging)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Options:")
	fmt.Fprintln(os.Stderr, "  -n NUM          number of parallel agents (default 1)")
	fmt.Fprintln(os.Stderr, "  --runner NAME   agent runner: 'pi' or 'claude' (default 'pi')")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "The prompt file should contain $$ID which gets replaced with the ticket ID.")
}

// --- CLI Commands ---

func cmdStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	numAgents := fs.Int("n", 1, "number of parallel agents")
	runner := fs.String("runner", "pi", "agent runner: 'pi' or 'claude'")
	fs.Parse(args)

	if *runner != "pi" && *runner != "claude" {
		fmt.Fprintf(os.Stderr, "error: invalid runner '%s', must be 'pi' or 'claude'\n", *runner)
		os.Exit(1)
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: prompt file required")
		os.Exit(1)
	}
	promptFile := fs.Arg(0)

	// Verify prompt file exists
	absPrompt, err := filepath.Abs(promptFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stat(absPrompt); err != nil {
		fmt.Fprintf(os.Stderr, "error: prompt file not found: %s\n", promptFile)
		os.Exit(1)
	}

	// Check if already running
	if resp, err := sendCommand("ping"); err == nil && resp == "pong" {
		fmt.Fprintln(os.Stderr, "error: daemon already running")
		os.Exit(1)
	}

	// Get current working directory for daemon
	cwd, _ := os.Getwd()

	// Spawn daemon subprocess
	executable, err := os.Executable()
	if err != nil {
		executable = os.Args[0]
	}

	daemonArgs := []string{"daemon", "-n", strconv.Itoa(*numAgents), "--runner", *runner, absPrompt}
	cmd := exec.Command(executable, daemonArgs...)
	cmd.Dir = cwd

	// Create log file
	logFile, err := os.OpenFile(logPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating log file: %v\n", err)
		os.Exit(1)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Detach from parent
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "error starting daemon: %v\n", err)
		os.Exit(1)
	}

	// Write PID file
	if err := os.WriteFile(pidPath(), []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write pid file: %v\n", err)
	}

	fmt.Printf("started pid=%d session=%s\n", cmd.Process.Pid, tmuxSession)
	fmt.Printf("log: %s\n", logPath())
}

func cmdStop() {
	// Try graceful stop via socket
	if resp, err := sendCommand("stop"); err == nil && resp == "ok" {
		// Wait for daemon to exit
		for i := 0; i < 30; i++ {
			time.Sleep(100 * time.Millisecond)
			if _, err := sendCommand("ping"); err != nil {
				fmt.Println("stopped")
				os.Remove(pidPath())
				os.Remove(socketPath())
				return
			}
		}
		fmt.Println("stopped (timeout waiting for confirmation)")
		return
	}

	// Fallback: read PID and send SIGTERM
	pidData, err := os.ReadFile(pidPath())
	if err != nil {
		fmt.Println("not running")
		return
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		fmt.Println("not running (invalid pid file)")
		os.Remove(pidPath())
		return
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		fmt.Println("not running")
		os.Remove(pidPath())
		return
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		fmt.Println("not running")
		os.Remove(pidPath())
		return
	}

	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if err := process.Signal(syscall.Signal(0)); err != nil {
			break
		}
	}

	os.Remove(pidPath())
	os.Remove(socketPath())
	fmt.Println("stopped")
}

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	follow := fs.Bool("f", false, "follow log output")
	fs.Parse(args)

	// First check if running and show current status
	resp, err := sendCommand("status")
	if err != nil {
		fmt.Println("not running")
		return
	}
	fmt.Println(resp)

	if !*follow {
		return
	}

	// Follow mode: tail the log file
	logFile := logPath()
	if _, err := os.Stat(logFile); err != nil {
		fmt.Fprintf(os.Stderr, "error: log file not found: %s\n", logFile)
		os.Exit(1)
	}

	fmt.Printf("\n--- following %s (Ctrl-C to stop) ---\n\n", logFile)

	// Use tail -f for simplicity
	cmd := exec.Command("tail", "-f", logFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Handle Ctrl-C gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cmd.Process.Kill()
	}()

	cmd.Run()
}

func cmdScale(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "error: scale requires a number")
		os.Exit(1)
	}

	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 {
		fmt.Fprintln(os.Stderr, "error: invalid number")
		os.Exit(1)
	}

	resp, err := sendCommand(fmt.Sprintf("scale %d", n))
	if err != nil {
		fmt.Println("not running")
		os.Exit(1)
	}
	fmt.Println(resp)
}

func cmdDrain() {
	resp, err := sendCommand("drain")
	if err != nil {
		fmt.Println("not running")
		os.Exit(1)
	}
	fmt.Println(resp)
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	n := fs.Int("n", 1, "number of parallel agents")
	runner := fs.String("runner", "pi", "agent runner: 'pi' or 'claude'")
	fs.Parse(args)

	if *runner != "pi" && *runner != "claude" {
		fmt.Fprintf(os.Stderr, "error: invalid runner '%s', must be 'pi' or 'claude'\n", *runner)
		os.Exit(1)
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: prompt file required")
		os.Exit(1)
	}
	promptFile := fs.Arg(0)

	if _, err := os.Stat(promptFile); err != nil {
		fmt.Fprintf(os.Stderr, "error: prompt file not found: %s\n", promptFile)
		os.Exit(1)
	}

	agentRunner = *runner
	setNumAgents(*n)

	log.SetFlags(log.Ltime)
	log.Printf("starting: max %d agent(s), tmux=%s", *n, tmuxSession)

	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		close(stopCh)
	}()

	runLoop(promptFile, stopCh)
	shutdown()
}

func cmdDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	n := fs.Int("n", 1, "number of parallel agents")
	runner := fs.String("runner", "pi", "agent runner: 'pi' or 'claude'")
	fs.Parse(args)

	if *runner != "pi" && *runner != "claude" {
		fmt.Fprintf(os.Stderr, "error: invalid runner '%s', must be 'pi' or 'claude'\n", *runner)
		os.Exit(1)
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: prompt file required")
		os.Exit(1)
	}
	promptFile := fs.Arg(0)

	agentRunner = *runner
	setNumAgents(*n)

	log.SetFlags(log.Ltime)
	log.Printf("daemon starting: max %d agent(s), tmux=%s", *n, tmuxSession)

	stopCh := make(chan struct{})

	go startSocketServer(stopCh)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		close(stopCh)
	}()

	runLoop(promptFile, stopCh)

	shutdown()
	os.Remove(socketPath())
	os.Remove(pidPath())
	log.Printf("daemon stopped")
}

// --- Daemon Logic ---

func getNumAgents() int {
	numAgentsMu.RLock()
	defer numAgentsMu.RUnlock()
	return numAgents
}

func setNumAgents(n int) {
	numAgentsMu.Lock()
	defer numAgentsMu.Unlock()
	numAgents = n
}

func isDraining() bool {
	drainingMu.RLock()
	defer drainingMu.RUnlock()
	return draining
}

func setDraining(v bool) {
	drainingMu.Lock()
	defer drainingMu.Unlock()
	draining = v
}

func isDispatched(ticketID string) bool {
	dispatchedMu.RLock()
	defer dispatchedMu.RUnlock()
	return dispatched[ticketID]
}

func markDispatched(ticketID string) {
	dispatchedMu.Lock()
	defer dispatchedMu.Unlock()
	dispatched[ticketID] = true
}

func clearDispatched(ticketID string) {
	dispatchedMu.Lock()
	defer dispatchedMu.Unlock()
	delete(dispatched, ticketID)
}

func runLoop(promptFile string, stopCh <-chan struct{}) {
	startTime = time.Now()
	lastStatus := time.Now()
	backoff := 1 * time.Second
	const maxBackoff = 10 * time.Second

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		cleanupDeadAgents()

		readyTickets := getReadyTickets()
		running := countRunningAgents()
		activeTickets := getActiveTickets()
		maxAgents := getNumAgents()
		draining := isDraining()

		// If draining and no agents running, we're done
		if draining && running == 0 {
			log.Printf("drain complete")
			return
		}

		if time.Since(lastStatus) >= 10*time.Second {
			status := ""
			if draining {
				status = "[draining] "
			}
			if len(activeTickets) > 0 {
				formatted := make([]string, len(activeTickets))
				for i, id := range activeTickets {
					formatted[i] = formatTicketWithTitle(id)
				}
				log.Printf("%srunning: %s | %d ready for pickup | max: %d",
					status, strings.Join(formatted, ", "), len(readyTickets), maxAgents)
			} else {
				log.Printf("%sidle | %d ready for pickup | max: %d", status, len(readyTickets), maxAgents)
			}
			lastStatus = time.Now()
		}

		// Don't start new agents if draining
		if draining || running >= maxAgents {
			time.Sleep(backoff)
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		// Check for orphaned tickets first (in_progress with worktree but no agent)
		orphanedTickets := getOrphanedTickets()

		var ticketID string
		var isOrphaned bool
		if len(orphanedTickets) > 0 {
			ticketID = orphanedTickets[0]
			isOrphaned = true
			log.Printf("restarting orphaned ticket: %s", formatTicketWithTitle(ticketID))
		} else {
			// Filter out already-dispatched tickets
			ticketID = ""
			for _, id := range readyTickets {
				if !isDispatched(id) {
					ticketID = id
					break
				}
			}
			if ticketID == "" {
				time.Sleep(backoff)
				backoff = min(backoff*2, maxBackoff)
				continue
			}
		}

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

		markDispatched(ticketID)
		if err := startAgent(ticketID, wtPath, string(promptBytes)); err != nil {
			clearDispatched(ticketID)
			log.Printf("start failed: %s: %v", ticketID, err)
			time.Sleep(backoff)
			continue
		}

		if isOrphaned {
			log.Printf("agent restarted: %s", formatTicketWithTitle(ticketID))
		} else {
			log.Printf("agent started: %s", formatTicketWithTitle(ticketID))
		}
		backoff = 1 * time.Second
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

func getInProgressTickets() []string {
	cmd := exec.Command("tk", "ls", "--status", "in_progress")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil
	}

	var tickets []string
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
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

// getOrphanedTickets returns in_progress tickets that have a worktree but no running agent
func getOrphanedTickets() []string {
	inProgress := getInProgressTickets()
	activeTickets := getActiveTickets()

	// Build set of active tickets for fast lookup
	active := make(map[string]bool)
	for _, id := range activeTickets {
		active[id] = true
	}

	var orphaned []string
	for _, id := range inProgress {
		if active[id] {
			continue // agent is running, not orphaned
		}
		// Check if worktree exists
		wtName := "ticket-" + id
		pathCmd := exec.Command("wt", "info", wtName, "--field", "path")
		if pathCmd.Run() == nil {
			// Worktree exists but no agent running → orphaned
			orphaned = append(orphaned, id)
		}
	}
	return orphaned
}

func countRunningAgents() int {
	return len(getActiveTickets())
}

func getActiveTickets() []string {
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

	pathCmd := exec.Command("wt", "info", wtName, "--field", "path")
	var pathOut bytes.Buffer
	pathCmd.Stdout = &pathOut
	if pathCmd.Run() == nil {
		return strings.TrimSpace(pathOut.String()), nil
	}

	cmd := exec.Command("wt", "create", "-n", wtName)
	var errOut bytes.Buffer
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, errOut.String())
	}

	pathCmd = exec.Command("wt", "info", wtName, "--field", "path")
	pathOut.Reset()
	pathCmd.Stdout = &pathOut
	if err := pathCmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get worktree path: %v", err)
	}

	return strings.TrimSpace(pathOut.String()), nil
}

func startAgent(ticketID, wtPath, basePrompt string) error {
	prompt := strings.ReplaceAll(basePrompt, "$$ID", ticketID)
	promptPath := wtPath + "/.wt/prompt.md"
	if err := os.WriteFile(promptPath, []byte(prompt), 0644); err != nil {
		return fmt.Errorf("write prompt: %w", err)
	}

	windowName := "ticket-" + ticketID

	// Build the agent command based on runner type
	var agentCmd string
	switch agentRunner {
	case "claude":
		// Use agent-sandbox wrapper with dangerously-skip-permissions
		agentCmd = fmt.Sprintf("cd %s && agent-sandbox claude --dangerously-skip-permissions \"$(cat .wt/prompt.md)\"", wtPath)
	default: // "pi"
		agentCmd = fmt.Sprintf("cd %s && agent-sandbox pi @.wt/prompt.md", wtPath)
	}

	var cmd *exec.Cmd
	if tmuxSessionExists() {
		cmd = exec.Command("tmux", "new-window", "-t", tmuxSession, "-n", windowName, agentCmd)
	} else {
		cmd = exec.Command("tmux", "new-session", "-d", "-s", tmuxSession, "-n", windowName, agentCmd)
		log.Printf("created tmux session: %s", tmuxSession)
	}

	if err := cmd.Run(); err != nil {
		return err
	}

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

		if paneDead {
			status := getTicketStatus(ticketID)
			if status == "closed" {
				log.Printf("agent done: %s", formatTicketWithTitle(ticketID))
			} else {
				log.Printf("WARN: agent exited, ticket %s still %s", formatTicketWithTitle(ticketID), status)
			}
			exec.Command("tmux", "kill-window", "-t", tmuxSession+":"+windowName).Run()
			clearDispatched(ticketID)
			continue
		}

		status := getTicketStatus(ticketID)
		if status == "closed" && branchMerged(branchName) {
			log.Printf("agent done: %s", formatTicketWithTitle(ticketID))
			killAgentInPane(pid)
			exec.Command("tmux", "kill-window", "-t", tmuxSession+":"+windowName).Run()
			clearDispatched(ticketID)
			continue
		}
	}
}

func branchMerged(branchName string) bool {
	// Check if branch commits are already in HEAD (main branch)
	cmd := exec.Command("git", "merge-base", "--is-ancestor", branchName, "HEAD")
	return cmd.Run() == nil
}

func killAgentInPane(shellPid string) {
	// Try to kill either pi or claude process
	processNames := []string{"pi", "claude"}
	for _, name := range processNames {
		pgrepCmd := exec.Command("pgrep", "-P", shellPid, "-x", name)
		var out bytes.Buffer
		pgrepCmd.Stdout = &out
		if pgrepCmd.Run() == nil {
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(out.String()), "%d", &pid); err == nil {
				syscall.Kill(pid, syscall.SIGTERM)
				return
			}
		}
	}
}

func getTicketStatus(ticketID string) string {
	status, _ := getTicketInfo(ticketID)
	return status
}

func getTicketTitle(ticketID string) string {
	_, title := getTicketInfo(ticketID)
	return title
}

func getTicketInfo(ticketID string) (status, title string) {
	cmd := exec.Command("tk", "show", ticketID)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "unknown", ""
	}

	content := out.String()
	inFrontmatter := false
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			inFrontmatter = !inFrontmatter
			continue
		}
		if inFrontmatter && strings.HasPrefix(trimmed, "status:") {
			status = strings.TrimSpace(strings.TrimPrefix(trimmed, "status:"))
		}
		// Title is the first markdown heading after frontmatter
		if !inFrontmatter && strings.HasPrefix(trimmed, "# ") {
			title = strings.TrimPrefix(trimmed, "# ")
			break
		}
	}
	if status == "" {
		status = "unknown"
	}
	return status, title
}

// formatTicketWithTitle returns "id (title)" or just "id" if no title
func formatTicketWithTitle(ticketID string) string {
	title := getTicketTitle(ticketID)
	if title != "" {
		// Truncate long titles
		if len(title) > 40 {
			title = title[:37] + "..."
		}
		return fmt.Sprintf("%s (%s)", ticketID, title)
	}
	return ticketID
}

func shutdown() {
	log.Printf("shutting down...")
	exec.Command("tmux", "kill-session", "-t", tmuxSession).Run()
	log.Printf("stopped")
}

// --- Socket Server (daemon side) ---

func startSocketServer(stopCh chan struct{}) {
	sockPath := socketPath()
	os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		log.Printf("socket server error: %v", err)
		return
	}
	defer listener.Close()

	go func() {
		<-stopCh
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-stopCh:
				return
			default:
				log.Printf("socket accept error: %v", err)
				continue
			}
		}

		go handleConnection(conn, stopCh)
	}
}

func handleConnection(conn net.Conn, stopCh chan struct{}) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return
	}

	cmd := strings.TrimSpace(line)
	var response string

	switch {
	case cmd == "ping":
		response = "pong"

	case cmd == "stop":
		response = "ok"
		conn.Write([]byte(response + "\n"))
		close(stopCh)
		return

	case cmd == "status":
		activeTickets := getActiveTickets()
		readyTickets := getReadyTickets()
		uptime := time.Since(startTime).Round(time.Second)
		maxAgents := getNumAgents()
		draining := isDraining()

		status := ""
		if draining {
			status = "[draining] "
		}
		if len(activeTickets) > 0 {
			formatted := make([]string, len(activeTickets))
			for i, id := range activeTickets {
				formatted[i] = formatTicketWithTitle(id)
			}
			response = fmt.Sprintf("%srunning: %s | %d ready | max: %d | uptime: %s",
				status, strings.Join(formatted, ", "), len(readyTickets), maxAgents, uptime)
		} else {
			response = fmt.Sprintf("%sidle | %d ready | max: %d | uptime: %s", status, len(readyTickets), maxAgents, uptime)
		}

	case strings.HasPrefix(cmd, "scale "):
		nStr := strings.TrimPrefix(cmd, "scale ")
		n, err := strconv.Atoi(nStr)
		if err != nil || n < 1 {
			response = "error: invalid number"
		} else {
			old := getNumAgents()
			setNumAgents(n)
			log.Printf("scaled: %d -> %d agents", old, n)
			response = fmt.Sprintf("scaled: %d -> %d", old, n)
		}

	case cmd == "drain":
		setDraining(true)
		running := countRunningAgents()
		log.Printf("draining: waiting for %d agent(s) to finish", running)
		response = fmt.Sprintf("draining: %d agent(s) running", running)

	default:
		response = "error: unknown command"
	}

	conn.Write([]byte(response + "\n"))
}

// --- Socket Client (CLI side) ---

func sendCommand(cmd string) (string, error) {
	conn, err := net.DialTimeout("unix", socketPath(), 2*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second))

	_, err = conn.Write([]byte(cmd + "\n"))
	if err != nil {
		return "", err
	}

	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}

	return strings.TrimSpace(response), nil
}

// --- Helpers ---

func socketPath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("agentloop-%s.sock", tmuxSession))
}

func pidPath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("agentloop-%s.pid", tmuxSession))
}

func logPath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("agentloop-%s.log", tmuxSession))
}
