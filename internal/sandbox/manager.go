package sandbox

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"forge/internal/config"
	"forge/internal/natsutil"
	"github.com/nats-io/nats.go"
)

type Manager struct {
	config     *config.Config
	nc         *nats.Conn
	networkMgr *NetworkManager
}

func NewManager(cfg *config.Config, nc *nats.Conn, networkMgr *NetworkManager) *Manager {
	return &Manager{
		config:     cfg,
		nc:         nc,
		networkMgr: networkMgr,
	}
}

type StartRequest struct {
	ProjectID string `json:"project_id"`
	Template  string `json:"template,omitempty"`
	// Limits, llm_token, etc... omitted for simplicity in this stub
}

func (m *Manager) StartProject(ctx context.Context, req StartRequest) error {
	wsRoot := m.config.WSRoot
	projectDir := filepath.Join(wsRoot, req.ProjectID)

	slog.Info("starting project", "id", req.ProjectID)

	// 1. Create directory structure
	workDir := filepath.Join(projectDir, "work")
	homeDir := filepath.Join(projectDir, "home", "dev")
	ctlDir := filepath.Join(projectDir, "ctl")

	for _, dir := range []string{workDir, homeDir, ctlDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create dir %s: %w", dir, err)
		}
		// ensure permissions for uid 1000 (dev user in container)
		os.Chown(dir, 1000, 1000)
	}

	// create pi config structure
	piDir := filepath.Join(homeDir, ".pi", "agent")
	os.MkdirAll(piDir, 0755)
	os.Chown(filepath.Join(homeDir, ".pi"), 1000, 1000)
	os.Chown(piDir, 1000, 1000)

	// In a real flow, extract template to workDir here

	// 2. Setup Bridge (skip for local testing if runtime is local)
	gatewayIP := "127.0.0.1"
	if m.config.Runtime != "local" {
		if err := m.networkMgr.SetupBridge(ctx); err != nil {
			return fmt.Errorf("failed to setup bridge: %w", err)
		}

		if ip, err := m.networkMgr.GetBridgeGateway(ctx); err == nil {
			gatewayIP = ip
		}
	}

	// 3. Start Container
	containerName := "forge-sbx-" + req.ProjectID

	// Remove old container if it exists
	exec.CommandContext(ctx, "docker", "rm", "-f", containerName).Run()

	args := []string{
		"run", "-d",
		"--name", containerName,
		"--runtime", m.config.Runtime,
		"--user", "1000:1000",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--read-only",
		"--tmpfs", "/tmp:rw,size=256m,mode=1777",
		"-v", fmt.Sprintf("%s:/workspace:rw", workDir),
		"-v", fmt.Sprintf("%s:/home/dev:rw", filepath.Join(projectDir, "home")),
		"-v", fmt.Sprintf("%s:/run/forge:rw", ctlDir),
		"--network", m.config.SbxBridge,
		"--dns", gatewayIP,
		"-e", "HTTP_PROXY=http://" + gatewayIP + ":3128",
		"-e", "HTTPS_PROXY=http://" + gatewayIP + ":3128",
		"-e", "NO_PROXY=localhost,127.0.0.1",
		"-e", "FORGE_PROJECT_ID=" + req.ProjectID,
		"-e", "PORT=3000",
		"-e", "HOST=0.0.0.0",
		"--workdir", "/workspace",
		"forge-sandbox:latest",
		"/usr/local/bin/forge-shim",
	}

	if m.config.FakeLLM {
		args = append(args, "--fake-agent")
	}

	var cmd *exec.Cmd
	if m.config.Runtime == "local" {
		slog.Info("running local shim instead of docker")
		shimArgs := []string{}
		if m.config.FakeLLM {
			shimArgs = append(shimArgs, "--fake-agent")
		}
		shimPath, _ := filepath.Abs("bin/forge-shim-amd64")
		cmd = exec.CommandContext(ctx, shimPath, shimArgs...)
		cmd.Env = append(os.Environ(), "FORGE_PROJECT_ID="+req.ProjectID)
		cmd.Dir = workDir

		logFile, _ := os.Create(filepath.Join(m.config.WSRoot, req.ProjectID, "shim.log"))
		cmd.Stdout = logFile
		cmd.Stderr = logFile

		os.MkdirAll(ctlDir, 0777)
		os.Chmod(ctlDir, 0777)

		err := cmd.Start()
		if err != nil {
			return fmt.Errorf("failed to start local shim: %w", err)
		}
	} else {
		slog.Info("running docker command", "args", strings.Join(args, " "))
		cmd = exec.CommandContext(ctx, "docker", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to start container: %s - %w", string(out), err)
		}
	}

	go m.streamEvents(req.ProjectID, filepath.Join(ctlDir, "ctl.sock"), m.config.Runtime)

	return nil
}

func (m *Manager) StopProject(ctx context.Context, projectID string) error {
	containerName := "forge-sbx-" + projectID
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", containerName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to stop container: %s - %w", string(out), err)
	}
	return nil
}

func (m *Manager) DestroyProject(ctx context.Context, projectID string) error {
	if err := m.StopProject(ctx, projectID); err != nil {
		slog.Warn("ignoring error stopping container", "error", err)
	}
	projectDir := filepath.Join(m.config.WSRoot, projectID)
	return os.RemoveAll(projectDir)
}

func (m *Manager) streamEvents(projectID, sockPath string, runtime string) {
	// Simple retry loop to connect to socket
	client := http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", sockPath)
			},
		},
	}

	var resp *http.Response
	var err error
	for i := 0; i < 30; i++ {
		resp, err = client.Get("http://unix/agent/events")
		if err == nil && resp.StatusCode == 200 {
			break
		}
		time.Sleep(1 * time.Second)
	}

	if err != nil || resp == nil {
		slog.Error("failed to connect to shim events stream", "error", err)
		return
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	topic := "forge.proj." + projectID + ".events"
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			slog.Error("error reading from shim stream", "error", err)
			return
		}
		m.nc.Publish(topic, line)
	}
}

func (m *Manager) HandleRPC(ctx context.Context) {
	nodeSub := "forge.node." + m.config.NodeName + ".rpc"
	_, err := m.nc.Subscribe(nodeSub, func(msg *nats.Msg) {
		var req map[string]interface{}
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			slog.Error("failed to unmarshal rpc", "error", err)
			return
		}

		op := req["op"].(string)
		slog.Info("received rpc", "op", op)

		switch op {
		case "project.start":
			var startReq StartRequest
			json.Unmarshal(msg.Data, &startReq) // ignore err for simplicity
			err := m.StartProject(ctx, startReq)
			resp := map[string]interface{}{"ok": err == nil}
			if err != nil {
				resp["error"] = err.Error()
			}
			natsutil.PublishJSON(m.nc, msg.Reply, resp)
		case "project.stop":
			projectID := req["project_id"].(string)
			err := m.StopProject(ctx, projectID)
			resp := map[string]interface{}{"ok": err == nil}
			natsutil.PublishJSON(m.nc, msg.Reply, resp)
		case "project.destroy":
			projectID := req["project_id"].(string)
			err := m.DestroyProject(ctx, projectID)
			resp := map[string]interface{}{"ok": err == nil}
			natsutil.PublishJSON(m.nc, msg.Reply, resp)
		case "agent.prompt":
			projectID := req["project_id"].(string)
			taskID := req["task_id"].(string)
			prompt := req["prompt"].(string)

			sockPath := filepath.Join(m.config.WSRoot, projectID, "ctl", "ctl.sock")
			client := http.Client{
				Transport: &http.Transport{
					DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
						return net.Dial("unix", sockPath)
					},
				},
			}

			body, _ := json.Marshal(map[string]interface{}{
				"task_id": taskID,
				"prompt":  prompt,
			})

			var respHttp *http.Response
			var err error
			for i := 0; i < 30; i++ {
				respHttp, err = client.Post("http://unix/agent/prompt", "application/json", strings.NewReader(string(body)))
				if err == nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}

			resp := map[string]interface{}{"ok": err == nil}
			if err != nil {
				slog.Error("agent.prompt failed", "error", err)
				resp["error"] = err.Error()
			} else {
				slog.Info("agent.prompt succeeded")
				respHttp.Body.Close()
			}
			slog.Info("publishing agent.prompt reply", "reply", msg.Reply)
			natsutil.PublishJSON(m.nc, msg.Reply, resp)
		}
	})
	if err != nil {
		slog.Error("failed to subscribe to node rpc", "error", err)
	} else {
		slog.Info("subscribed to node rpc", "subject", nodeSub)
	}
}
