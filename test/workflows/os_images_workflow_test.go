package workflows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOSImagesWorkflowBuildsSupportedImages(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "os-images.yaml"))
	if err != nil {
		t.Fatalf("failed to read os-images workflow: %v", err)
	}

	text := string(data)
	for _, want := range []string{
		"arch: [amd64, arm64]",
		"image:",
		"key_prefix: kubeadm-ubuntu-24.04",
		"key_prefix: kubeadm-debian-13",
		"key_prefix: kubeadm-ubuntu-26.04",
		"key_prefix: k3s-ubuntu-24.04",
		"key_prefix: k3s-debian-13",
		"key_prefix: k3s-ubuntu-26.04",
		"runner: ubuntu-latest",
		"runner: ubuntu-24.04-arm",
		"runs-on: ${{ matrix.runner }}",
		"${{ matrix.image.key_prefix }}-${{ matrix.arch }}",
		"hack/osimage/build-kubeadm-raw.sh",
		"hack/osimage/build-k3s-raw.sh",
		"hack/artifacter/main.go",
		"packages: write",
		"${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}-${{ matrix.image.key_prefix }}-${{ matrix.arch }}",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("workflow missing %q", want)
		}
	}
}

func TestOSImageBuildScriptsHaveRequiredBehavior(t *testing.T) {
	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "kubeadm image-builder script",
			path: filepath.Join("..", "..", "hack", "osimage", "build-kubeadm-raw.sh"),
			want: []string{
				"make deps-raw",
				"IMAGE_ARCH",
				"${IMAGE_BUILDER_TARGET}",
				"kubernetes_semver=${KUBERNETES_VERSION}",
				"manifest.json",
				"qemu-img",
			},
		},
		{
			name: "k3s cloud image script",
			path: filepath.Join("..", "..", "hack", "osimage", "build-k3s-raw.sh"),
			want: []string{
				"virt-customize",
				"IMAGE_ARCH",
				"INSTALL_K3S_SKIP_START=true",
				"INSTALL_K3S_SKIP_ENABLE=true",
				"qemu-img convert -O raw",
				"manifest.json",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("failed to read script: %v", err)
			}
			text := string(data)
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Fatalf("script %s missing %q", tt.path, want)
				}
			}
		})
	}
}
