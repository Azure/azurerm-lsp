package schema

import (
	"fmt"
	"strings"

	"github.com/zclconf/go-cty/cty"
)

// This is a simplified and modified version of the hashicorp/terraform-json.
// The motivation for this is to add more information that is lost during the conversion from plugin sdk (v2) to the terraform core schema.
// (github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema/core_schema.go)
// Specifically, we are:
// 1. adding Required, Optional, Computed for the SchemaBlockType
// 2. adding Default for the SchemaAttribute
// 3. adding ExactlyOneOf, AtLeastOneOf, ConflictsWith and RequiredWith for both SchemaBlockType and the SchemaAttribute
// 4. removing any other attributes

type ProviderSchema struct {
	ResourceSchemas map[string]*Schema `json:"resource_schemas,omitempty"`
}

type Schema struct {
	Block *SchemaBlock `json:"block,omitempty"`
}

type SchemaBlock struct {
	Attributes   map[string]*SchemaAttribute `json:"attributes,omitempty"`
	NestedBlocks map[string]*SchemaBlockType `json:"block_types,omitempty"`
}

type SchemaBlockType struct {
	NestingMode NestingMode  `json:"nesting_mode,omitempty"`
	Block       *SchemaBlock `json:"block,omitempty"`

	Required bool `json:"required,omitempty"`
	Optional bool `json:"optional,omitempty"`
	Computed bool `json:"computed,omitempty"`

	ConflictsWith []string `json:"conflicts_with,omitempty"`
	ExactlyOneOf  []string `json:"exactly_one_of,omitempty"`
	AtLeastOneOf  []string `json:"at_least_one_of,omitempty"`
	RequiredWith  []string `json:"required_with,omitempty"`
}

type SchemaAttribute struct {
	Name          string   `json:"name,omitempty"`
	AttributeType cty.Type `json:"type,omitempty"`

	Required bool        `json:"required,omitempty"`
	Optional bool        `json:"optional,omitempty"`
	Computed bool        `json:"computed,omitempty"`
	Default  interface{} `json:"default,omitempty"`

	ConflictsWith []string `json:"conflicts_with,omitempty"`
	ExactlyOneOf  []string `json:"exactly_one_of,omitempty"`
	AtLeastOneOf  []string `json:"at_least_one_of,omitempty"`
	RequiredWith  []string `json:"required_with,omitempty"`

	// The following fields are not part of the original schema but are added for ease of use
	ResourceOrDataSourceName string `json:"resource_or_data_source_name,omitempty"`
	AttributePath            string `json:"attribute_path,omitempty"`
	Content                  string `json:"content,omitempty"`
	description              string

	PossibleValues []string `json:"possible_values,omitempty"`

	// Block specifics
	NestingMode NestingMode                 `json:"nesting_mode,omitempty"`
	Fields      map[string]*SchemaAttribute `json:"fields,omitempty"`
	sortOrder   string
}

func (b *SchemaAttribute) GetAutoCompletePossibleValues() []string {
	switch b.AttributeType {
	case cty.String:
		possibleValues := make([]string, 0, len(b.PossibleValues))
		for _, value := range b.PossibleValues {
			possibleValues = append(possibleValues, fmt.Sprintf(`"%s"`, value))
		}

		return possibleValues
	default:
		return b.PossibleValues
	}
}

func (b *SchemaAttribute) SetSortOrder(order string) {
	b.sortOrder = order
}

func (b *SchemaAttribute) GetSortOrder() string {
	return b.sortOrder
}

func (b *SchemaAttribute) GetDescription() string {
	if b.description != "" {
		return b.description
	}

	if b.Content == "" {
		return "UnDocumented"
	}

	parts := strings.SplitN(b.Content, "-", 2)
	if len(parts) < 2 {
		return b.Content
	}

	description := strings.TrimSpace(parts[1])

	possibleTypesPrefix := []string{"Optional", "Required", "(Optional)", "(Required)"}
	for _, prefix := range possibleTypesPrefix {
		if strings.HasPrefix(description, prefix) {
			description = strings.TrimPrefix(description, prefix)
			description = strings.TrimSpace(description)
			break
		}
	}

	b.description = description

	return description
}

func (b *SchemaAttribute) GetAttributeDocLink(parentLink string) string {
	fieldParts := strings.Split(b.AttributePath, ".")
	if len(fieldParts) == 0 {
		return parentLink
	}

	outerField := fieldParts[0]
	return fmt.Sprintf("%s#%s", parentLink, outerField)
}

func (b *SchemaAttribute) GetGitHubIssueLink() string {
	return fmt.Sprintf(GitHubIssuesURL, b.ResourceOrDataSourceName+" "+strings.ReplaceAll(b.AttributePath, ".", " "))
}

func (b *SchemaAttribute) GetRaiseGitHubIssueLink() string {
	return fmt.Sprintf(NewGitHubIssuesURL, fmt.Sprintf("`%s` - %s", b.ResourceOrDataSourceName, strings.ReplaceAll(b.AttributePath, ".", " ")))
}

func (b *SchemaAttribute) GetDetails() []string {
	var details []string
	if b.Default != nil {
		details = append(details, fmt.Sprintf("- **Default:** `%v`", b.Default))
	}
	if len(b.PossibleValues) > 0 {
		details = append(details, fmt.Sprintf("- **Possible Values:** `%v`", strings.Join(b.PossibleValues, "`, `")))
	}

	return details
}

func (b *SchemaAttribute) GetRequirementType() string {
	var requirementBadge string
	switch {
	case b.Required:
		requirementBadge = "required"
	case b.Optional:
		requirementBadge = "optional"
	}

	return requirementBadge
}
