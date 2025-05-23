package handlers

import (
	"context"
	"github.com/Azure/azurerm-lsp/internal/parser"
	"github.com/Azure/azurerm-lsp/internal/protocol"
	"github.com/Azure/azurerm-lsp/internal/utils"
	"github.com/Azure/azurerm-lsp/provider-schema"
	"github.com/Azure/azurerm-lsp/provider-schema/azurerm/schema"
)

func (svc *service) HandleComplete(ctx context.Context, params protocol.CompletionParams) ([]protocol.CompletionItem, error) {
	docContent, docFileName, err := parser.GetDocumentContent(ctx, params.TextDocument.URI)
	if err != nil {
		return nil, err
	}

	ctxInfo, diags, err := parser.BuildHCLContext(docContent, docFileName, params.Position)
	if err != nil || (diags != nil && diags.HasErrors()) {
		docContent, fieldName, isNewBlock, err := parser.AttemptReparse(docContent, params.Position.Line)
		if err != nil {
			if isNewBlock {
				return GetTopLevelCompletions(params), nil
			}

			return nil, nil
		}

		ctxInfo, diags, err = parser.BuildHCLContext(docContent, docFileName, params.Position)
		if err != nil || (diags != nil && diags.HasErrors()) {
			if utils.MatchAnyPrefix(fieldName, schema.AzureRMPrefix, schema.ResourcesPrefix) {
				return GetTopLevelCompletions(params), nil
			}

			return nil, nil
		}

		if ctxInfo.Block != nil || ctxInfo.SubBlock != nil || ctxInfo.Attribute != nil {
			if ctxInfo.ParsedPath != "" {
				fieldName = ctxInfo.ParsedPath + "." + fieldName
			}

			return GetAttributeCompletions(ctxInfo.Resource, fieldName), nil
		}

		return nil, nil
	}

	switch {
	case ctxInfo.Attribute != nil:
		return GetAttributeCompletions(ctxInfo.Resource, ctxInfo.ParsedPath), nil
	case ctxInfo.SubBlock != nil || ctxInfo.Block != nil:
		return GetBlockAttributeCompletions(ctxInfo.Resource, ctxInfo.ParsedPath), nil
	default:
		return GetTopLevelCompletions(params), nil
	}
}

func GetTopLevelCompletions(params protocol.CompletionParams) []protocol.CompletionItem {
	resources := provider_schema.ListAllResources()
	dataSources := provider_schema.ListAllDataSources()
	lineRange := getLineRange(params)

	var items []protocol.CompletionItem
	for _, name := range append(resources, dataSources...) {
		snippet, err := provider_schema.GetSnippet(name)
		if err != nil {
			continue
		}

		content, isDataSource, err := provider_schema.GetResourceContent(name)
		if err != nil {
			continue
		}

		kind := "resource"
		if isDataSource {
			kind = "data source"
		}

		items = append(items, protocol.CompletionItem{
			Label:            name,
			InsertText:       snippet,
			InsertTextFormat: protocol.SnippetTextFormat,
			Kind:             protocol.SnippetCompletion,
			Detail:           "AzureRM " + kind,
			TextEdit: &protocol.TextEdit{
				Range:   lineRange,
				NewText: snippet,
			},
			Documentation: protocol.MarkupContent{
				Kind:  protocol.Markdown,
				Value: content,
			},
		})
	}
	return items
}

func GetBlockAttributeCompletions(resourceName, path string) []protocol.CompletionItem {
	props, err := provider_schema.ListDirectProperties(resourceName, path)
	if err != nil {
		return nil
	}

	var items []protocol.CompletionItem
	for _, p := range props {
		content, prop, err := provider_schema.GetAttributeContent(resourceName, p.AttributePath)
		if err != nil || prop == nil {
			continue
		}

		items = append(items, protocol.CompletionItem{
			Label:      p.Name,
			Kind:       protocol.PropertyCompletion,
			SortText:   p.GetSortOrder(),
			InsertText: p.Name,
			Documentation: protocol.MarkupContent{
				Kind:  protocol.Markdown,
				Value: content,
			},
		})
	}
	return items
}

func GetAttributeCompletions(resourceName, path string) []protocol.CompletionItem {
	values, err := provider_schema.GetPossibleValuesForProperty(resourceName, path)
	if err != nil {
		return nil
	}
	content, _, err := provider_schema.GetAttributeContent(resourceName, path)
	if err != nil {
		return nil
	}

	items := make([]protocol.CompletionItem, 0, len(values))
	for _, val := range values {
		items = append(items, protocol.CompletionItem{
			Label:  val,
			Kind:   protocol.ValueCompletion,
			Detail: "Possible value for " + path,
			Documentation: protocol.MarkupContent{
				Kind:  protocol.Markdown,
				Value: content,
			},
		})
	}

	return items
}

func getLineRange(params protocol.CompletionParams) protocol.Range {
	start := protocol.Position{Line: params.Position.Line, Character: 0}
	end := params.Position
	return protocol.Range{Start: start, End: end}
}
