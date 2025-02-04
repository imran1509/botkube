package kubectl

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gookit/color"
	"github.com/spf13/pflag"

	"github.com/kubeshop/botkube/pkg/pluginx"
)

const binaryName = "kubectl"

// BinaryRunner runs a kubectl binary.
type BinaryRunner struct {
	executeCommandWithEnvs func(ctx context.Context, rawCmd string, envs map[string]string) (string, error)
}

// NewBinaryRunner returns a new BinaryRunner instance.
func NewBinaryRunner() *BinaryRunner {
	return &BinaryRunner{
		executeCommandWithEnvs: pluginx.ExecuteCommandWithEnvs,
	}
}

// RunKubectlCommand runs a Kubectl CLI command and run output.
func (e *BinaryRunner) RunKubectlCommand(ctx context.Context, defaultNamespace, cmd string) (string, error) {
	if err := detectNotSupportedCommands(cmd); err != nil {
		return "", err
	}
	if err := detectNotSupportedGlobalFlags(cmd); err != nil {
		return "", err
	}

	if strings.EqualFold(cmd, "options") {
		return optionsCommandOutput(), nil
	}

	isNs, err := isNamespaceFlagSet(cmd)
	if err != nil {
		return "", err
	}

	if !isNs {
		// appending the defaultNamespace at the beginning to do not break the command e.g.
		//    kubectl exec mypod -- date
		cmd = fmt.Sprintf("-n %s %s", defaultNamespace, cmd)
	}

	envs := map[string]string{
		// TODO: take it from the execute context.
		"KUBECONFIG": os.Getenv("KUBECONFIG"),
	}

	runCmd := fmt.Sprintf("%s %s", binaryName, cmd)
	out, err := e.executeCommandWithEnvs(ctx, runCmd, envs)
	if err != nil {
		return "", fmt.Errorf("%s\n%s", out, err.Error())
	}

	return color.ClearCode(out), nil
}

// getAllNamespaceFlag returns the namespace value extracted from a given args.
// If `--A, --all-namespaces` or `--namespace/-n` was found, returns true.
func isNamespaceFlagSet(cmd string) (bool, error) {
	f := pflag.NewFlagSet("extract-ns", pflag.ContinueOnError)
	f.BoolP("help", "h", false, "to make sure that parsing is ignoring the --help,-h flags as there are specially process by pflag")

	// ignore unknown flags errors, e.g. `--cluster-name` etc.
	f.ParseErrorsWhitelist.UnknownFlags = true

	var isNs string
	f.StringVarP(&isNs, "namespace", "n", "", "Kubernetes Namespace")

	var isAllNs bool
	f.BoolVarP(&isAllNs, "all-namespaces", "A", false, "Kubernetes All Namespaces")
	if err := f.Parse(strings.Fields(cmd)); err != nil {
		return false, err
	}
	return isAllNs || isNs != "", nil
}
