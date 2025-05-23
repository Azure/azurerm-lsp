package processors

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const outputFileName = "combined_output.json"

//go:embed combined_output.json
var combinedOutputJSON []byte

// ProcessOutput combines schema and markdown outputs and saves the result to a file
func ProcessOutput(providerPath, gitBranch, outputDir string) (TerraformObjects, error) {
	// Step 1: Generate schema
	schemaOutput, err := ProcessSchema(providerPath, gitBranch)
	if err != nil {
		return nil, fmt.Errorf("error processing schema: %v", err)
	}

	// Step 2: Generate markdown docs
	markdownOutput, err := ProcessMarkdown(providerPath)
	if err != nil {
		return nil, fmt.Errorf("error processing markdown: %v", err)
	}

	// Step 3: Combine schema and markdown outputs
	combinedOutput, err := CombineSchemaAndMarkdown(schemaOutput, markdownOutput)
	if err != nil {
		return nil, fmt.Errorf("error combining outputs: %v", err)
	}

	// Step 4: Marshal combined output to JSON
	jsonOutput, err := json.MarshalIndent(combinedOutput, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("error marshaling combined output: %v", err)
	}

	// Step 5: Write combined output to a file
	err = os.WriteFile(
		filepath.Join(outputDir, outputFileName),
		jsonOutput,
		0600, // More restrictive permissions
	)
	if err != nil {
		return nil, fmt.Errorf("error writing combined output to file: %v", err)
	}

	fmt.Printf("Combined output successfully written to %s\n", outputFileName)
	return combinedOutput, nil
}

func LoadProcessedOutput() (TerraformObjects, error) {
	var terraformObjects TerraformObjects

	// Unmarshal the combined output JSON into the terraformObjects variable
	err := json.Unmarshal(combinedOutputJSON, &terraformObjects)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling combined output JSON: %v", err)
	}

	return terraformObjects, nil
}
