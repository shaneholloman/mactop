// Copyright (c) 2024-2026 Carsen Klock under MIT License
// rdma.go - RDMA over Thunderbolt detection

package app

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RDMADevice holds information about a single RDMA device
type RDMADevice struct {
	Name      string `json:"name" yaml:"name" xml:"Name"`
	NodeGUID  string `json:"node_guid" yaml:"node_guid" xml:"NodeGUID"`
	Transport string `json:"transport" yaml:"transport" xml:"Transport"`
	PortState string `json:"port_state" yaml:"port_state" xml:"PortState"`
	ActiveMTU int    `json:"active_mtu" yaml:"active_mtu" xml:"ActiveMTU"`
	LinkLayer string `json:"link_layer" yaml:"link_layer" xml:"LinkLayer"`
	Interface string `json:"interface,omitempty" yaml:"interface,omitempty" xml:"Interface,omitempty"` // Mapped network interface (e.g., en2)
}

// RDMAStatus holds RDMA availability information
type RDMAStatus struct {
	Available bool         `json:"available" yaml:"available" xml:"Available"`
	Status    string       `json:"status" yaml:"status" xml:"Status"`
	Devices   []RDMADevice `json:"devices,omitempty" yaml:"devices,omitempty" xml:"Devices,omitempty"`
}

var (
	rdmaMutex      sync.Mutex
	lastRDMAStatus RDMAStatus
	lastRDMACheck  time.Time
	rdmaCacheTTL   = 10 * time.Second // Cache RDMA status for 10 seconds
)

// CheckRDMAAvailable checks if RDMA over Thunderbolt is enabled
// Uses `rdma_ctl status` command available on macOS 26.2+
func CheckRDMAAvailable() RDMAStatus {
	rdmaMutex.Lock()
	defer rdmaMutex.Unlock()

	// Return cached result if recent enough
	if time.Since(lastRDMACheck) < rdmaCacheTTL {
		return lastRDMAStatus
	}

	status := RDMAStatus{
		Available: false,
		Status:    "Unknown",
	}

	// Try to run rdma_ctl status
	cmd := exec.Command("rdma_ctl", "status")
	output, err := cmd.Output()
	if err != nil {
		// Command not found or failed - RDMA not available
		status.Status = "RDMA not available (rdma_ctl not found or macOS < 26.2)"
		lastRDMAStatus = status
		lastRDMACheck = time.Now()
		return status
	}

	result := strings.TrimSpace(string(output))
	result = strings.ToLower(result)

	if strings.Contains(result, "enabled") {
		status.Available = true
		status.Status = "RDMA Enabled"
		// Enumerate RDMA devices when enabled
		status.Devices = GetRDMADevices()
	} else if strings.Contains(result, "disabled") {
		status.Available = false
		status.Status = "RDMA Disabled (use rdma_ctl enable in Recovery Mode)"
	} else {
		status.Available = strings.Contains(result, "enabled")
		status.Status = "RDMA: " + strings.TrimSpace(string(output))
	}

	lastRDMAStatus = status
	lastRDMACheck = time.Now()
	return status
}

// GetRDMADevices enumerates RDMA devices by parsing ibv_devinfo output
func GetRDMADevices() []RDMADevice {
	var devices []RDMADevice

	cmd := exec.Command("ibv_devinfo")
	output, err := cmd.Output()
	if err != nil {
		return devices
	}

	lines := strings.Split(string(output), "\n")
	var currentDevice *RDMADevice

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// New device starts with "hca_id:"
		if strings.HasPrefix(line, "hca_id:") {
			if currentDevice != nil {
				// Derive network interface from device name (rdma_enX -> enX)
				if strings.HasPrefix(currentDevice.Name, "rdma_") {
					currentDevice.Interface = strings.TrimPrefix(currentDevice.Name, "rdma_")
				}
				devices = append(devices, *currentDevice)
			}
			currentDevice = &RDMADevice{
				Name: strings.TrimSpace(strings.TrimPrefix(line, "hca_id:")),
			}
			continue
		}

		if currentDevice == nil {
			continue
		}

		// Parse device properties
		if strings.HasPrefix(line, "transport:") {
			// Format: "transport:			Thunderbolt (100)"
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				transport := strings.TrimSpace(parts[1])
				// Remove the numeric code suffix like "(100)"
				if idx := strings.Index(transport, "("); idx > 0 {
					transport = strings.TrimSpace(transport[:idx])
				}
				currentDevice.Transport = transport
			}
		} else if strings.HasPrefix(line, "node_guid:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				currentDevice.NodeGUID = strings.TrimSpace(parts[1])
			}
		} else if strings.HasPrefix(line, "state:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				state := strings.TrimSpace(parts[1])
				// Extract state name from format "PORT_DOWN (1)"
				if idx := strings.Index(state, "("); idx > 0 {
					state = strings.TrimSpace(state[:idx])
				}
				currentDevice.PortState = state
			}
		} else if strings.HasPrefix(line, "active_mtu:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				mtuStr := strings.TrimSpace(parts[1])
				// Extract MTU value from format "4096 (5)"
				if idx := strings.Index(mtuStr, " "); idx > 0 {
					mtuStr = mtuStr[:idx]
				}
				if mtu, err := strconv.Atoi(mtuStr); err == nil {
					currentDevice.ActiveMTU = mtu
				}
			}
		} else if strings.HasPrefix(line, "link_layer:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				currentDevice.LinkLayer = strings.TrimSpace(parts[1])
			}
		}
	}

	// Don't forget the last device
	if currentDevice != nil {
		if strings.HasPrefix(currentDevice.Name, "rdma_") {
			currentDevice.Interface = strings.TrimPrefix(currentDevice.Name, "rdma_")
		}
		devices = append(devices, *currentDevice)
	}

	return devices
}
