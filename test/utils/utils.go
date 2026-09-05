package utils

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:staticcheck
)

const (
	certmanagerVersion = "v1.21.1"
	certmanagerURLTmpl = "https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml"

	defaultKindBinary  = "kind"
	defaultKindCluster = "kind"
)

func WarnError(err error) {
	if _, writeErr := fmt.Fprintf(GinkgoWriter, "warning: %v\n", err); writeErr != nil {
		log.Printf("failed to write warning to GinkgoWriter: %v", writeErr)
	}
}

// Runは指定されたcommandをこのcontextで実行する。
func Run(cmd *exec.Cmd) (string, error) {
	dir, _ := GetProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		if _, writeErr := fmt.Fprintf(GinkgoWriter, "chdir dir: %q\n", err); writeErr != nil {
			log.Printf("failed to write chdir error to GinkgoWriter: %v", writeErr)
		}
	}

	env := cmd.Env
	if len(env) == 0 {
		env = os.Environ()
	}
	cmd.Env = append(env, "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	if _, err := fmt.Fprintf(GinkgoWriter, "running: %q\n", command); err != nil {
		log.Printf("failed to write command to GinkgoWriter: %v", err)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%q failed with error %q: %w", command, string(output), err)
	}

	return string(output), nil
}

// UninstallCertManagerはcert-managerを削除する。
func UninstallCertManager() {
	ctx := context.Background()
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.CommandContext(ctx, "kubectl", "delete", "-f", url)
	if _, err := Run(cmd); err != nil {
		WarnError(err)
	}

	// 既定では削除されないkube-systemの残存leaseを削除する。
	kubeSystemLeases := []string{
		"cert-manager-cainjector-leader-election",
		"cert-manager-controller",
	}
	for _, lease := range kubeSystemLeases {
		cmd = exec.CommandContext(ctx, "kubectl", "delete", "lease", lease,
			"-n", "kube-system", "--ignore-not-found", "--force", "--grace-period=0")
		if _, err := Run(cmd); err != nil {
			WarnError(err)
		}
	}
}

// InstallCertManagerはcert-manager bundleをinstallする。
func InstallCertManager() error {
	ctx := context.Background()
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", url)
	if _, err := Run(cmd); err != nil {
		return err
	}
	// clusterからcert-managerを削除した後に再installした場合は時間がかかるため、cert-manager-webhookがReadyになるまで待機する。
	cmd = exec.CommandContext(ctx, "kubectl", "wait", "deployment.apps/cert-manager-webhook",
		"--for", "condition=Available",
		"--namespace", "cert-manager",
		"--timeout", "5m",
	)
	if _, err := Run(cmd); err != nil {
		return err
	}

	return waitCertManagerWebhookCABundle(ctx)
}

func waitCertManagerWebhookCABundle(ctx context.Context) error {
	webhooks := []struct {
		resource string
		name     string
	}{
		{
			resource: "mutatingwebhookconfigurations.admissionregistration.k8s.io",
			name:     "cert-manager-webhook",
		},
		{
			resource: "validatingwebhookconfigurations.admissionregistration.k8s.io",
			name:     "cert-manager-webhook",
		},
	}
	for _, webhook := range webhooks {
		deadline := time.Now().Add(5 * time.Minute)
		for {
			cmd := exec.CommandContext(ctx, "kubectl", "get", webhook.resource, webhook.name,
				"-o", "jsonpath={.webhooks[0].clientConfig.caBundle}",
			)
			output, err := Run(cmd)
			if err == nil && strings.TrimSpace(output) != "" {
				break
			}
			if time.Now().After(deadline) {
				if err != nil {
					return err
				}
				return fmt.Errorf("%s/%s CA bundle was not injected", webhook.resource, webhook.name)
			}
			time.Sleep(time.Second)
		}
	}
	return nil
}

// IsCertManagerCRDsInstalledはcert-managerに関連する主要なCRDの存在を確認し、cert-manager CRDがinstall済みかを返す。
func IsCertManagerCRDsInstalled() bool {
	// 一般的なcert-manager CRDのlist。
	certManagerCRDs := []string{
		"certificates.cert-manager.io",
		"issuers.cert-manager.io",
		"clusterissuers.cert-manager.io",
		"certificaterequests.cert-manager.io",
		"orders.acme.cert-manager.io",
		"challenges.acme.cert-manager.io",
	}

	// 全CRDを取得するkubectl commandを実行する。
	cmd := exec.CommandContext(context.Background(), "kubectl", "get", "crds")
	output, err := Run(cmd)
	if err != nil {
		return false
	}

	// cert-manager CRDが存在するか確認する。
	crdList := GetNonEmptyLines(output)
	for _, crd := range certManagerCRDs {
		for _, line := range crdList {
			if strings.Contains(line, crd) {
				return true
			}
		}
	}

	return false
}

// LoadImageToKindClusterWithNameはlocal Docker imageをkind clusterへloadする。
func LoadImageToKindClusterWithName(name string) error {
	cluster := defaultKindCluster
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		cluster = v
	}
	kindOptions := []string{"load", "docker-image", name, "--name", cluster}
	kindBinary := defaultKindBinary
	if v, ok := os.LookupEnv("KIND"); ok {
		kindBinary = v
	}
	cmd := exec.CommandContext(context.Background(), kindBinary, kindOptions...)
	_, err := Run(cmd)
	return err
}

// GetNonEmptyLinesはcommand outputの文字列を改行ごとの要素へ分解し、空要素を無視する。
func GetNonEmptyLines(output string) []string {
	var res []string
	elements := strings.SplitSeq(output, "\n")
	for element := range elements {
		if element != "" {
			res = append(res, element)
		}
	}

	return res
}

// GetProjectDirはproject directoryを返す。
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, fmt.Errorf("failed to get current working directory: %w", err)
	}
	wd = strings.ReplaceAll(wd, "/test/e2e", "")
	return wd, nil
}

// UncommentCodeはfileからtargetを検索し、target contentのcomment prefixを除去する。target contentは複数行にまたがってよい。
func UncommentCode(filename, target, prefix string) error {
	// gosecのfalse positive。
	//nolint:gosec
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file %q: %w", filename, err)
	}
	strContent := string(content)

	idx := strings.Index(strContent, target)
	if idx < 0 {
		return fmt.Errorf("unable to find the code %q to be uncommented", target)
	}

	out := new(bytes.Buffer)
	_, err = out.Write(content[:idx])
	if err != nil {
		return fmt.Errorf("failed to write to output: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewBufferString(target))
	if !scanner.Scan() {
		return nil
	}
	for {
		if _, err = out.WriteString(strings.TrimPrefix(scanner.Text(), prefix)); err != nil {
			return fmt.Errorf("failed to write to output: %w", err)
		}
		// targetの直前の行が最後の行である場合に改行を書き込まない。
		if !scanner.Scan() {
			break
		}
		if _, err = out.WriteString("\n"); err != nil {
			return fmt.Errorf("failed to write to output: %w", err)
		}
	}

	if _, err = out.Write(content[idx+len(target):]); err != nil {
		return fmt.Errorf("failed to write to output: %w", err)
	}

	// gosecのfalse positive。
	//nolint:gosec
	if err = os.WriteFile(filename, out.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write file %q: %w", filename, err)
	}

	return nil
}
