package cleanup

import (
	"sync"

	"go.uber.org/zap"
)

// cleanupFunc represents a function to be called during cleanup.
// It returns an error if the cleanup step fails.
type Func func() error

// cleanupManager manages the stack of cleanup functions.
type Manager struct {
	mu          sync.Mutex  // Protects access to funcs and err
	funcs       []Func      // Stack of cleanup functions (LIFO)
	err         error       // Stores the first error encountered during cleanup
	logger      *zap.Logger // Logger for reporting cleanup errors
	cleanupOnce sync.Once   // Ensures cleanup runs only once
}

// newCleanupManager creates a new cleanup manager.
func NewManager(logger *zap.Logger) *Manager {
	return &Manager{
		funcs:  make([]Func, 0),
		logger: logger,
	}
}

// add adds a function to the cleanup stack.
func (cm *Manager) Add(f Func) {
	if f == nil {
		return // Do not add nil functions
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.funcs = append(cm.funcs, f)
}

// execute runs all registered cleanup functions in reverse order (LIFO).
// It ensures cleanup runs only once and returns the first error encountered.
// It also syncs the logger if provided.
func (cm *Manager) Execute() error {
	cm.cleanupOnce.Do(func() {
		cm.mu.Lock()
		defer cm.mu.Unlock()

		cm.logger.Debug("Starting cleanup process...")
		// Run cleanup functions in reverse order (LIFO)
		for i := len(cm.funcs) - 1; i >= 0; i-- {
			f := cm.funcs[i]
			if f == nil { // Should not happen with the check in add, but belt-and-suspenders
				continue
			}
			err := f()
			if err != nil {
				if cm.err == nil {
					cm.err = err // Store the first error
					cm.logger.Error("Cleanup error encountered", zap.Error(err))
				} else {
					// Log subsequent errors but don't overwrite the first one
					cm.logger.Error("Additional cleanup error", zap.Error(err))
				}
			}
		}
		cm.logger.Debug("Cleanup process finished.")

		// Sync the logger after all cleanup attempts
		// Ignore sync error as recommended by zap docs
		_ = cm.logger.Sync()
	})
	return cm.err
}
