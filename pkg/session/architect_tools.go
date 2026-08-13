package session

import (
	"context"
	"fmt"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// ArchitectTools restricts a FileTools to read-only exploration —
// list_files, read_file and grep — for the Architect's Plan and Review.
//
// The Architect must never edit the repository; that is the Implementer's
// job, and the whole point of the role split is that one persistent context
// writes the brief and reviews the diff without also being the thing that
// produced it. So this isn't just an unlisted-tool convention: Call refuses
// anything outside the read-only set even if a model names a tool it was
// never offered, rather than relying on Defs() alone to keep it honest.
type ArchitectTools struct {
	files *FileTools
}

// NewArchitectTools builds a read-only view of root for the Architect.
func NewArchitectTools(root string) *ArchitectTools {
	return &ArchitectTools{files: &FileTools{Root: root}}
}

var architectReadOnlyTools = map[string]bool{
	ToolListFiles: true,
	ToolReadFile:  true,
	ToolGrep:      true,
}

func (t *ArchitectTools) Defs() []provider.ToolDef {
	all := t.files.Defs()
	defs := make([]provider.ToolDef, 0, len(architectReadOnlyTools))
	for _, d := range all {
		if architectReadOnlyTools[d.Name] {
			defs = append(defs, d)
		}
	}
	return defs
}

func (t *ArchitectTools) Call(ctx context.Context, call provider.ToolCall) (provider.ToolResult, error) {
	if !architectReadOnlyTools[call.Name] {
		return provider.ToolResult{
			Content: fmt.Sprintf("tool %q is not available here — the Architect can only list_files, read_file and grep", call.Name),
			IsError: true,
		}, nil
	}
	return t.files.Call(ctx, call)
}
