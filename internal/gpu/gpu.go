// Package gpu detects GPUs from sysfs and picks Vulkan/PRIME environment
// defaults for hybrid laptops.
package gpu

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Vendor string

const (
	NVIDIA Vendor = "nvidia"
	AMD    Vendor = "amd"
	Intel  Vendor = "intel"
	Other  Vendor = "other"
)

type Card struct {
	Name    string // card0
	Vendor  Vendor
	Driver  string
	BootVGA bool
}

// Integrated reports whether the card is most likely an iGPU.
func (c Card) Integrated() bool {
	if c.Vendor == Intel {
		return true
	}
	// An AMD card that drives boot VGA alongside another GPU is usually an APU.
	return c.Vendor == AMD && c.BootVGA
}

var sysDRM = "/sys/class/drm"

func Detect() []Card {
	entries, _ := filepath.Glob(filepath.Join(sysDRM, "card[0-9]*"))
	var cards []Card
	for _, e := range entries {
		base := filepath.Base(e)
		if strings.Contains(base, "-") { // connectors like card0-HDMI-A-1
			continue
		}
		vendor := read(filepath.Join(e, "device", "vendor"))
		if vendor == "" {
			continue
		}
		c := Card{Name: base}
		switch vendor {
		case "0x10de":
			c.Vendor = NVIDIA
		case "0x1002":
			c.Vendor = AMD
		case "0x8086":
			c.Vendor = Intel
		default:
			c.Vendor = Other
		}
		if link, err := os.Readlink(filepath.Join(e, "device", "driver")); err == nil {
			c.Driver = filepath.Base(link)
		}
		c.BootVGA = read(filepath.Join(e, "device", "boot_vga")) == "1"
		cards = append(cards, c)
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].Name < cards[j].Name })
	return cards
}

func read(p string) string {
	b, _ := os.ReadFile(p)
	return strings.TrimSpace(string(b))
}

// Env returns environment variables that steer Vulkan/OpenGL to the chosen
// GPU on hybrid systems. pref is "auto", "discrete" or "integrated".
func Env(cards []Card, pref string) map[string]string {
	env := map[string]string{}
	if len(cards) < 2 {
		return env
	}
	var discrete *Card
	for i := range cards {
		if !cards[i].Integrated() {
			discrete = &cards[i]
			break
		}
	}
	if discrete == nil {
		return env
	}
	switch pref {
	case "auto", "discrete":
		switch {
		case discrete.Vendor == NVIDIA && discrete.Driver == "nvidia":
			env["__NV_PRIME_RENDER_OFFLOAD"] = "1"
			env["__VK_LAYER_NV_optimus"] = "NVIDIA_only"
			env["__GLX_VENDOR_LIBRARY_NAME"] = "nvidia"
		default:
			// Mesa (radv/anv/nouveau) honours DRI_PRIME for both GL and Vulkan.
			env["DRI_PRIME"] = "1"
		}
	case "integrated":
		if discrete.Vendor == NVIDIA {
			env["__NV_PRIME_RENDER_OFFLOAD"] = "0"
			env["__GLX_VENDOR_LIBRARY_NAME"] = "mesa"
		}
		env["DRI_PRIME"] = "0"
	}
	return env
}

// HasVulkan reports whether any Vulkan ICD is installed.
func HasVulkan() bool {
	for _, d := range []string{"/usr/share/vulkan/icd.d", "/etc/vulkan/icd.d", "/usr/local/share/vulkan/icd.d"} {
		if m, _ := filepath.Glob(filepath.Join(d, "*.json")); len(m) > 0 {
			return true
		}
	}
	return os.Getenv("VK_ICD_FILENAMES") != "" || os.Getenv("VK_DRIVER_FILES") != ""
}
