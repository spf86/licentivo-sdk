package licentivo

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func displayDetail(value string) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value))
	runes := []rune(value)
	if len(runes) > 160 {
		return string(runes[:160])
	}
	return value
}
func machineDisplayDetails() DeviceInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := func(name string, args ...string) string {
		raw, err := exec.CommandContext(ctx, name, args...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(raw))
	}
	read := func(name string) string { raw, _ := os.ReadFile(name); return string(raw) }
	var system, cpu, gpu string
	memory, storage := platformDisplayResources()
	switch runtime.GOOS {
	case "windows":
		cpu = registryDisplayValue(command("reg.exe", "query", `HKLM\HARDWARE\DESCRIPTION\System\CentralProcessor\0`, "/v", "ProcessorNameString"), "ProcessorNameString")
		values := command("reg.exe", "query", `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "/reg:64")
		system = registryDisplayValue(values, "ProductName")
		build := registryDisplayValue(values, "CurrentBuildNumber")
		if number, _ := strconv.Atoi(build); number >= 22000 {
			system = strings.Replace(system, "Windows 10", "Windows 11", 1)
		}
		if version := registryDisplayValue(values, "DisplayVersion"); version != "" {
			system += " " + version
		}
		if build != "" {
			system += " (" + build + ")"
		}
		cards := command("reg.exe", "query", `HKLM\SYSTEM\CurrentControlSet\Control\Video`, "/s", "/f", "DriverDesc", "/t", "REG_SZ")
		seen := map[string]bool{}
		for _, line := range strings.Split(cards, "\n") {
			name := registryDisplayValue(line, "DriverDesc")
			if name != "" && !seen[name] {
				if gpu != "" {
					gpu += " / "
				}
				gpu += name
				seen[name] = true
			}
		}
	case "darwin":
		system = command("/usr/bin/sw_vers", "-productName") + " " + command("/usr/bin/sw_vers", "-productVersion")
		cpu = command("/usr/sbin/sysctl", "-n", "machdep.cpu.brand_string")
		bytes, _ := strconv.ParseInt(command("/usr/sbin/sysctl", "-n", "hw.memsize"), 10, 64)
		memory = bytes / (1024 * 1024)
	case "linux":
		for _, line := range strings.Split(read("/etc/os-release"), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				system = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
				break
			}
		}
		cpu = linuxDisplayCPU(read("/proc/cpuinfo"))
		for _, line := range strings.Split(read("/proc/meminfo"), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 1 && fields[0] == "MemTotal:" {
				kb, _ := strconv.ParseInt(fields[1], 10, 64)
				memory = kb / 1024
				break
			}
		}
	}
	return DeviceInfo{OSVersion: displayDetail(system), CPU: displayDetail(cpu), MemoryMB: memory, GPU: displayDetail(gpu), SystemType: runtime.GOARCH, StorageGB: storage}
}

func linuxDisplayCPU(raw string) string {
	fields := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
			if fields[key] == "" {
				fields[key] = value
			}
		}
	}
	for _, key := range []string{"model name", "Hardware", "Processor"} {
		if fields[key] != "" {
			return fields[key]
		}
	}
	if fields["CPU part"] != "" {
		return "ARM CPU (implementer " + fields["CPU implementer"] + ", part " + fields["CPU part"] + ")"
	}
	return ""
}
func registryDisplayValue(raw, key string) string {
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == key {
			return strings.TrimSpace(strings.Join(fields[2:], " "))
		}
	}
	return ""
}
