package orchestrator

import (
	"fmt"
	"net"
	"sync"
)

// PortRange defines the range of ports to allocate from
const (
	PortRangeStart = 10000
	PortRangeEnd   = 20000
)

// PortAllocator defines the interface for port allocation
type PortAllocator interface {
	Allocate(projectID, nodeID string) (int, error)
	Release(projectID, nodeID string) error
	GetAssigned(projectID, nodeID string) (int, bool)
}

// SimplePortAllocator manages port allocation for nodes
type SimplePortAllocator struct {
	mu        sync.Mutex
	allocated map[string]int  // "projectID:nodeID" -> port
	usedPorts map[int]bool    // port -> in use
	nextPort  int
}

// NewSimplePortAllocator creates a new port allocator
func NewSimplePortAllocator() *SimplePortAllocator {
	return &SimplePortAllocator{
		allocated: make(map[string]int),
		usedPorts: make(map[int]bool),
		nextPort:  PortRangeStart,
	}
}

// Allocate assigns a port to a node, reusing existing assignment if present
func (a *SimplePortAllocator) Allocate(projectID, nodeID string) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	key := fmt.Sprintf("%s:%s", projectID, nodeID)

	// Return existing allocation
	if port, exists := a.allocated[key]; exists {
		return port, nil
	}

	// Find next available port
	for attempts := 0; attempts < (PortRangeEnd - PortRangeStart); attempts++ {
		port := a.nextPort
		a.nextPort++
		if a.nextPort >= PortRangeEnd {
			a.nextPort = PortRangeStart
		}

		if a.usedPorts[port] {
			continue
		}

		// Verify port is actually available on the system
		if !isPortAvailable(port) {
			continue
		}

		a.allocated[key] = port
		a.usedPorts[port] = true
		return port, nil
	}

	return 0, fmt.Errorf("no available ports in range %d-%d", PortRangeStart, PortRangeEnd)
}

// Release frees a port allocation
func (a *SimplePortAllocator) Release(projectID, nodeID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	key := fmt.Sprintf("%s:%s", projectID, nodeID)

	if port, exists := a.allocated[key]; exists {
		delete(a.allocated, key)
		delete(a.usedPorts, port)
	}

	return nil
}

// GetAssigned returns the currently assigned port for a node, if any
func (a *SimplePortAllocator) GetAssigned(projectID, nodeID string) (int, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	key := fmt.Sprintf("%s:%s", projectID, nodeID)
	port, exists := a.allocated[key]
	return port, exists
}

// ReleaseAllForProject releases all port allocations for a project
func (a *SimplePortAllocator) ReleaseAllForProject(projectID string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	prefix := projectID + ":"
	for key, port := range a.allocated {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			delete(a.allocated, key)
			delete(a.usedPorts, port)
		}
	}
}

// isPortAvailable checks if a port is free on the system
func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
