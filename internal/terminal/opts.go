package terminal

import "github.com/aether-dev/aether/internal/types"

type SpawnOpts struct {
	Kind        types.TerminalKind `json:"kind"`
	OwnerID     types.ID           `json:"ownerId"`
	WorkspaceID types.ID           `json:"workspaceId"`
	Cwd         string             `json:"cwd"`
	Shell       string             `json:"shell"`
	Cols        int                `json:"cols"`
	Rows        int                `json:"rows"`
	Title       string             `json:"title"`
	Persistent  bool               `json:"persistent"`
	SSH         *types.SSHTarget   `json:"ssh,omitempty"`
	Command     []string           `json:"command,omitempty"`
}
