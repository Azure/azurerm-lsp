package lsp

import (
	"bytes"

	lsp "github.com/Azure/ms-terraform-lsp/internal/protocol"
	"github.com/Azure/ms-terraform-lsp/internal/source"
	"github.com/hashicorp/hcl-lang/lang"
)

type TokenEncoder struct {
	Lines      source.Lines
	Tokens     []lang.SemanticToken
	ClientCaps lsp.SemanticTokensClientCapabilities
}

func (te *TokenEncoder) Encode() []uint32 {
	data := make([]uint32, 0)

	for i := range te.Tokens {
		data = append(data, te.encodeTokenOfIndex(i)...)
	}

	return data
}

func (te *TokenEncoder) encodeTokenOfIndex(i int) []uint32 {
	token := te.Tokens[i]

	var tokenType TokenType
	modifiers := make([]TokenModifier, 0)

	switch token.Type {
	case lang.TokenBlockType:
		tokenType = TokenTypeType
	case lang.TokenBlockLabel:
		tokenType = TokenTypeString
	case lang.TokenAttrName:
		tokenType = TokenTypeProperty
	case lang.TokenBool:
		tokenType = TokenTypeKeyword
	case lang.TokenNumber:
		tokenType = TokenTypeNumber
	case lang.TokenString:
		tokenType = TokenTypeString
	case lang.TokenObjectKey:
		tokenType = TokenTypeParameter
	case lang.TokenMapKey:
		tokenType = TokenTypeParameter
	case lang.TokenKeyword:
		tokenType = TokenTypeVariable
	case lang.TokenTraversalStep:
		tokenType = TokenTypeVariable

	default:
		return []uint32{}
	}

	if !te.tokenTypeSupported(tokenType) {
		return []uint32{}
	}

	tokenTypeIdx := TokenTypesLegend(te.ClientCaps.TokenTypes).Index(tokenType)

	for _, m := range token.Modifiers {
		switch m {
		case lang.TokenModifierDependent:
			if !te.tokenModifierSupported(TokenModifierModification) {
				continue
			}
			modifiers = append(modifiers, TokenModifierModification)
		case lang.TokenModifierDeprecated:
			if !te.tokenModifierSupported(TokenModifierDeprecated) {
				continue
			}
			modifiers = append(modifiers, TokenModifierDeprecated)
		}
	}

	modifierBitMask := TokenModifiersLegend(te.ClientCaps.TokenModifiers).BitMask(modifiers)

	data := make([]uint32, 0)

	// Client may not support multiline tokens which would be indicated
	// via lsp.SemanticTokensCapabilities.MultilineTokenSupport
	// once it becomes available in gopls LSP structs.
	//
	// For now we just safely assume client does *not* support it.

	tokenLineDelta := token.Range.End.Line - token.Range.Start.Line

	previousLine := 0
	previousStartChar := 0
	if i > 0 {
		previousLine = te.Tokens[i-1].Range.End.Line - 1
		currentLine := te.Tokens[i].Range.End.Line - 1
		if currentLine == previousLine {
			previousStartChar = te.Tokens[i-1].Range.Start.Column - 1
		}
	}

	if tokenLineDelta == 0 || false /* te.clientCaps.MultilineTokenSupport */ {
		deltaLine := token.Range.Start.Line - 1 - previousLine
		tokenLength := token.Range.End.Byte - token.Range.Start.Byte
		deltaStartChar := token.Range.Start.Column - 1 - previousStartChar

		data = append(data, []uint32{
			// #nosec G115
			uint32(deltaLine),
			// #nosec G115
			uint32(deltaStartChar),
			// #nosec G115
			uint32(tokenLength),
			// #nosec G115
			uint32(tokenTypeIdx),
			// #nosec G115
			uint32(modifierBitMask),
		}...)
	} else {
		// Add entry for each line of a multiline token
		for tokenLine := token.Range.Start.Line - 1; tokenLine <= token.Range.End.Line-1; tokenLine++ {
			deltaLine := tokenLine - previousLine

			deltaStartChar := 0
			if tokenLine == token.Range.Start.Line-1 {
				deltaStartChar = token.Range.Start.Column - 1 - previousStartChar
			}

			lineBytes := bytes.TrimRight(te.Lines[tokenLine].Bytes(), "\n\r")
			length := len(lineBytes)

			if tokenLine == token.Range.End.Line-1 {
				length = token.Range.End.Column - 1
			}

			data = append(data, []uint32{
				// #nosec G115
				uint32(deltaLine),
				// #nosec G115
				uint32(deltaStartChar),
				// #nosec G115
				uint32(length),
				// #nosec G115
				uint32(tokenTypeIdx),
				// #nosec G115
				uint32(modifierBitMask),
			}...)

			previousLine = tokenLine
		}
	}

	return data
}

func (te *TokenEncoder) tokenTypeSupported(tokenType TokenType) bool {
	return sliceContains(te.ClientCaps.TokenTypes, string(tokenType))
}

func (te *TokenEncoder) tokenModifierSupported(tokenModifier TokenModifier) bool {
	return sliceContains(te.ClientCaps.TokenModifiers, string(tokenModifier))
}
