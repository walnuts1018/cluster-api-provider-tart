//go:build e2e

package framework

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	testutils "github.com/walnuts1018/cluster-api-provider-tart/test/utils"
)

// DumpAllは、失敗したE2E specのため、既存test/utils/debug.goのcluster全般dumpに加えて
// bare-metal lab固有のartifact(QEMU serial console, libvirt/QEMU domain定義,
// wol-libvirt-gatewayログ, netboot-serverログ)を1箇所のディレクトリへまとめて保存する。
// 個々の収集はbest-effortであり、1つの失敗が他の収集を止めないようにする。
func DumpAll(artifactDir string) {
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: failed to create artifact directory %q: %v\n", artifactDir, err)
		return
	}

	if err := testutils.DumpClusterState(filepath.Join(artifactDir, "cluster-state")); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: failed to dump cluster state: %v\n", err)
	}
	if err := testutils.DumpControllerLogs(filepath.Join(artifactDir, "controller-logs")); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: failed to dump controller logs: %v\n", err)
	}
	// testutils.DumpControllerLogsはlabel(control-plane=controller-manager)で絞り込むため、
	// 異なるlabelを持つpod(netboot-server等)や、CAPI core/cert-manager等の他namespaceのcrashは
	// 拾えない。関連する全namespaceのpod logをlabelに依存せずまとめて収集する。
	for _, namespace := range []string{
		tartNamespace,
		"capi-system",
		"capi-kubeadm-bootstrap-system",
		"capi-kubeadm-control-plane-system",
		"cert-manager",
	} {
		if err := dumpNamespacePodLogs(namespace, filepath.Join(artifactDir, "pod-logs", namespace)); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to dump pod logs for namespace %q: %v\n", namespace, err)
		}
	}
	if err := testutils.DumpDnsmasqState(filepath.Join(artifactDir, "network-state")); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: failed to dump dnsmasq/network state: %v\n", err)
	}
	if err := dumpLibvirtState(filepath.Join(artifactDir, "libvirt-state")); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: failed to dump libvirt state: %v\n", err)
	}
	if err := dumpSerialConsoleLogs(filepath.Join(artifactDir, "serial-console")); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: failed to dump serial console logs: %v\n", err)
	}
}

// dumpLibvirtStateは`virsh dumpxml`/`virsh list --all`相当の情報を保存する。virshが利用できない
// (linux runner以外の)環境ではbest-effortでskipする。
func dumpLibvirtState(artifactDir string) error {
	if _, err := exec.LookPath("virsh"); err != nil {
		//nolint:nilerr // virshが存在しない環境(linux runner以外)ではbest-effortでskipする意図的な仕様
		return nil
	}
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}

	listOutput, err := exec.Command("virsh", "--connect", "qemu:///system", "list", "--all").CombinedOutput()
	if err != nil {
		return fmt.Errorf("virsh list: %w: %s", err, string(listOutput))
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "domains.txt"), listOutput, 0o644); err != nil {
		return fmt.Errorf("write virsh list output: %w", err)
	}

	netOutput, err := exec.Command("virsh", "--connect", "qemu:///system", "net-list", "--all").CombinedOutput()
	if err == nil {
		if writeErr := os.WriteFile(filepath.Join(artifactDir, "networks.txt"), netOutput, 0o644); writeErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to write virsh net-list output: %v\n", writeErr)
		}
	}

	names, err := domainNamesFromVirshList(listOutput)
	if err != nil {
		return err
	}
	for _, name := range names {
		dumpOutput, dumpErr := exec.Command("virsh", "--connect", "qemu:///system", "dumpxml", name).CombinedOutput()
		if dumpErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to dumpxml domain %q: %v\n", name, dumpErr)
			continue
		}
		if writeErr := os.WriteFile(filepath.Join(artifactDir, name+".xml"), dumpOutput, 0o644); writeErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to write dumpxml for %q: %v\n", name, writeErr)
		}
	}
	return nil
}

// dumpNamespacePodLogsは指定されたnamespace内の全podについて、現在のcontainer logと直前の
// (crashした場合の)container logを収集する。namespace自体が存在しない場合はbest-effortでskipする。
func dumpNamespacePodLogs(namespace, artifactDir string) error {
	output, err := exec.Command("kubectl", "get", "pods", "-n", namespace,
		"-o", "go-template={{ range .items }}{{ .metadata.name }}{{ \"\\n\" }}{{ end }}").CombinedOutput()
	if err != nil {
		//nolint:nilerr // namespace未作成(providerがそもそもinstallされていない)場合のbest-effort skip
		return nil
	}
	podNames := testutils.GetNonEmptyLines(string(output))
	if len(podNames) == 0 {
		return nil
	}
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}

	for _, podName := range podNames {
		logOutput, logErr := exec.Command("kubectl", "logs", podName, "-n", namespace, "--all-containers", "--tail=500").CombinedOutput()
		if logErr == nil {
			if writeErr := os.WriteFile(filepath.Join(artifactDir, podName+".log"), logOutput, 0o644); writeErr != nil {
				_, _ = fmt.Fprintf(os.Stderr, "warning: failed to write log for pod %s/%s: %v\n", namespace, podName, writeErr)
			}
		}

		prevLogOutput, prevErr := exec.Command("kubectl", "logs", podName, "-n", namespace, "--all-containers", "--previous", "--tail=500").CombinedOutput()
		if prevErr == nil && len(prevLogOutput) > 0 {
			if writeErr := os.WriteFile(filepath.Join(artifactDir, podName+"-previous.log"), prevLogOutput, 0o644); writeErr != nil {
				_, _ = fmt.Fprintf(os.Stderr, "warning: failed to write previous log for pod %s/%s: %v\n", namespace, podName, writeErr)
			}
		}

		descOutput, descErr := exec.Command("kubectl", "describe", "pod", podName, "-n", namespace).CombinedOutput()
		if descErr == nil {
			if writeErr := os.WriteFile(filepath.Join(artifactDir, podName+"-describe.txt"), descOutput, 0o644); writeErr != nil {
				_, _ = fmt.Fprintf(os.Stderr, "warning: failed to write describe for pod %s/%s: %v\n", namespace, podName, writeErr)
			}
		}
	}
	return nil
}

// domainNamesFromVirshListは`virsh list --all`の簡易parseでdomain名を抽出する。
// 出力形式は" Id   Name    State"のヘッダ行を含むテーブルであり、2列目がdomain名である。
func domainNamesFromVirshList(output []byte) ([]string, error) {
	lines := testutils.GetNonEmptyLines(string(output))
	var names []string
	for _, line := range lines {
		fields := splitFields(line)
		if len(fields) < 2 {
			continue
		}
		// ヘッダ行("Id Name State")と区切り線を除外する。
		if fields[0] == "Id" || fields[0] == "-" {
			continue
		}
		names = append(names, fields[1])
	}
	return names, nil
}

func splitFields(line string) []string {
	var fields []string
	current := ""
	for _, r := range line {
		if r == ' ' || r == '\t' {
			if current != "" {
				fields = append(fields, current)
				current = ""
			}
			continue
		}
		current += string(r)
	}
	if current != "" {
		fields = append(fields, current)
	}
	return fields
}

// dumpSerialConsoleLogsは、lab.Config.WorkDir配下にlibvirtlabが書き出すserial-console.logを
// artifactDirへコピーする。E2Eのlab配線がWorkDirを明示的に共有する前提であり、テスト側で
// SerialConsoleSourceDirを設定して呼び出す。
func dumpSerialConsoleLogs(artifactDir string) error {
	sourceDir := os.Getenv("TART_E2E_LAB_WORKDIR")
	if sourceDir == "" {
		// TODO: BeforeSuiteでlab.Config.WorkDirを決定した後、この環境変数へ設定する配線を追加する。
		// lab未初期化のspec(unit的なframeworkテスト等)ではskipして問題ない。
		return nil
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return fmt.Errorf("read lab work directory %q: %w", sourceDir, err)
	}
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		logPath := filepath.Join(sourceDir, entry.Name(), "serial-console.log")
		data, readErr := os.ReadFile(logPath)
		if readErr != nil {
			continue
		}
		if writeErr := os.WriteFile(filepath.Join(artifactDir, entry.Name()+"-serial-console.log"), data, 0o644); writeErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to copy serial console log for %q: %v\n", entry.Name(), writeErr)
		}
	}
	return nil
}
