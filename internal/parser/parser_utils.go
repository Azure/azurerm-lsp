package parser

import (
	"fmt"
	"strings"

	"github.com/Azure/azurerm-lsp/internal/lsp"
	"github.com/Azure/azurerm-lsp/internal/protocol"
	"github.com/Azure/azurerm-lsp/provider-schema/azurerm/schema"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type HCLContext struct {
	File       *hcl.File
	Body       *hclsyntax.Body
	Block      *hclsyntax.Block
	SubBlock   *hclsyntax.Block
	Attribute  *hclsyntax.Attribute
	Resource   string
	ParsedPath string
}

func BuildHCLContext(docContent, fileName string, position protocol.Position) (*HCLContext, hcl.Diagnostics, error) {
	file, diags := hclsyntax.ParseConfig([]byte(docContent), fileName, hcl.InitialPos)
	if diags != nil && diags.HasErrors() {
		return nil, diags, fmt.Errorf("failed to parse HCL: %v", diags)
	}

	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, nil, fmt.Errorf("file is not valid HCL body")
	}

	hclPos := lsp.LSPPosToHCL(position)
	block := BlockAtPos(body, hclPos)
	if block == nil || len(block.Labels) == 0 {
		return nil, nil, fmt.Errorf("no valid block found")
	}

	resource := block.Labels[0]
	if !strings.HasPrefix(resource, schema.AzureRMPrefix) {
		return nil, nil, fmt.Errorf("not an AzureRM resource")
	}

	subBlock := BlockAtPos(block.Body, hclPos)
	attr := AttributeAtPos(block, hclPos)
	if attr == nil && subBlock != nil {
		attr = AttributeAtPos(subBlock, hclPos)
	}

	var path string
	if attr != nil {
		path = buildNestedPath(block, attr)
	} else if subBlock != nil {
		path = buildNestedPath(block, subBlock)
	}

	return &HCLContext{
		File:       file,
		Body:       body,
		Block:      block,
		SubBlock:   subBlock,
		Attribute:  attr,
		Resource:   resource,
		ParsedPath: path,
	}, nil, nil
}

func buildNestedPath(topLevelBlock *hclsyntax.Block, targetNode hclsyntax.Node) string {
	var pathParts []string

	var traverse func(block *hclsyntax.Block, target hclsyntax.Node) bool
	traverse = func(block *hclsyntax.Block, target hclsyntax.Node) bool {
		for _, attr := range block.Body.Attributes {
			if attr == target {
				pathParts = append(pathParts, attr.Name)
				return true
			}
		}
		for _, nested := range block.Body.Blocks {
			if nested == target {
				pathParts = append(pathParts, nested.Type)
				return true
			}
			if traverse(nested, target) {
				pathParts = append([]string{nested.Type}, pathParts...)
				return true
			}
		}
		return false
	}

	if traverse(topLevelBlock, targetNode) {
		return strings.Join(pathParts, ".")
	}
	return ""
}

func AttemptReparse(content string, lineNum uint32) (updatedContent, fieldName string, isNewBlock bool, err error) {
	lineContents := strings.Split(content, "\n")

	if lineNum < 0 || int(lineNum) >= len(lineContents) {
		return "", "", false, fmt.Errorf("invalid line number")
	}

	lineContent := strings.TrimSpace(lineContents[lineNum])
	if lineContent == "" || lineContent == schema.AzureRMPrefix {
		return "", "", true, fmt.Errorf("empty line content")
	}

	fieldParts := strings.Split(lineContent, "=")
	if len(fieldParts) == 0 {
		return "", "", true, fmt.Errorf("invalid line content")
	}

	updatedContent = strings.Join(lineContents[:lineNum], "\n") + "\n\n" + strings.Join(lineContents[lineNum+1:], "\n")
	fieldName = strings.TrimSpace(fieldParts[0])
	if fieldName == "" || fieldName == schema.AzureRMPrefix {
		return "", "", true, fmt.Errorf("invalid field name")
	}

	return updatedContent, fieldName, false, nil
}
