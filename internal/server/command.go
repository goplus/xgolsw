package server

import (
	"encoding/json"
	"fmt"
)

const (
	CommandXGoRenameResources = "xgo.renameResources"
	CommandXGoGetInputSlots   = "xgo.getInputSlots"
	CommandXGoGetProperties   = "xgo.getProperties"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#workspace_executeCommand
func (s *Server) workspaceExecuteCommand(params *ExecuteCommandParams) (any, error) {
	switch params.Command {
	case CommandXGoRenameResources:
		var cmdParams []XGoRenameResourceParams
		for _, arg := range params.Arguments {
			var cmdParam XGoRenameResourceParams
			if err := json.Unmarshal(arg, &cmdParam); err != nil {
				return nil, fmt.Errorf("failed to unmarshal command argument as XGoRenameResourceParams: %w", err)
			}
			cmdParams = append(cmdParams, cmdParam)
		}
		return s.renameResources(cmdParams)
	case CommandXGoGetInputSlots:
		var cmdParams []XGoGetInputSlotsParams
		for _, arg := range params.Arguments {
			var cmdParam XGoGetInputSlotsParams
			if err := json.Unmarshal(arg, &cmdParam); err != nil {
				return nil, fmt.Errorf("failed to unmarshal command argument as XGoGetInputSlotsParams: %w", err)
			}
			cmdParams = append(cmdParams, cmdParam)
		}
		return s.xgoGetInputSlots(cmdParams)
	case CommandXGoGetProperties:
		var cmdParams XGoGetPropertiesParams
		if len(params.Arguments) != 1 {
			return nil, fmt.Errorf("expected exactly one argument for command %s", CommandXGoGetProperties)
		}
		if err := json.Unmarshal(params.Arguments[0], &cmdParams); err != nil {
			return nil, fmt.Errorf("failed to unmarshal command argument as XGoGetPropertiesParams: %w", err)
		}
		return s.xgoGetProperties(cmdParams)
	}
	return nil, fmt.Errorf("unknown command: %s", params.Command)
}
