package gpu

import "testing"

func TestEnvHybridNvidia(t *testing.T) {
	cards := []Card{{Name: "card0", Vendor: NVIDIA, Driver: "nvidia"}, {Name: "card1", Vendor: Intel, Driver: "i915", BootVGA: true}}
	e := Env(cards, "auto")
	if e["__NV_PRIME_RENDER_OFFLOAD"] != "1" || e["__VK_LAYER_NV_optimus"] != "NVIDIA_only" {
		t.Fatalf("got %v", e)
	}
	if Env(cards, "integrated")["__NV_PRIME_RENDER_OFFLOAD"] != "0" {
		t.Fatal("integrated should disable offload")
	}
	if len(Env(cards[:1], "auto")) != 0 {
		t.Fatal("single GPU needs no env")
	}
	amd := []Card{{Vendor: AMD, Driver: "amdgpu"}, {Vendor: Intel, BootVGA: true}}
	if Env(amd, "auto")["DRI_PRIME"] != "1" {
		t.Fatal("AMD dGPU should use DRI_PRIME")
	}
}
