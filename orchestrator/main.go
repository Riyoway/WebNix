package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

var (
	tenantPattern  = regexp.MustCompile(`^[a-z2-7]{26}$`)
	versionPattern = regexp.MustCompile(`([0-9]+)\.([0-9]+)\.([0-9]+)`)
)

type config struct {
	socket      string
	image       string
	profile     string
	network     string
	storagePool string
	denylist    string
	cpu         string
	memory      string
	disk        string
	idleTimeout time.Duration
	maxSessions int
	maxTenants  int
	maxRunning  int
}

type manager struct {
	cfg        config
	logger     *slog.Logger
	locksMu    sync.Mutex
	locks      map[string]*sync.Mutex
	capacityMu sync.Mutex
	stateMu    sync.Mutex
	sessions   map[string]int
	idle       map[string]*time.Timer
}

type resizeMessage struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

func main() {
	cfg := loadConfig()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	m := &manager{
		cfg:      cfg,
		logger:   logger,
		locks:    make(map[string]*sync.Mutex),
		sessions: make(map[string]int),
		idle:     make(map[string]*time.Timer),
	}

	preflightCtx, preflightCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := m.preflight(preflightCtx); err != nil {
		preflightCancel()
		logger.Error("preflight failed", "error", err)
		os.Exit(1)
	}
	preflightCancel()

	if err := os.MkdirAll(filepath.Dir(cfg.socket), 0o750); err != nil {
		logger.Error("failed to create runtime directory", "error", err)
		os.Exit(1)
	}
	_ = os.Remove(cfg.socket)
	listener, err := net.Listen("unix", cfg.socket)
	if err != nil {
		logger.Error("failed to listen", "error", err)
		os.Exit(1)
	}
	defer listener.Close()
	if err := os.Chmod(cfg.socket, 0o660); err != nil {
		logger.Error("failed to chmod socket", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("GET /v1/terminal", m.terminal)

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	logger.Info("orchestrator listening", "socket", cfg.socket)
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("orchestrator stopped", "error", err)
		os.Exit(1)
	}
}

func loadConfig() config {
	idle, err := time.ParseDuration(env("WEBNIX_IDLE_TIMEOUT", "20m"))
	if err != nil {
		idle = 20 * time.Minute
	}
	return config{
		socket:      env("WEBNIX_SOCKET", "/run/webnix/orchestrator.sock"),
		image:       env("WEBNIX_VM_IMAGE", "webnix-base"),
		profile:     env("WEBNIX_PROFILE", "webnix"),
		network:     env("WEBNIX_NETWORK", "webnix0"),
		storagePool: env("WEBNIX_STORAGE_POOL", "default"),
		denylist:    env("WEBNIX_DENYLIST", "/etc/webnix/denied-tenants"),
		cpu:         env("WEBNIX_CPU", "2"),
		memory:      env("WEBNIX_MEMORY", "4GiB"),
		disk:        env("WEBNIX_DISK", "30GiB"),
		idleTimeout: idle,
		maxSessions: boundedInt("WEBNIX_MAX_SESSIONS", 4, 1, 16),
		maxTenants:  boundedInt("WEBNIX_MAX_TENANTS", 16, 1, 10000),
		maxRunning:  boundedInt("WEBNIX_MAX_RUNNING_WORKSPACES", 3, 1, 10000),
	}
}

func (m *manager) preflight(ctx context.Context) error {
	versionText, err := m.runIncus(ctx, 10*time.Second, "version")
	if err != nil {
		return fmt.Errorf("incus unavailable: %w", err)
	}
	match := versionPattern.FindStringSubmatch(versionText)
	if len(match) != 4 {
		return fmt.Errorf("cannot parse Incus version %q", strings.TrimSpace(versionText))
	}
	parts := [3]int{}
	for i := 0; i < 3; i++ {
		parts[i], _ = strconv.Atoi(match[i+1])
	}
	if parts[0] < 7 || parts[0] == 7 && parts[1] < 3 {
		return fmt.Errorf("Incus 7.3.0 or newer is required, found %d.%d.%d", parts[0], parts[1], parts[2])
	}

	checks := [][]string{
		{"image", "info", m.cfg.image},
		{"profile", "show", m.cfg.profile},
		{"network", "show", m.cfg.network},
		{"storage", "show", m.cfg.storagePool},
	}
	for _, args := range checks {
		if _, err := m.runIncus(ctx, 10*time.Second, args...); err != nil {
			return fmt.Errorf("required Incus resource %q is unavailable: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}

func (m *manager) terminal(w http.ResponseWriter, r *http.Request) {
	tenant := r.URL.Query().Get("tenant")
	if !tenantPattern.MatchString(tenant) {
		http.Error(w, "invalid tenant", http.StatusBadRequest)
		return
	}

	denied, err := m.tenantDenied(tenant)
	if err != nil {
		m.logger.Error("denylist read failed", "error", err)
		http.Error(w, "workspace policy unavailable", http.StatusServiceUnavailable)
		return
	}
	if denied {
		http.Error(w, "workspace suspended", http.StatusForbidden)
		return
	}

	if !m.acquireSession(tenant) {
		http.Error(w, "too many terminal sessions", http.StatusTooManyRequests)
		return
	}
	defer m.releaseSession(tenant)

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // This listener is a filesystem-protected Unix socket, never a browser endpoint.
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(128 << 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = writeControl(ctx, conn, "status", "Preparing NixOS workspace…")
	if err := m.ensureWorkspace(ctx, tenant); err != nil {
		m.logger.Error("workspace preparation failed", "tenant", tenant, "error", err)
		_ = writeControl(ctx, conn, "error", "Workspace could not be prepared")
		_ = conn.Close(websocket.StatusInternalError, "workspace preparation failed")
		return
	}

	_ = writeControl(ctx, conn, "status", "Starting terminal…")
	cmd := m.execCommand(ctx, tenant)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 100, Rows: 30})
	if err != nil {
		m.logger.Error("terminal start failed", "tenant", tenant, "error", err)
		_ = writeControl(ctx, conn, "error", "Terminal could not be started")
		return
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGHUP)
		}
	}()

	_ = writeControl(ctx, conn, "ready", "NixOS workspace connected")
	errC := make(chan error, 2)
	go m.ptyToWebSocket(ctx, conn, ptmx, errC)
	go m.webSocketToPTY(ctx, conn, ptmx, errC)
	<-errC
}

func (m *manager) ensureWorkspace(ctx context.Context, tenant string) error {
	lock := m.tenantLock(tenant)
	lock.Lock()
	defer lock.Unlock()

	project := projectName(tenant)
	if err := m.ensureProject(ctx, project); err != nil {
		return err
	}
	if !m.instanceExists(ctx, project) {
		args := []string{
			"--project", project,
			"init", m.cfg.image, "workspace", "--vm",
			"--profile", m.cfg.profile,
			"--config", "limits.cpu=" + m.cfg.cpu,
			"--config", "limits.memory=" + m.cfg.memory,
			"--config", "security.nesting=false",
			"--config", "security.secureboot=false",
		}
		if _, err := m.runIncus(ctx, 4*time.Minute, args...); err != nil {
			return fmt.Errorf("initialize workspace: %w", err)
		}
		// Override root disk size on the inherited profile device for this VM.
		if _, err := m.runIncus(ctx, 30*time.Second, "--project", project, "config", "device", "override", "workspace", "root", "size="+m.cfg.disk); err != nil {
			return fmt.Errorf("set workspace disk limit: %w", err)
		}
	}

	state, _ := m.workspaceState(ctx, project)
	if state != "RUNNING" {
		if err := m.startWorkspaceWithinCapacity(ctx, project); err != nil {
			return err
		}
	}

	deadline := time.Now().Add(75 * time.Second)
	for time.Now().Before(deadline) {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := m.runIncus(probeCtx, 5*time.Second, "--project", project, "exec", "workspace", "--", "/run/current-system/sw/bin/true")
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1200 * time.Millisecond):
		}
	}
	return errors.New("incus agent did not become ready")
}

func (m *manager) ensureProject(ctx context.Context, project string) error {
	if _, err := m.runIncus(ctx, 10*time.Second, "project", "show", project); err == nil {
		return nil
	}

	m.capacityMu.Lock()
	defer m.capacityMu.Unlock()
	if _, err := m.runIncus(ctx, 10*time.Second, "project", "show", project); err == nil {
		return nil
	}
	tenantCount, err := m.countTenantProjects(ctx)
	if err != nil {
		return fmt.Errorf("count tenant projects: %w", err)
	}
	if tenantCount >= m.cfg.maxTenants {
		return fmt.Errorf("tenant capacity reached (%d)", m.cfg.maxTenants)
	}

	args := []string{
		"project", "create", project,
		"--config", "features.images=false",
		"--config", "features.profiles=false",
		"--config", "features.networks=false",
		"--config", "restricted=true",
		"--config", "restricted.backups=block",
		"--config", "restricted.snapshots=block",
		"--config", "restricted.virtual-machines.lowlevel=block",
		"--config", "restricted.virtual-machines.nesting=block",
		"--config", "restricted.devices.disk=managed",
		"--config", "restricted.devices.nic=managed",
		"--config", "restricted.devices.gpu=block",
		"--config", "restricted.devices.pci=block",
		"--config", "restricted.devices.proxy=block",
		"--config", "restricted.devices.usb=block",
		"--config", "restricted.networks.access=" + m.cfg.network,
		"--config", "restricted.storage-pools.access=" + m.cfg.storagePool,
		"--config", "limits.instances=1",
		"--config", "limits.virtual-machines=1",
		"--config", "limits.cpu=" + m.cfg.cpu,
		"--config", "limits.memory=" + m.cfg.memory,
		"--config", "limits.disk=" + m.cfg.disk,
	}
	if _, err := m.runIncus(ctx, 30*time.Second, args...); err != nil {
		// A concurrent creator may have won the race.
		if _, showErr := m.runIncus(ctx, 10*time.Second, "project", "show", project); showErr == nil {
			return nil
		}
		return fmt.Errorf("create project: %w", err)
	}
	return nil
}

func (m *manager) countTenantProjects(ctx context.Context) (int, error) {
	output, err := m.runIncus(ctx, 15*time.Second, "project", "list", "-c", "n", "--format", "csv,noheader")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "wn-") {
			count++
		}
	}
	return count, nil
}

func (m *manager) countRunningWorkspaces(ctx context.Context) (int, error) {
	output, err := m.runIncus(ctx, 20*time.Second, "list", "--all-projects", "-c", "ens", "--format", "csv,noheader")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, ",")
		if len(fields) != 3 {
			continue
		}
		project := strings.TrimSpace(fields[0])
		name := strings.TrimSpace(fields[1])
		state := strings.ToUpper(strings.TrimSpace(fields[2]))
		if strings.HasPrefix(project, "wn-") && name == "workspace" && state == "RUNNING" {
			count++
		}
	}
	return count, nil
}

func (m *manager) startWorkspaceWithinCapacity(ctx context.Context, project string) error {
	m.capacityMu.Lock()
	defer m.capacityMu.Unlock()

	state, _ := m.workspaceState(ctx, project)
	if state == "RUNNING" {
		return nil
	}
	running, err := m.countRunningWorkspaces(ctx)
	if err != nil {
		return fmt.Errorf("count running workspaces: %w", err)
	}
	if running >= m.cfg.maxRunning {
		return fmt.Errorf("running workspace capacity reached (%d)", m.cfg.maxRunning)
	}
	if _, err := m.runIncus(ctx, 90*time.Second, "--project", project, "start", "workspace"); err != nil {
		return fmt.Errorf("start workspace: %w", err)
	}
	return nil
}

func (m *manager) workspaceState(ctx context.Context, project string) (string, error) {
	output, err := m.runIncus(ctx, 15*time.Second, "--project", project, "list", "name=workspace", "-c", "s", "--format", "csv,noheader")
	if err != nil {
		return "", err
	}
	state := strings.ToUpper(strings.TrimSpace(strings.SplitN(output, "\n", 2)[0]))
	if state == "" {
		return "", errors.New("workspace instance not found")
	}
	return state, nil
}

func (m *manager) instanceExists(ctx context.Context, project string) bool {
	_, err := m.runIncus(ctx, 10*time.Second, "--project", project, "info", "workspace")
	return err == nil
}

func (m *manager) execCommand(ctx context.Context, tenant string) *exec.Cmd {
	project := projectName(tenant)
	args := []string{
		"--project", project,
		"exec", "workspace",
		"--mode", "interactive",
		"--user", "1000",
		"--group", "1000",
		"--cwd", "/home/dev",
		"--env", "HOME=/home/dev",
		"--env", "USER=dev",
		"--env", "LOGNAME=dev",
		"--env", "SHELL=/run/current-system/sw/bin/bash",
		"--env", "TERM=xterm-256color",
		"--",
		"/run/current-system/sw/bin/bash", "-l",
	}
	cmd := exec.CommandContext(ctx, "incus", args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	return cmd
}

func (m *manager) ptyToWebSocket(ctx context.Context, conn *websocket.Conn, ptmx *os.File, errC chan<- error) {
	buffer := make([]byte, 32<<10)
	for {
		n, err := ptmx.Read(buffer)
		if n > 0 {
			if writeErr := conn.Write(ctx, websocket.MessageBinary, buffer[:n]); writeErr != nil {
				errC <- writeErr
				return
			}
		}
		if err != nil {
			errC <- err
			return
		}
	}
}

func (m *manager) webSocketToPTY(ctx context.Context, conn *websocket.Conn, ptmx *os.File, errC chan<- error) {
	for {
		messageType, data, err := conn.Read(ctx)
		if err != nil {
			errC <- err
			return
		}
		switch messageType {
		case websocket.MessageBinary:
			if len(data) > 64<<10 {
				errC <- errors.New("terminal input frame too large")
				return
			}
			if _, err := ptmx.Write(data); err != nil {
				errC <- err
				return
			}
		case websocket.MessageText:
			var resize resizeMessage
			if err := json.Unmarshal(data, &resize); err != nil || resize.Type != "resize" {
				continue
			}
			if resize.Cols < 20 || resize.Rows < 5 || resize.Cols > 500 || resize.Rows > 300 {
				continue
			}
			_ = pty.Setsize(ptmx, &pty.Winsize{Cols: resize.Cols, Rows: resize.Rows})
		}
	}
}

func (m *manager) runIncus(parent context.Context, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "incus", args...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(output), ctx.Err()
	}
	if err != nil {
		return string(output), fmt.Errorf("incus %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func (m *manager) tenantLock(tenant string) *sync.Mutex {
	m.locksMu.Lock()
	defer m.locksMu.Unlock()
	if lock, ok := m.locks[tenant]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	m.locks[tenant] = lock
	return lock
}

func (m *manager) acquireSession(tenant string) bool {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if timer := m.idle[tenant]; timer != nil {
		timer.Stop()
		delete(m.idle, tenant)
	}
	if m.sessions[tenant] >= m.cfg.maxSessions {
		return false
	}
	m.sessions[tenant]++
	return true
}

func (m *manager) releaseSession(tenant string) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if m.sessions[tenant] > 0 {
		m.sessions[tenant]--
	}
	if m.sessions[tenant] != 0 {
		return
	}
	delete(m.sessions, tenant)
	m.idle[tenant] = time.AfterFunc(m.cfg.idleTimeout, func() {
		lock := m.tenantLock(tenant)
		lock.Lock()
		defer lock.Unlock()

		m.stateMu.Lock()
		if m.sessions[tenant] > 0 {
			delete(m.idle, tenant)
			m.stateMu.Unlock()
			return
		}
		m.stateMu.Unlock()

		project := projectName(tenant)
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if _, err := m.runIncus(ctx, 90*time.Second, "--project", project, "stop", "workspace", "--timeout", "30", "--force"); err != nil {
			m.logger.Warn("idle workspace stop failed", "tenant", tenant, "error", err)
		} else {
			m.logger.Info("idle workspace stopped", "tenant", tenant)
		}
		m.stateMu.Lock()
		delete(m.idle, tenant)
		m.stateMu.Unlock()
	})
}

func (m *manager) tenantDenied(tenant string) (bool, error) {
	data, err := os.ReadFile(m.cfg.denylist)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == tenant {
			return true, nil
		}
	}
	return false, nil
}

func projectName(tenant string) string { return "wn-" + tenant }

func writeControl(ctx context.Context, conn *websocket.Conn, messageType, message string) error {
	payload, _ := json.Marshal(map[string]string{"type": messageType, "message": message})
	return conn.Write(ctx, websocket.MessageText, payload)
}

func boundedInt(key string, fallback, minValue, maxValue int) int {
	value, err := strconv.Atoi(env(key, strconv.Itoa(fallback)))
	if err != nil || value < minValue || value > maxValue {
		return fallback
	}
	return value
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
