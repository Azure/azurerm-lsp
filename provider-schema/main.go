package provider_schema

import (
	"fmt"
	"github.com/Azure/azurerm-lsp/provider-schema/processors"
	"os"
	"os/exec"
	"path/filepath"
)

func GenerateProviderSchema(providerPath, gitBranch string) {
	dirPath := os.Getenv("PWD") + "/provider-schema"

	cmd := exec.Command(
		"bash",
		"-c",
		filepath.Join(dirPath, "processors/.tools/generate-provider-schema/run.sh"),
		providerPath,
		gitBranch,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		fmt.Printf("Error running script: %v\n", err)
		return
	}

	_, err = processors.ProcessOutput(providerPath, gitBranch, dirPath+"/processors")
	if err != nil {
		fmt.Printf("Error processing output: %v\n", err)
		return
	}

	_, err = processors.LoadProcessedOutput()
	if err != nil {
		fmt.Printf("Error loading processed output: %v\n", err)
		return
	}
}
