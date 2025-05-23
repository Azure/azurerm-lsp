package handlers

import (
	"context"

	"github.com/Azure/azurerm-lsp/internal/lsp"
	"github.com/Azure/azurerm-lsp/internal/parser"
	"github.com/Azure/azurerm-lsp/internal/protocol"
	provider_schema "github.com/Azure/azurerm-lsp/provider-schema"
	"github.com/hashicorp/hcl/v2"
)

func (svc *service) HandleHover(ctx context.Context, params protocol.TextDocumentPositionParams) (*protocol.Hover, error) {
	docContent, docFileName, err := parser.GetDocumentContent(ctx, params.TextDocument.URI)
	if err != nil {
		return nil, err
	}

	ctxInfo, diags, err := parser.BuildHCLContext(docContent, docFileName, params.Position)
	if err != nil || (diags != nil && diags.HasErrors()) {
		docContent, fieldName, _, err := parser.AttemptReparse(docContent, params.Position.Line)
		if err != nil {
			return nil, nil
		}

		ctxInfo, diags, err = parser.BuildHCLContext(docContent, docFileName, params.Position)
		if err != nil || (diags != nil && diags.HasErrors()) {
			return nil, nil
		}

		if ctxInfo.ParsedPath != "" {
			ctxInfo.ParsedPath += "." + fieldName
		} else {
			ctxInfo.ParsedPath = fieldName
		}
	}

	var content string

	switch {
	case ctxInfo.ParsedPath != "" && (ctxInfo.SubBlock != nil || ctxInfo.Attribute != nil):
		content, _, err = provider_schema.GetAttributeContent(ctxInfo.Resource, ctxInfo.ParsedPath)
	default:
		content, _, err = provider_schema.GetResourceContent(ctxInfo.Resource)
	}
	if err != nil {
		return nil, nil
	}

	var keyRange hcl.Range
	if ctxInfo.Attribute != nil {
		keyRange = ctxInfo.Attribute.NameRange
	} else if ctxInfo.SubBlock != nil {
		if len(ctxInfo.SubBlock.LabelRanges) == 0 {
			keyRange = ctxInfo.SubBlock.TypeRange
		} else {
			keyRange = ctxInfo.SubBlock.LabelRanges[0]
		}
	} else {
		if len(ctxInfo.Block.LabelRanges) == 0 {
			keyRange = ctxInfo.Block.TypeRange
		} else {
			keyRange = ctxInfo.Block.LabelRanges[0]
		}
	}

	pos := lsp.LSPPosToHCL(params.Position)
	// Only show hover if position is within the key range
	if !keyRange.ContainsPos(pos) ||
		(pos.Line == keyRange.Start.Line && pos.Column < keyRange.Start.Column) ||
		(pos.Line == keyRange.End.Line && pos.Column > keyRange.End.Column) {
		return nil, nil // Not on key, do not show hover
	}

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: content,
		},
		Range: lsp.HCLRangeToLSP(keyRange),
	}, nil
}
