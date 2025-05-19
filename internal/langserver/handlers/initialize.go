package handlers

import (
	"context"

	lsctx "github.com/Azure/azurerm-lsp/internal/context"
	ilsp "github.com/Azure/azurerm-lsp/internal/lsp"
	lsp "github.com/Azure/azurerm-lsp/internal/protocol"
	"github.com/creachadair/jrpc2"
)

func (svc *service) Initialize(ctx context.Context, params lsp.InitializeParams) (lsp.InitializeResult, error) {
	serverCaps := lsp.InitializeResult{
		Capabilities: lsp.ServerCapabilities{
			TextDocumentSync: lsp.TextDocumentSyncOptions{
				OpenClose: true,
				Change:    lsp.Incremental,
			},
			CompletionProvider: lsp.CompletionOptions{
				ResolveProvider: false,
				TriggerCharacters: []string{
					"",
					` `,
					`.`,
					`/`,
					`@`,
					`{`,
					`"`,
				},
			},
			HoverProvider: true,

			CodeActionProvider: lsp.CodeActionOptions{
				CodeActionKinds: ilsp.SupportedCodeActions.AsSlice(),
				ResolveProvider: false,
			},
			DeclarationProvider:        false,
			DefinitionProvider:         false,
			CodeLensProvider:           nil,
			ReferencesProvider:         false,
			DocumentFormattingProvider: false,
			DocumentSymbolProvider:     false,
			WorkspaceSymbolProvider:    false,
			Workspace:                  nil,

			ExecuteCommandProvider: &lsp.ExecuteCommandOptions{
				Commands: availableCommands(),
				WorkDoneProgressOptions: lsp.WorkDoneProgressOptions{
					WorkDoneProgress: true,
				},
			},
		},
	}

	serverCaps.ServerInfo.Name = "azurerm-lsp"
	version, ok := lsctx.LanguageServerVersion(ctx)
	if ok {
		serverCaps.ServerInfo.Version = version
	}

	clientCaps := params.Capabilities
	expClientCaps := lsp.ExperimentalClientCapabilities(clientCaps.Experimental)

	svc.server = jrpc2.ServerFromContext(ctx)

	err := svc.configureSessionDependencies()
	if err != nil {
		return serverCaps, err
	}

	if tv, ok := expClientCaps.TelemetryVersion(); ok {
		svc.logger.Printf("enabling telemetry (version: %d)", tv)
		err := svc.setupTelemetry(tv, svc.server)
		if err != nil {
			svc.logger.Printf("failed to setup telemetry: %s", err)
		}
		svc.logger.Printf("telemetry enabled (version: %d)", tv)
	}

	if params.ClientInfo.Name != "" {
		err := ilsp.SetClientName(ctx, params.ClientInfo.Name)
		if err != nil {
			return serverCaps, err
		}
	}

	err = ilsp.SetClientCapabilities(ctx, &clientCaps)
	if err != nil {
		return serverCaps, err
	}

	if !clientCaps.Workspace.WorkspaceFolders && len(params.WorkspaceFolders) > 0 {
		_ = jrpc2.ServerFromContext(ctx).Notify(ctx, "window/showMessage", &lsp.ShowMessageParams{
			Type: lsp.Warning,
			Message: "Client sent workspace folders despite not declaring support. " +
				"Please report this as a bug.",
		})
	}

	return serverCaps, nil
}
