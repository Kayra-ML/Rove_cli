package goal

import "github.com/Kayra-ML/rove/internal/types"

// Running reports whether a goal is being driven now.
func (e *Engine) Running(goalID types.ID) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.running[goalID]
	return ok
}
