package owlcms

import (
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"controlpanel/shared"
)

const controlPanelStopPath = "/controlpanel/stop"

// controlPanelTokenEnv gives OWLCMS a token at launch; presenting it on stop proves the
// request comes from this control panel run. OWLCMS falls back to a local-connection
// check when it was not launched with a token.
const controlPanelTokenEnv = "OWLCMS_CONTROLPANELTOKEN"

// controlPanelToken is random, generated once per control panel run, and never logged.
// Same format as OWLCMS account tokens: 12 characters from a 64-symbol URL-safe alphabet (72 bits).
var controlPanelToken = newControlPanelToken()

const controlPanelTokenAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

func newControlPanelToken() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		log.Printf("cannot generate the OWLCMS stop token: %v", err)
		return ""
	}
	for i := range b {
		// 64 symbols divide 256 evenly, so masking keeps the distribution uniform
		b[i] = controlPanelTokenAlphabet[b[i]&63]
	}
	return string(b)
}

// StopProcessByPort asks an externally/reclaimed OWLCMS process to stop itself
// before falling back to signal-style OS termination. It does not force-kill:
// explicit in-app restart exits 1, while external stops must remain clean.
func StopProcessByPort(port string) error {
	if err := requestOwlcmsStop(port); err == nil {
		return nil
	} else {
		log.Printf("OWLCMS in-process stop failed on port %s; falling back to signal stop: %v", port, err)
	}

	pid, source, err := shared.ResolvePIDFromFileOrPort(pidFilePath, port)
	if err != nil {
		return err
	}
	if pid == 0 {
		return nil
	}

	log.Printf("Signaling OWLCMS PID %d resolved from %s", pid, source)
	if err := shared.SignalStopPID(pid, 20*time.Second); err != nil {
		return err
	}
	if strings.TrimSpace(port) != "" && shared.CheckPort(port) == nil {
		return fmt.Errorf("OWLCMS PID %d exited but port %s is still in use", pid, port)
	}
	return nil
}

func requestOwlcmsStop(port string) error {
	trimmedPort := strings.TrimSpace(port)
	if trimmedPort == "" {
		return fmt.Errorf("missing port")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%s%s", trimmedPort, controlPanelStopPath)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	if controlPanelToken != "" {
		req.Header.Set("Authorization", "Bearer "+controlPanelToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("clean stop endpoint %s is not available in this OWLCMS version", controlPanelStopPath)
		}
		if resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("OWLCMS refused the clean stop request from this host")
		}
		return fmt.Errorf("%s returned %s: %s", controlPanelStopPath, resp.Status, strings.TrimSpace(string(body)))
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if shared.CheckPort(trimmedPort) != nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("OWLCMS accepted stop request but port %s stayed open", trimmedPort)
}
