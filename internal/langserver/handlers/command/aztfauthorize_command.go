package command

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Azure/aztfmigrate/tf"
	ictx "github.com/Azure/azurerm-lsp/internal/context"
	ilsp "github.com/Azure/azurerm-lsp/internal/lsp"
	lsp "github.com/Azure/azurerm-lsp/internal/protocol"
	"github.com/hashicorp/go-uuid"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
)

//go:generate sh ./data/refresh.sh

type AztfAuthorizeCommand struct{}

var _ CommandHandler = &AztfAuthorizeCommand{}

const (
	tempAuthorizeFolderNamePrefix = "aztfauthorize_temp"
	configFileName                = "read_permission.tf"
)

type permission struct {
	Actions        []string
	NotActions     []string
	DataActions    []string
	NotDataActions []string
}

func (c AztfAuthorizeCommand) Handle(ctx context.Context, arguments []json.RawMessage) (interface{}, error) {
	var params lsp.CodeActionParams
	generateForMissing := false
	if len(arguments) != 0 {
		err := json.Unmarshal(arguments[0], &params)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
		}

		var generateSetting map[string]interface{}
		err = json.Unmarshal(arguments[1], &generateSetting)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
		}

		generateForMissing = generateSetting["generateForMissingPermission"].(bool)
	}

	telemetrySender, err := ictx.Telemetry(ctx)
	if err != nil {
		return nil, err
	}

	clientCaller, err := ictx.ClientCaller(ctx)
	if err != nil {
		return nil, err
	}

	clientNotifier, err := ictx.ClientNotifier(ctx)
	if err != nil {
		log.Printf("[ERROR] failed to get client notifier: %+v", err)
		return nil, err
	}

	telemetrySender.SendEvent(ctx, "aztfauthorize", map[string]interface{}{
		"status": "started",
	})
	reportAuthorizeCommandProgress(ctx, "Parsing Terraform configurations...", 0)
	defer reportAuthorizeCommandProgress(ctx, "Role generation completed.", 100)

	fs, err := ictx.DocumentStorage(ctx)
	if err != nil {
		return nil, err
	}

	doc, err := fs.GetDocument(ilsp.FileHandlerFromDocumentURI(params.TextDocument.URI))
	if err != nil {
		return nil, err
	}

	startDocPos := lsp.TextDocumentPositionParams{
		TextDocument: params.TextDocument,
		Position:     params.Range.Start,
	}
	startPos, err := ilsp.FilePositionFromDocumentPosition(startDocPos, doc)
	if err != nil {
		return nil, err
	}

	endDocPos := lsp.TextDocumentPositionParams{
		TextDocument: params.TextDocument,
		Position:     params.Range.End,
	}
	endPos, err := ilsp.FilePositionFromDocumentPosition(endDocPos, doc)
	if err != nil {
		return nil, err
	}

	data, err := doc.Text()
	if err != nil {
		return nil, err
	}

	// parsing the document
	syntaxDoc, diags := hclsyntax.ParseConfig(data, "", hcl.InitialPos)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing the HCL file: %s", diags.Error())
	}

	body, ok := syntaxDoc.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("failed to parse HCL syntax")
	}

	actions := make(map[string]struct{}, 0)
	apiVersionRe := regexp.MustCompile(`^\d{4}\-\d{2}\-\d{2}$`)
	workingDirectory := getWorkingDirectory(string(params.TextDocument.URI), runtime.GOOS)
	mapping, err := getAzurermMapping(ctx, workingDirectory)
	if err != nil {
		return nil, fmt.Errorf("error get azurerm mapping: %+v", err)
	}

	for _, block := range body.Blocks {
		if startPos.Position().Byte <= block.Range().Start.Byte && block.Range().End.Byte <= endPos.Position().Byte {
			address := strings.Join(block.Labels, ".")
			if strings.HasPrefix(address, "azurerm_") {
				v, ok := mapping[fmt.Sprintf("%v.%v", block.Type, block.Labels[0])]
				if !ok {
					log.Printf("[DEBUG] %q not found in mapping", fmt.Sprintf("%v.%v", block.Type, block.Labels[0]))
					continue
				}

				// azurerm resource function
				for _, f := range []string{"create", "read", "update", "delete"} {
					fv, ok := v[f]
					if !ok {
						continue
					}

					// for each azurerm resource function, aztfo report has a list of Azure API operations
					for _, op := range fv.([]interface{}) {
						opv := op.(map[string]interface{})
						if !apiVersionRe.MatchString(opv["version"].(string)) {
							// skip for data plane resource
							continue
						}

						path := opv["path"].(string)
						kind := opv["kind"].(string)
						action := strings.ReplaceAll(path, "/{}", "")
						action, _ = strings.CutSuffix(action, "/DEFAULT")

						// in case there might be more than one provider
						if strings.Contains(action, "PROVIDERS") {
							action = action[strings.LastIndex(action, "/PROVIDERS")+11:]
						}
						if strings.HasPrefix(action, "/SUBSCRIPTIONS") {
							action = "MICROSOFT.RESOURCES" + action
						}

						switch kind {
						case "PUT", "PATCH":
							action += "/write"
						case "GET":
							action += "/read"
						case "DELETE":
							action += "/delete"
						case "POST":
							action += "/action"
						}

						actions[action] = struct{}{}
					}
				}
			}
		}
	}

	reportAuthorizeCommandProgress(ctx, fmt.Sprintf("actions: %+v", actions), 30)

	permissions, err := matchPermissions(actions)
	if err != nil {
		return nil, fmt.Errorf("error get required permissions: %+v", err)
	}

	timeNow := fmt.Sprintf("customRoleGenerated_%v.tf", time.Now().UTC().Format("060102T150405"))

	// creating temp workspace
	tempDir := filepath.Join(workingDirectory, fmt.Sprintf("%v_%v", tempAuthorizeFolderNamePrefix, timeNow))
	if err := os.MkdirAll(tempDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create temp workspace %q, please check the permission: %w", tempDir, err)
	}
	defer func() {
		err := os.RemoveAll(tempDir)
		if err != nil {
			log.Printf("[ERROR] removing temp workspace %q: %+v", tempDir, err)
		}
	}()

	reportAuthorizeCommandProgress(ctx, fmt.Sprintf("permissions: %+v", permissions), 60)

	if generateForMissing {
		existingPerm, err := getExistingPermission(ctx, params, tempDir)
		if err != nil {
			return nil, fmt.Errorf("error get existing permissions: %+v", err)
		}

		*permissions = filterPermission(*permissions, *existingPerm)

		if len(permissions.Actions) == 0 && len(permissions.DataActions) == 0 {
			_ = clientNotifier.Notify(ctx, "window/showMessage", lsp.ShowMessageParams{
				Type:    lsp.Warning,
				Message: "No missing permission",
			})

			telemetrySender.SendEvent(ctx, "aztfauthorize", map[string]interface{}{
				"status": "completed",
			})

			return nil, nil
		}

		reportAuthorizeCommandProgress(ctx, fmt.Sprintf("missing permissions: %+v", permissions), 80)
	}

	roleConfig := generateRoleConfig(*permissions)

	_, _ = clientCaller.Callback(ctx, "workspace/applyEdit", lsp.ApplyWorkspaceEditParams{
		Label: "Update config",
		Edit: lsp.WorkspaceEdit{
			Changes: map[string][]lsp.TextEdit{
				("untitled:" + timeNow): {
					{
						Range: lsp.Range{
							Start: lsp.Position{Line: 0, Character: 0},
							End:   lsp.Position{Line: 0, Character: 0},
						},
						NewText: string(roleConfig),
					},
				},
			},
		},
	})

	_, _ = clientCaller.Callback(ctx, "window/showDocument", lsp.ShowDocumentParams{
		URI:       "untitled:" + timeNow,
		External:  false,
		TakeFocus: true,
	})

	telemetrySender.SendEvent(ctx, "aztfauthorize", map[string]interface{}{
		"status": "completed",
	})

	return nil, nil
}

func reportAuthorizeCommandProgress(ctx context.Context, message string, percentage uint32) {
	clientCaller, err := ictx.ClientCaller(ctx)
	if err != nil {
		log.Printf("[ERROR] failed to get client caller: %+v", err)
		return
	}

	clientNotifier, err := ictx.ClientNotifier(ctx)
	if err != nil {
		log.Printf("[ERROR] failed to get client notifier: %+v", err)
		return
	}

	switch percentage {
	case 0:
		_, _ = clientCaller.Callback(ctx, "window/workDoneProgress/create", lsp.WorkDoneProgressCreateParams{
			Token: "aztfauthorize",
		})
		_ = clientNotifier.Notify(ctx, "$/progress", map[string]interface{}{
			"token": "aztfauthorize",
			"value": lsp.WorkDoneProgressBegin{
				Kind:        "begin",
				Title:       "Azure providers role generation",
				Cancellable: false,
				Message:     message,
				Percentage:  0,
			},
		})
	case 100:
		_ = clientNotifier.Notify(ctx, "$/progress", map[string]interface{}{
			"token": "aztfauthorize",
			"value": lsp.WorkDoneProgressEnd{
				Kind:    "end",
				Message: message,
			},
		})
	default:
		_ = clientNotifier.Notify(ctx, "$/progress", map[string]interface{}{
			"token": "aztfauthorize",
			"value": lsp.WorkDoneProgressReport{
				Kind:        "report",
				Cancellable: false,
				Message:     message,
				Percentage:  percentage,
			},
		})
	}
}

func getExistingPermission(ctx context.Context, params lsp.CodeActionParams, tempDir string) (*[]permission, error) {
	dataConfig := `
terraform {
  required_providers {
    azapi = {
      source = "Azure/azapi"
    }
  }
}

provider "azapi" {
}

data "azapi_client_config" "current" {
}

data "azapi_resource_list" "permissions" {
  parent_id = "/subscriptions/${data.azapi_client_config.current.subscription_id}"
  type      = "Microsoft.Authorization/permissions@2022-04-01"
}

output "permissions" {
  value = data.azapi_resource_list.permissions.output.value
}
`

	if err := os.WriteFile(filepath.Join(tempDir, configFileName), []byte(dataConfig), 0600); err != nil {
		return nil, err
	}

	terraform, err := tf.NewTerraform(tempDir, false)
	if err != nil {
		return nil, err
	}

	if err := terraform.GetExec().Init(ctx); err != nil {
		return nil, err
	}

	if err := terraform.GetExec().Apply(ctx); err != nil {
		return nil, err
	}

	var permissions []permission
	if value, err := terraform.GetExec().Output(ctx); err != nil {
		return nil, err
	} else {
		jsonBytes, _ := value["permissions"].Value.MarshalJSON()
		if err := json.Unmarshal(jsonBytes, &permissions); err != nil {
			return nil, err
		}

	}

	return &permissions, nil
}

func filterPermission(required permission, existing []permission) permission {
	if len(existing) == 0 {
		return required
	}

	needed := permission{
		Actions:     make([]string, 0),
		DataActions: make([]string, 0),
	}

	type permissionRegexp struct {
		Actions        []*regexp.Regexp
		NotActions     []*regexp.Regexp
		DataActions    []*regexp.Regexp
		NotDataActions []*regexp.Regexp
	}
	existingPermissionRegexp := make([]permissionRegexp, 0)

	for _, e := range existing {
		existingPermissionRegexp = append(existingPermissionRegexp, permissionRegexp{
			Actions:        transformStringRegexp(e.Actions),
			NotActions:     transformStringRegexp(e.NotActions),
			DataActions:    transformStringRegexp(e.DataActions),
			NotDataActions: transformStringRegexp(e.NotDataActions),
		})
	}

	for _, action := range required.Actions {
		duplicated := false
		for _, e := range existingPermissionRegexp {
			// notActions is handled before actions
			notActionMatch := false
			for _, v := range e.NotActions {
				if v.MatchString(action) {
					notActionMatch = true
					break
				}
			}

			// if the notActions match, skip this one and match the next permission
			if notActionMatch {
				continue
			}

			actionMatch := false
			for _, v := range e.Actions {
				if v.MatchString(action) {
					actionMatch = true
					break
				}
			}

			if actionMatch {
				duplicated = true
				break
			}
		}

		if !duplicated {
			needed.Actions = append(needed.Actions, action)
		}
	}

	for _, action := range required.DataActions {
		duplicated := false
		for _, e := range existingPermissionRegexp {
			notActionMatch := false
			for _, v := range e.NotDataActions {
				if v.MatchString(action) {
					notActionMatch = true
					break
				}
			}

			if notActionMatch {
				continue
			}

			actionMatch := false
			for _, v := range e.DataActions {
				if v.MatchString(action) {
					actionMatch = true
					break
				}
			}

			if actionMatch {
				duplicated = true
				break
			}
		}

		if !duplicated {
			needed.DataActions = append(needed.DataActions, action)
		}
	}

	return needed
}

func transformStringRegexp(input []string) []*regexp.Regexp {
	output := make([]*regexp.Regexp, 0)
	for _, v := range input {
		output = append(output, regexp.MustCompile(fmt.Sprintf("^%s$", strings.ReplaceAll(v, "*", ".+"))))
	}

	return output
}

//go:embed data/provider_operations.json
var providerOperationsJsonBytes []byte

func matchPermissions(targetActions map[string]struct{}) (*permission, error) {
	azProviderOperationCache := make(map[string]map[string]interface{}, 0)

	var jsonValue []map[string]interface{}
	if err := json.Unmarshal(providerOperationsJsonBytes, &jsonValue); err != nil {
		return nil, err
	}

	for _, value := range jsonValue {
		rpName := value["name"].(string)
		azProviderOperationCache[normalizeName(rpName)] = value
	}

	actions := make(map[string]struct{}, 0)
	dataActions := make(map[string]struct{}, 0)

	for aa := range targetActions {
		rp := aa[:strings.Index(aa, "/")]
		values, ok := azProviderOperationCache[normalizeName(rp)]
		if !ok {
			continue
		}

		for _, opRaw := range values["operations"].([]interface{}) {
			op := opRaw.(map[string]interface{})
			opName := op["name"].(string)
			if !strings.EqualFold(opName, aa) {
				continue
			}

			if op["isDataAction"].(bool) {
				dataActions[opName] = struct{}{}
			} else {
				actions[opName] = struct{}{}
			}
		}

		for _, v := range values["resourceTypes"].([]interface{}) {
			for _, opRaw := range v.(map[string]interface{})["operations"].([]interface{}) {
				op := opRaw.(map[string]interface{})
				opName := op["name"].(string)
				if !strings.EqualFold(opName, aa) {
					continue
				}

				if op["isDataAction"].(bool) {
					dataActions[opName] = struct{}{}
				} else {
					actions[opName] = struct{}{}
				}
			}
		}
	}

	return &permission{
		Actions:     mapToSlice(actions),
		DataActions: mapToSlice(dataActions),
	}, nil
}

func mapToSlice(input map[string]struct{}) []string {
	result := make([]string, 0)
	for v := range input {
		result = append(result, v)
	}
	sort.Strings(result)

	return result
}

func generateRoleConfig(perm permission) []byte {
	uuidGen, _ := uuid.GenerateUUID()
	result := hclwrite.Format(fmt.Appendf([]byte{}, `variable "subscription_id" {
  type        = string
  description = "The UUID of the Azure Subscription where the Role Definition will be created"
}

variable "assign_scope_id" {
  type        = string
  description = "The resource ID of the scope where the Role Definition will be assigned"
}

variable "principal_id" {
  type        = string
  description = "The ID of the Principal (User, Group or Service Principal) to assign the Role Definition to"
}

provider "azurerm" {
  features {}
}

resource "azurerm_role_definition" "role%[1]s" {
  name  = "CustomRole_%[1]s"
  scope = "/subscriptions/${var.subscription_id}"

  permissions {
    actions          = [%[2]s]
    data_actions     = [%[3]s]
    not_actions      = []
    not_data_actions = []
  }

  assignable_scopes = [
    "/subscriptions/${var.subscription_id}"
  ]
}

resource "azurerm_role_assignment" "assignment%[1]s" {
  scope              = var.assign_scope_id
  role_definition_id = azurerm_role_definition.role%[1]s.role_definition_resource_id
  principal_id       = var.principal_id
}
`, uuidGen, printStringSlice(perm.Actions), printStringSlice(perm.DataActions)))

	return result
}

func printStringSlice(input []string) string {
	if len(input) == 0 {
		return ""
	}

	return "\n\"" + strings.Join(input, "\",\n\"") + "\"\n"
}

func normalizeName(input string) string {
	return strings.ToUpper(input)
}

//go:embed data/aztfo_report.json
var localReportBytes []byte

var mapping map[string]map[string]interface{}

func getAzurermMapping(ctx context.Context, dir string) (map[string]map[string]interface{}, error) {
	if mapping != nil {
		return mapping, nil
	}

	mapping = make(map[string]map[string]interface{}, 0)
	var jsonValue []map[string]interface{}
	reportBytes := getAztfoReport(ctx, dir)
	if err := json.Unmarshal(reportBytes, &jsonValue); err != nil {
		return nil, err
	}

	for _, v := range jsonValue {
		id := v["id"].(map[string]interface{})
		blockType := "resource"
		if id["is_data_source"].(bool) {
			blockType = "data"
		}
		resourceType := fmt.Sprintf("%v.%v", blockType, id["name"].(string))
		mapping[resourceType] = v
	}
	return mapping, nil
}

// getAztfoReport get online magodo/aztfo report based on azurerm provider version get from `terraform version` command
// if the process fails, it will use the local report
func getAztfoReport(ctx context.Context, dir string) []byte {
	terraform, err := tf.NewTerraform(dir, false)
	if err != nil {
		log.Printf("[ERROR] failed to run terraform to retrieve installed azurerm provider version: %+v", err)
		return localReportBytes
	}

	var azurermVersion string
	_, providerVersion, err := terraform.GetExec().Version(ctx, false)
	if err != nil {
		log.Printf("[ERROR] failed to retrieve installed azurerm provider version: %+v", err)
		return localReportBytes
	}

	if v, ok := providerVersion["registry.terraform.io/hashicorp/azurerm"]; ok && v != nil {
		azurermVersion = fmt.Sprintf("v%v", v.String())
		log.Printf("[INFO] find local azurerm version: %v", azurermVersion)
	}

	// try to find the latest azurerm version
	if azurermVersion == "" {
		// the url will be routed to the latest version
		response, err := http.Get("https://github.com/hashicorp/terraform-provider-azurerm/releases/latest")
		if err != nil {
			log.Printf("[ERROR] failed to get latest provider version from github: %+v", err)
			return localReportBytes
		}

		// for example: https://github.com/hashicorp/terraform-provider-azurerm/releases/tag/v4.26.0
		latestUrl := response.Request.URL.String()
		azurermVersion = latestUrl[strings.LastIndex(latestUrl, "/")+1:]
		log.Printf("[DEBUG] get latest provider version from github: %v", azurermVersion)
	}

	if azurermVersion != "" {
		// https://raw.githubusercontent.com/wiki/magodo/aztfo/reports/v4.26.0.json
		remoteReportUrl := fmt.Sprintf("https://raw.githubusercontent.com/wiki/magodo/aztfo/reports/%s.json", azurermVersion)
		response, err := http.Get(remoteReportUrl)
		if err != nil || response.StatusCode == http.StatusNotFound {
			log.Printf("[DEBUG] failed to get the azurerm operation report for version %v, use local azurerm report instead", azurermVersion)
			return localReportBytes
		}
		defer func() {
			if err := response.Body.Close(); err != nil {
				// Handle the error from resp.Body.Close()
				log.Printf("[Error] closing response body: %v", err)
			}
		}()

		bodyBytes, _ := io.ReadAll(response.Body)
		return bodyBytes
	}

	return localReportBytes
}

func getWorkingDirectory(uri string, os string) string {
	workingDirectory := path.Dir(strings.TrimPrefix(uri, "file://"))
	if os == "windows" {
		workingDirectory, _ = url.QueryUnescape(workingDirectory)
		workingDirectory = strings.ReplaceAll(workingDirectory, "/", "\\")
		workingDirectory = strings.TrimPrefix(workingDirectory, "\\")
	}
	return workingDirectory
}
