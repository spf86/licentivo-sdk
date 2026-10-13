package licentivo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// MachineDeviceID reads the OS identity each time; it never trusts an ID copied with the app.
// Derive identity from the local system, never from a copied configuration or cache.
func SystemDeviceID(product string) (string, error) {
	var identity string
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	switch runtime.GOOS {
	case "windows":
		b, err := exec.CommandContext(ctx, "reg.exe", "query", `HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid", "/reg:64").Output()
		if err == nil {
			m := regexp.MustCompile(`(?i)MachineGuid\s+REG_SZ\s+([0-9a-f-]+)`).FindStringSubmatch(string(b))
			if len(m) == 2 {
				identity = m[1]
			}
		}
	case "linux":
		for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			if b, err := os.ReadFile(path); err == nil {
				identity = strings.TrimSpace(string(b))
				if identity != "" {
					break
				}
			}
		}
	case "darwin":
		b, err := exec.CommandContext(ctx, "/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
		if err == nil {
			m := regexp.MustCompile(`"IOPlatformUUID"\s*=\s*"([0-9A-Fa-f-]+)"`).FindStringSubmatch(string(b))
			if len(m) == 2 {
				identity = m[1]
			}
		}
	}
	identity = strings.ToLower(strings.TrimSpace(identity))
	if !regexp.MustCompile(`^(?:[0-9a-f]{32}|[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})$`).MatchString(identity) || strings.Trim(strings.ReplaceAll(identity, "-", ""), "0") == "" {
		return "", errors.New("OS machine identity unavailable; cannot bind license to this device")
	}
	return licenseHash("LicenovaDevice/v2\n" + strings.ToLower(product) + "\n" + identity), nil
}

type DeviceProfile struct {
	Version        int    `json:"version"`
	SystemID       string `json:"system_id"`
	HardwareID     string `json:"hardware_id"`
	VirtualMachine bool   `json:"virtual_machine"`
	Platform       string `json:"platform"`
}

func MachineProfile(product string) (DeviceProfile, string, error) {
	system, err := SystemDeviceID(product)
	if err != nil {
		return DeviceProfile{}, "", err
	}
	hardware, description := "", ""
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch runtime.GOOS {
	case "windows":
		output, e := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "(Get-CimInstance Win32_ComputerSystemProduct).UUID; $v=Get-CimInstance Win32_ComputerSystem; $v.Manufacturer; $v.Model").Output()
		if e == nil {
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			if len(lines) > 0 {
				hardware = lines[0]
			}
			description = string(output)
		}
	case "linux":
		if b, e := os.ReadFile("/sys/class/dmi/id/product_uuid"); e == nil {
			hardware = string(b)
		}
		for _, path := range []string{"/sys/class/dmi/id/sys_vendor", "/sys/class/dmi/id/product_name"} {
			if b, e := os.ReadFile(path); e == nil {
				description += " " + string(b)
			}
		}
		if _, e := os.Stat("/.dockerenv"); e == nil {
			description += " virtual machine"
		}
	case "darwin":
		if b, e := exec.CommandContext(ctx, "/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output(); e == nil {
			m := regexp.MustCompile(`"IOPlatformUUID"\s*=\s*"([0-9A-Fa-f-]+)"`).FindStringSubmatch(string(b))
			if len(m) == 2 {
				hardware = m[1]
			}
			description = string(b)
		}
	}
	hardware = strings.ToLower(strings.TrimSpace(hardware))
	hw := ""
	if regexp.MustCompile(`^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$`).MatchString(hardware) && strings.Trim(strings.ReplaceAll(hardware, "-", ""), "0f") != "" {
		hw = licenseHash("LicenovaHardware/v1\n" + strings.ToLower(product) + "\n" + hardware)
	}
	vm := regexp.MustCompile(`(?i)vmware|virtualbox|virtual machine|kvm|qemu|xen|parallels`).MatchString(description)
	profile := DeviceProfile{3, system, hw, vm, runtime.GOOS}
	return profile, licenseHash("LicenovaDevice/v3\n" + strings.ToLower(product) + "\n" + system + "\n" + hw), nil
}
func MachineDeviceID(product string) (string, error) {
	_, id, err := MachineProfile(product)
	return id, err
}
