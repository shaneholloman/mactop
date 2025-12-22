package app

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type ThunderboltInfo struct {
	Items []ThunderboltBus `json:"SPThunderboltDataType"`
}

type ThunderboltBus struct {
	Name          string                 `json:"_name"`
	Vendor        string                 `json:"vendor_name_key"`
	Receptacle    *ThunderboltReceptacle `json:"receptacle_1_tag"`
	ConnectedDevs []ThunderboltDevice    `json:"_items"`
	NetworkStats  *ThunderboltNetStats   `json:"network_stats,omitempty"`
}

type ThunderboltReceptacle struct {
	Status       string `json:"receptacle_status_key"`
	CurrentSpeed string `json:"current_speed_key"`
	ReceptacleID string `json:"receptacle_id_key"`
}

type ThunderboltDevice struct {
	Name       string `json:"_name"`
	Vendor     string `json:"vendor_name_key"`
	Mode       string `json:"mode_key"`
	DeviceName string `json:"device_name_key"`
}

var cachedThunderboltInfo *ThunderboltInfo

func GetThunderboltInfo() (*ThunderboltInfo, error) {
	if cachedThunderboltInfo != nil {
		return cachedThunderboltInfo, nil
	}

	cmd := exec.Command("system_profiler", "-json", "SPThunderboltDataType")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var tbInfo ThunderboltInfo
	if err := json.Unmarshal(out, &tbInfo); err != nil {
		return nil, err
	}

	cachedThunderboltInfo = &tbInfo
	return &tbInfo, nil
}

// USB device types for SPUSBDataType
type USBInfo struct {
	Items []USBBus `json:"SPUSBDataType"`
}

type USBBus struct {
	Name       string      `json:"_name"`
	HostCtrl   string      `json:"host_controller"`
	PCIDev     string      `json:"pci_device"`
	PCIVendor  string      `json:"pci_vendor"`
	USBDevices []USBDevice `json:"_items"`
}

type USBDevice struct {
	Name         string `json:"_name"`
	Manufacturer string `json:"manufacturer"`
	ProductID    string `json:"product_id"`
	VendorID     string `json:"vendor_id"`
	Speed        string `json:"device_speed"`
	LocationID   string `json:"location_id"`
}

var cachedUSBInfo *USBInfo

func GetUSBInfo() (*USBInfo, error) {
	if cachedUSBInfo != nil {
		return cachedUSBInfo, nil
	}

	cmd := exec.Command("system_profiler", "-json", "SPUSBDataType")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var usbInfo USBInfo
	if err := json.Unmarshal(out, &usbInfo); err != nil {
		return nil, err
	}

	cachedUSBInfo = &usbInfo
	return &usbInfo, nil
}

// USBDeviceWithPort represents a USB device with its USB-C port number
type USBDeviceWithPort struct {
	Device     ThunderboltDeviceOutput
	PortNumber string // USB-C port number (maps to TB receptacle_id)
}

// GetUSBDevicesWithPorts returns external USB storage devices with their USB-C port numbers
func GetUSBDevicesWithPorts() []USBDeviceWithPort {
	// First, get USB device port mappings from ioreg
	portMap := getUSBDevicePortMap()

	// Get storage devices
	cmd := exec.Command("system_profiler", "-json", "SPStorageDataType")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var storageInfo struct {
		Items []struct {
			Name          string `json:"_name"`
			MountPoint    string `json:"mount_point"`
			PhysicalDrive struct {
				DeviceName string `json:"device_name"`
				IsInternal string `json:"is_internal_disk"`
				Protocol   string `json:"protocol"`
				MediumType string `json:"medium_type"`
			} `json:"physical_drive"`
		} `json:"SPStorageDataType"`
	}

	if err := json.Unmarshal(out, &storageInfo); err != nil {
		return nil
	}

	var devices []USBDeviceWithPort
	seen := make(map[string]bool)

	for _, vol := range storageInfo.Items {
		if vol.PhysicalDrive.IsInternal != "no" {
			continue
		}
		protocol := vol.PhysicalDrive.Protocol
		if protocol != "USB" && protocol != "Thunderbolt" {
			continue
		}

		devName := vol.PhysicalDrive.DeviceName
		if devName == "" {
			devName = vol.Name
		}
		if seen[devName] {
			continue
		}
		seen[devName] = true

		devInfo := protocol
		if vol.PhysicalDrive.MediumType != "" {
			devInfo += ", " + strings.ToUpper(vol.PhysicalDrive.MediumType)
		}

		// Look for port number in port map (match by device name substring)
		portNum := ""
		for key, port := range portMap {
			if strings.Contains(strings.ToLower(devName), strings.ToLower(key)) ||
				strings.Contains(strings.ToLower(key), strings.ToLower(devName)) {
				portNum = port
				break
			}
		}

		devices = append(devices, USBDeviceWithPort{
			Device: ThunderboltDeviceOutput{
				Name:   devName,
				Vendor: "",
				Mode:   protocol,
				Info:   devInfo,
			},
			PortNumber: portNum,
		})
	}
	return devices
}

// getUSBDevicePortMap returns a map of USB device names to their USB-C port numbers
func getUSBDevicePortMap() map[string]string {
	portMap := make(map[string]string)

	// Use ioreg to get USB device port mappings
	cmd := exec.Command("ioreg", "-r", "-c", "IOUSBHostDevice", "-a")
	out, err := cmd.Output()
	if err != nil {
		return portMap
	}

	// Parse the plist output to extract device names and port numbers
	// We look for "USB Product Name" and "UsbCPortNumber" in parent port
	outStr := string(out)

	// Simple regex-like parsing for USB Product Name and UsbCPortNumber
	lines := strings.Split(outStr, "\n")
	var currentDevice string
	for _, line := range lines {
		if strings.Contains(line, "<key>USB Product Name</key>") {
			// Next line should have the value
			continue
		}
		if strings.Contains(line, "<string>") && currentDevice == "" {
			// Extract product name
			start := strings.Index(line, "<string>")
			end := strings.Index(line, "</string>")
			if start >= 0 && end > start {
				currentDevice = line[start+8 : end]
			}
		}
	}

	// Alternative: use grep-like extraction
	cmd2 := exec.Command("bash", "-c", `ioreg -r -c IOUSBHostDevice 2>/dev/null | grep -E "USB Product Name|UsbCPortNumber" | paste - - 2>/dev/null`)
	out2, _ := cmd2.Output()
	lines2 := strings.Split(string(out2), "\n")
	for _, line := range lines2 {
		// Parse paired lines like: "USB Product Name" = "ASM236X series" ... "UsbCPortNumber" = 5
		if strings.Contains(line, "USB Product Name") && strings.Contains(line, "UsbCPortNumber") {
			// Extract product name
			nameStart := strings.Index(line, `"USB Product Name" = "`)
			if nameStart >= 0 {
				nameStart += len(`"USB Product Name" = "`)
				nameEnd := strings.Index(line[nameStart:], `"`)
				if nameEnd > 0 {
					prodName := line[nameStart : nameStart+nameEnd]
					// Extract port number
					portStart := strings.Index(line, `"UsbCPortNumber" = `)
					if portStart >= 0 {
						portStart += len(`"UsbCPortNumber" = `)
						portEnd := portStart
						for portEnd < len(line) && line[portEnd] >= '0' && line[portEnd] <= '9' {
							portEnd++
						}
						if portEnd > portStart {
							portMap[prodName] = line[portStart:portEnd]
						}
					}
				}
			}
		}
	}

	return portMap
}

// GetUSBDevicesForDisplay returns external storage devices (legacy function for compatibility)
func GetUSBDevicesForDisplay() []ThunderboltDeviceOutput {
	devicesWithPorts := GetUSBDevicesWithPorts()
	var devices []ThunderboltDeviceOutput
	for _, d := range devicesWithPorts {
		devices = append(devices, d.Device)
	}
	return devices
}

type ThunderboltOutput struct {
	Buses []ThunderboltBusOutput `json:"buses"`
}

type ThunderboltBusOutput struct {
	Name         string                    `json:"name"`
	Status       string                    `json:"status"` // Active, Inactive
	Icon         string                    `json:"icon"`   // ⚡, ○
	Speed        string                    `json:"speed,omitempty"`
	Devices      []ThunderboltDeviceOutput `json:"devices,omitempty"`
	NetworkStats *ThunderboltNetStats      `json:"network_stats,omitempty"`
}

type ThunderboltDeviceOutput struct {
	Name   string `json:"name"`
	Vendor string `json:"vendor,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Info   string `json:"info_string,omitempty"`
}

// GetFormattedThunderboltInfo returns a structured representation for JSON output
func GetFormattedThunderboltInfo() (*ThunderboltOutput, error) {
	info, err := GetThunderboltInfo()
	if err != nil {
		return nil, err
	}

	// First pass: detect machine's max port capability
	// Check for TB5 (120 Gb/s or 80 Gb/s) based on any port's speed
	maxPortCapability := "TB4" // Default for modern Macs
	for _, bus := range info.Items {
		if bus.Receptacle != nil {
			speed := bus.Receptacle.CurrentSpeed
			if strings.Contains(speed, "120") || strings.Contains(speed, "80 Gb") {
				maxPortCapability = "TB5"
				break
			}
		}
	}

	output := &ThunderboltOutput{}
	for _, bus := range info.Items {
		// Extract bus number from name
		busNum := ""
		if strings.Contains(bus.Name, "_bus_") {
			parts := strings.Split(bus.Name, "_bus_")
			if len(parts) > 1 {
				busNum = parts[1]
			}
		}

		isActive := false
		speed := ""
		activeProtocol := "" // The protocol the connection is actually running at

		if bus.Receptacle != nil {
			if bus.Receptacle.Status == "receptacle_connected" {
				isActive = true
			}
			if bus.Receptacle.CurrentSpeed != "" {
				speed = bus.Receptacle.CurrentSpeed
				if isActive && !strings.Contains(speed, "Up to") {
					if strings.Contains(speed, "120") || strings.Contains(speed, "80") {
						activeProtocol = "TB5"
					} else if strings.Contains(speed, "40") {
						activeProtocol = "TB4"
					} else if strings.Contains(speed, "20") {
						activeProtocol = "TB3"
					}
				}
			}
		} else if len(bus.ConnectedDevs) > 0 {
			isActive = true
		}

		// Build bus label: show capability, and if active at different protocol, show that too
		var busLabel string
		if isActive && activeProtocol != "" && activeProtocol != maxPortCapability {
			busLabel = fmt.Sprintf("%s @ %s Bus %s", maxPortCapability, activeProtocol, busNum)
		} else {
			busLabel = fmt.Sprintf("%s Bus %s", maxPortCapability, busNum)
		}

		statusStr := "Inactive"
		icon := "○"
		if isActive {
			statusStr = "Active"
			icon = "ϟ"
		}

		busOut := ThunderboltBusOutput{
			Name:   busLabel,
			Status: statusStr,
			Icon:   icon,
			Speed:  speed,
		}

		for _, dev := range bus.ConnectedDevs {
			devName := dev.Name
			if devName == "" {
				devName = dev.DeviceName
			}

			devInfo := ""
			if dev.Vendor != "" {
				devInfo = fmt.Sprintf("%s", dev.Vendor)
			}
			modePretty := ""
			if dev.Mode != "" {
				// Convert to short format: "Thunderbolt 3" -> "TB3"
				mode := strings.ToLower(dev.Mode)
				mode = strings.ReplaceAll(mode, "_", " ")
				switch {
				case strings.Contains(mode, "thunderbolt 5") || strings.Contains(mode, "thunderbolt5") || strings.Contains(mode, "thunderbolt five"):
					modePretty = "TB5"
				case strings.Contains(mode, "thunderbolt 4") || strings.Contains(mode, "thunderbolt4") || strings.Contains(mode, "thunderbolt four"):
					modePretty = "TB4"
				case strings.Contains(mode, "thunderbolt 3") || strings.Contains(mode, "thunderbolt3") || strings.Contains(mode, "thunderbolt three"):
					modePretty = "TB3"
				case strings.Contains(mode, "usb4") || strings.Contains(mode, "usb 4"):
					modePretty = "USB4"
				default:
					modePretty = strings.Title(strings.ReplaceAll(dev.Mode, "_", " "))
				}
				if devInfo != "" {
					devInfo += ", " + modePretty
				} else {
					devInfo = modePretty
				}
			}

			busOut.Devices = append(busOut.Devices, ThunderboltDeviceOutput{
				Name:   devName,
				Vendor: dev.Vendor,
				Mode:   modePretty,
				Info:   devInfo,
			})
		}
		output.Buses = append(output.Buses, busOut)
	}

	// Add USB storage devices to matching TB buses based on USB-C port numbers
	usbDevicesWithPorts := GetUSBDevicesWithPorts()
	for _, usbDev := range usbDevicesWithPorts {
		matched := false
		// Try to match by USB-C port number to TB bus receptacle ID
		if usbDev.PortNumber != "" {
			for i := range output.Buses {
				// Get receptacle ID for this bus from original info
				for _, bus := range info.Items {
					busNum := ""
					if strings.Contains(bus.Name, "_bus_") {
						parts := strings.Split(bus.Name, "_bus_")
						if len(parts) > 1 {
							busNum = parts[1]
						}
					}
					// Match bus by number
					if strings.HasSuffix(output.Buses[i].Name, "Bus "+busNum) && bus.Receptacle != nil {
						if bus.Receptacle.ReceptacleID == usbDev.PortNumber {
							output.Buses[i].Devices = append(output.Buses[i].Devices, usbDev.Device)
							if output.Buses[i].Status == "Inactive" {
								output.Buses[i].Status = "Active (USB)"
								output.Buses[i].Icon = "⏺"
							}
							matched = true
							break
						}
					}
				}
				if matched {
					break
				}
			}
		}
		// Fallback: add to first inactive bus if no match
		if !matched {
			for i := range output.Buses {
				if output.Buses[i].Status == "Inactive" && len(output.Buses[i].Devices) == 0 {
					output.Buses[i].Devices = append(output.Buses[i].Devices, usbDev.Device)
					output.Buses[i].Status = "Active (USB)"
					output.Buses[i].Icon = "⏺"
					break
				}
			}
		}
	}

	return output, nil
}

func (t *ThunderboltInfo) Description() string {
	formatted, err := GetFormattedThunderboltInfo()
	if err != nil {
		return "Error loading Thunderbolt info."
	}
	if len(formatted.Buses) == 0 {
		return "No Thunderbolt controllers found."
	}

	var sb strings.Builder
	for _, bus := range formatted.Buses {
		speedStr := ""
		if bus.Speed != "" {
			speedStr = " @ " + bus.Speed
		}
		sb.WriteString(fmt.Sprintf("%s %s (%s)%s\n", bus.Icon, bus.Name, bus.Status, speedStr))

		if len(bus.Devices) > 0 {
			for i, dev := range bus.Devices {
				prefix := "  ├─"
				if i == len(bus.Devices)-1 {
					prefix = "  └─"
				}
				if dev.Info != "" {
					sb.WriteString(fmt.Sprintf("%s %s (%s)\n", prefix, dev.Name, dev.Info))
				} else {
					sb.WriteString(fmt.Sprintf("%s %s\n", prefix, dev.Name))
				}
			}
		}
	}

	return strings.TrimSpace(sb.String())
}
