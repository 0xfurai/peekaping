package api_key

import (
	"context"

	"peekaping/internal/modules/workspace"
)

// testWorkspace is the workspace the tests' requests run in
const testWorkspace = workspace.ID("0d6f1e0e-6b1c-4d49-8c37-0b5f4a1b3a01")

// adminContext is the context of a request of an admin of testWorkspace
func adminContext() context.Context {
	return workspace.WithContext(context.Background(), testWorkspace, workspace.RoleAdmin)
}
