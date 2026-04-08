package service

import (
	"fmt"
	"olt-monitor/internal/domain"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const onuInitializationDelay = 3 * time.Second

type ProvisioningService struct {
	oltManager *OLTManager
	telnet     *TelnetService
}

func NewProvisioningService(manager *OLTManager, telnet *TelnetService) *ProvisioningService {
	return &ProvisioningService{
		oltManager: manager,
		telnet:     telnet,
	}
}

// GetUnconfiguredONUs fetches unconfigured ONUs from the OLT via Telnet
// For ZTE: shows gpon onu uncfg
func (s *ProvisioningService) GetUnconfiguredONUs(oltID string) ([]domain.UnconfiguredONU, error) {
	olt, err := s.oltManager.GetOLT(oltID)
	if err != nil {
		return nil, fmt.Errorf("OLT %s not found: %w", oltID, err)
	}

	// Connect to Telnet
	sess, err := s.telnet.Connect(olt.Config)
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	// Execute command to find unconfigured ONUs
	output, err := sess.SendCommand("show gpon onu uncfg")
	if err != nil {
		return nil, err
	}

	return parseUnconfiguredOutput(output)
}

// ProvisionONU executes the provisioning flow
func (s *ProvisioningService) ProvisionONU(req domain.ProvisioningRequest) (*domain.ProvisioningResponse, error) {
	logs, addLog := newProvisioningLogger()
	addLog("Init", "-", "info", fmt.Sprintf("Starting provisioning for SN: %s on %s", req.SN, req.OLTID))

	olt, commands, hydratedReq, err := s.prepareProvisioning(req)
	if err != nil {
		addLog("Template", "-", "failed", err.Error())
		return &domain.ProvisioningResponse{Success: false, Message: err.Error(), Logs: *logs}, nil
	}

	addLog("Connect", "-", "info", "Connecting to OLT Telnet...")
	sess, err := s.telnet.Connect(olt.Config)
	if err != nil {
		addLog("Connect", "-", "failed", err.Error())
		return &domain.ProvisioningResponse{Success: false, Message: "Telnet Connection Failed", Logs: *logs}, nil
	}
	defer sess.Close()

	addLog("Connect", "-", "success", "Connected to OLT")
	if shouldWaitForONUInitialization(commands) {
		addLog("Wait", "-", "info", "Waiting 3s for ONU initialization...")
		time.Sleep(onuInitializationDelay)
	}

	output, execErr := executeProvisioningBatch(sess, commands)
	if execErr != nil {
		addLog("Batch", strings.Join(commands, "\n"), "failed", execErr.Error())
		return &domain.ProvisioningResponse{Success: false, Message: "Provisioning failed during batch execution", Logs: *logs}, nil
	}
	if validationErr := validateProvisioningOutput(output); validationErr != nil {
		addLog("Batch", strings.Join(commands, "\n"), "failed", validationErr.Error())
		return &domain.ProvisioningResponse{Success: false, Message: "Provisioning stopped due to OLT error", Logs: *logs}, nil
	}

	appendProvisioningStepLogs(logs, commands)
	addLog("Finish", "-", "success", fmt.Sprintf("Provisioning completed successfully for template %s", hydratedReq.TemplateID))

	return &domain.ProvisioningResponse{Success: true, Message: "Provisioning Successful", Logs: *logs}, nil
}

// PreviewProvisioning generates the command list without executing it.
func (s *ProvisioningService) PreviewProvisioning(req domain.ProvisioningRequest) ([]string, error) {
	_, commands, _, err := s.prepareProvisioning(req)
	return commands, err
}

func (s *ProvisioningService) prepareProvisioning(req domain.ProvisioningRequest) (*domain.OLTInstance, []string, domain.ProvisioningRequest, error) {
	olt, err := s.oltManager.GetOLT(req.OLTID)
	if err != nil {
		return nil, nil, req, fmt.Errorf("OLT not found")
	}

	hydratedReq := req
	applyVLANProfiles(olt, &hydratedReq)
	commands, err := generateProvisioningCommands(hydratedReq)
	if err != nil {
		return nil, nil, hydratedReq, err
	}

	return olt, commands, hydratedReq, nil
}

func newProvisioningLogger() (*[]domain.ProvisioningLog, func(step, cmd, status, msg string)) {
	logs := &[]domain.ProvisioningLog{}
	addLog := func(step, cmd, status, msg string) {
		*logs = append(*logs, domain.ProvisioningLog{
			Step:    step,
			Command: cmd,
			Status:  status,
			Message: msg,
			Time:    time.Now().Format("15:04:05"),
		})
	}

	return logs, addLog
}

func shouldWaitForONUInitialization(commands []string) bool {
	for _, cmd := range commands {
		if strings.HasPrefix(strings.TrimSpace(cmd), "pon-onu-mng") {
			return true
		}
	}

	return false
}

func executeProvisioningBatch(sess *TelnetSession, commands []string) (string, error) {
	return sess.SendCommands(commands)
}

func validateProvisioningOutput(output string) error {
	lowerOutput := strings.ToLower(output)
	if strings.Contains(lowerOutput, "error") || strings.Contains(lowerOutput, "invalid") {
		return fmt.Errorf("OLT Error: %s", output)
	}

	return nil
}

func appendProvisioningStepLogs(logs *[]domain.ProvisioningLog, commands []string) {
	for index, cmd := range commands {
		*logs = append(*logs, domain.ProvisioningLog{
			Step:    fmt.Sprintf("Step %d", index+1),
			Command: cmd,
			Status:  "success",
			Message: "OK",
			Time:    time.Now().Format("15:04:05"),
		})
	}
}

// Helpers

func parseUnconfiguredOutput(raw string) ([]domain.UnconfiguredONU, error) {
	var onus []domain.UnconfiguredONU

	reTable := regexp.MustCompile(`gpon-onu_\d+/(\d+)/(\d+):(\d+)\s+([A-Za-z0-9]+)`)
	reKeyValue := regexp.MustCompile(`gpon-onu_\d+/(\d+)/(\d+):(\d+)\s+.*?SN:([A-Za-z0-9]+)`)

	for _, line := range strings.Split(raw, "\n") {
		onu, ok := parseUnconfiguredLine(strings.TrimSpace(line), reTable, reKeyValue)
		if ok {
			onus = append(onus, onu)
		}
	}

	return onus, nil
}

func parseUnconfiguredLine(line string, reTable, reKeyValue *regexp.Regexp) (domain.UnconfiguredONU, bool) {
	if !strings.HasPrefix(line, "gpon-onu_") {
		return domain.UnconfiguredONU{}, false
	}

	if strings.Contains(line, "SN:") {
		return parseUnconfiguredMatch(reKeyValue.FindStringSubmatch(line))
	}

	if matches := reTable.FindStringSubmatch(line); len(matches) >= 5 {
		return parseUnconfiguredMatch(matches)
	}

	return parseUnconfiguredMatch(reKeyValue.FindStringSubmatch(line))
}

func parseUnconfiguredMatch(matches []string) (domain.UnconfiguredONU, bool) {
	if len(matches) < 5 {
		return domain.UnconfiguredONU{}, false
	}

	board, err := strconv.Atoi(matches[1])
	if err != nil {
		return domain.UnconfiguredONU{}, false
	}

	pon, err := strconv.Atoi(matches[2])
	if err != nil {
		return domain.UnconfiguredONU{}, false
	}

	onuID, err := strconv.Atoi(matches[3])
	if err != nil {
		return domain.UnconfiguredONU{}, false
	}

	return domain.UnconfiguredONU{
		Board: board,
		PON:   pon,
		ONUID: onuID,
		SN:    matches[4],
		Type:  "ZTE",
		State: "Unconfigured",
	}, true
}

// FindNextOnuID scans the OLT for used ONU IDs on a specific PON and returns the next highest available ID.
func (s *ProvisioningService) FindNextOnuID(oltID string, board, pon int) (int, error) {
	olt, err := s.oltManager.GetOLT(oltID)
	if err != nil {
		return 0, fmt.Errorf("OLT not found")
	}

	sess, err := s.telnet.Connect(olt.Config)
	if err != nil {
		return 0, err
	}
	defer sess.Close()

	// Command to list ONUs on specific PON
	// ZTE output format needs to be parsed specifically.
	// "show gpon onu baseinfo gpon-olt_1/slot/port" usually gives a list.
	cmd := fmt.Sprintf("show gpon onu baseinfo gpon-olt_1/%d/%d", board, pon)
	out, err := sess.SendCommand(cmd)
	if err != nil {
		return 0, err
	}

	// Parse configured IDs
	// Output Example:
	// ...
	// gpon-onu_1/2/1:1 ...
	// gpon-onu_1/2/1:2 ...
	// ...

	// Regex matches: gpon-onu_\d+/\d+/\d+:(\d+)
	// We want to capture the ID part.
	re := regexp.MustCompile(fmt.Sprintf(`gpon-onu_\d+/%d/%d:(\d+)`, board, pon))
	matches := re.FindAllStringSubmatch(out, -1)

	maxID := 0
	for _, m := range matches {
		if len(m) > 1 {
			id, err := strconv.Atoi(m[1])
			if err == nil {
				if id > maxID {
					maxID = id
				}
			}
		}
	}

	// Return next available
	return maxID + 1, nil
}
