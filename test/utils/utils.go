package utils

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint:staticcheck
)

const (
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
