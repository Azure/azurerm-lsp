package provider_schema

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Azure/azurerm-lsp/provider-schema/azurerm/schema"
	"github.com/Azure/azurerm-lsp/provider-schema/processors"
)

var finalTerraformObject processors.TerraformObjects

func GetFinalTerraformObject() processors.TerraformObjects {
	if finalTerraformObject == nil {
		terraformObject, err := processors.LoadProcessedOutput()
		if err != nil {
			panic(err)
		}

		finalTerraformObject = terraformObject
	}

	return finalTerraformObject
}

func ListAllResources() []string {
	var resources []string

	for name, terraformObject := range GetFinalTerraformObject() {
		if terraformObject.IsDataSource() {
			continue
		}

		resources = append(resources, name)
	}

	return resources
}

func ListAllDataSources() []string {
	var dataSources []string

	for _, terraformObject := range GetFinalTerraformObject() {
		if !terraformObject.IsDataSource() {
			continue
		}

		dataSources = append(dataSources, terraformObject.GetName())
	}

	return dataSources
}

func ListAllResourcesAndDataSources() []string {
	var resourcesAndDataSources []string

	for name := range GetFinalTerraformObject() {
		resourcesAndDataSources = append(resourcesAndDataSources, name)
	}

	return resourcesAndDataSources
}

// NavigateToNestedBlock navigates down a nested block path and returns the final block or property
func NavigateToNestedBlock(objectName, path string) (*schema.SchemaAttribute, error) {
	resource, exists := GetFinalTerraformObject()[objectName]
	if !exists {
		return nil, fmt.Errorf("resource/data source '%s' not found", objectName)
	}

	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid path '%s'", path)
	}

	curFields := resource.Fields
	var result *schema.SchemaAttribute
	for _, part := range parts {
		result, exists = curFields[part]
		if !exists {
			return nil, fmt.Errorf("path '%s' not found in resource/data source '%s'", path, objectName)
		}
		curFields = result.Fields
	}

	if result == nil {
		return nil, fmt.Errorf("path '%s' is nil in resource/data source '%s'", path, objectName)
	}

	return result, nil
}

func GetObjectInfo(objectName string) (*processors.TerraformObject, error) {
	resource, exists := GetFinalTerraformObject()[objectName]
	if !exists {
		return nil, fmt.Errorf("resource/data source '%s' not found", objectName)
	}

	return resource, nil
}

func GetPropertyInfo(objectName, propertyPath string) (*schema.SchemaAttribute, error) {
	return NavigateToNestedBlock(objectName, propertyPath)
}

func GetPossibleValuesForProperty(objectName, propertyName string) ([]string, error) {
	block, err := NavigateToNestedBlock(objectName, propertyName)
	if err != nil {
		return nil, err
	}

	if block == nil {
		return nil, fmt.Errorf("block '%s' not found in resource/data source '%s'", propertyName, objectName)
	}

	if block.PossibleValues == nil {
		return nil, fmt.Errorf("no possible values found for block '%s' in resource/data source '%s'", propertyName, objectName)
	}

	return block.GetAutoCompletePossibleValues(), nil
}

func ListDirectProperties(objectName string, path string) ([]*schema.SchemaAttribute, error) {
	resource, exists := GetFinalTerraformObject()[objectName]
	if !exists {
		return nil, fmt.Errorf("resource/data source '%s' not found", objectName)
	}
	fields := resource.Fields

	if path != "" {
		block, err := NavigateToNestedBlock(objectName, path)
		if err != nil {
			return nil, err
		}

		if block == nil {
			return nil, fmt.Errorf("block '%s' not found in resource/data source '%s'", path, objectName)
		}

		fields = block.Fields
	}

	var properties []*schema.SchemaAttribute
	for _, property := range fields {
		if property.Computed {
			continue
		}

		properties = append(properties, property)
	}

	setSort(properties)

	return properties, nil
}

func setSort(properties []*schema.SchemaAttribute) {
	slices.SortFunc(properties, func(a, b *schema.SchemaAttribute) int {
		if a.Required && !b.Required {
			return -1
		}
		if !a.Required && b.Required {
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})

	sortIndex := 0
	for _, property := range properties {
		property.SetSortOrder(fmt.Sprintf("%d", sortIndex))
		sortIndex++
	}
}

func GetSnippet(objectName string) (string, error) {
	resource, exists := GetFinalTerraformObject()[objectName]
	if !exists {
		return "", fmt.Errorf("resource/data source '%s' not found", objectName)
	}

	return resource.GetSnippet(), nil
}

func GetPropertyDocContent(objectName string, property *schema.SchemaAttribute) string {
	if property == nil {
		return ""
	}

	propertyDescription := property.GetDescription()

	// try to get direct properties of this property
	directProperties, err := ListDirectProperties(objectName, property.AttributePath)
	if err != nil || len(directProperties) == 0 {
		return propertyDescription
	}

	// if there are direct properties, append them to the description
	var directPropertiesDescriptions []string
	for _, directProperty := range directProperties {
		directPropertiesDescriptions = append(directPropertiesDescriptions, fmt.Sprintf(" - %s: %s", directProperty.Name, directProperty.GetDescription()))
	}

	return fmt.Sprintf("%s\n\n%s", propertyDescription, strings.Join(directPropertiesDescriptions, "\n"))
}

func GetResourceContent(resourceName string) (string, bool, error) {
	resourceInfo, err := GetObjectInfo(resourceName)
	if err != nil {
		return "", false, fmt.Errorf("error retrieving resource info: %v", err)
	}
	return fmt.Sprintf(ResourceTemplate,
		resourceName,
		resourceInfo.GetResourceOrDataSourceDocLink(),
		resourceInfo.GetGitHubIssueLink(),
		resourceInfo.GetRaiseGitHubIssueLink(),
		resourceInfo.GetDocContent(),
	), resourceInfo.IsDataSource(), nil
}

func GetAttributeContent(resourceName, path string) (string, *schema.SchemaAttribute, error) {
	obj, err := GetObjectInfo(resourceName)
	if err != nil {
		return "", nil, fmt.Errorf("error retrieving object info: %v", err)
	}
	prop, err := GetPropertyInfo(resourceName, path)
	if err != nil {
		return "", nil, fmt.Errorf("error retrieving property info: %v", err)
	}
	return fmt.Sprintf(AttributeTemplate,
		prop.Name,
		prop.GetRequirementType(),
		prop.AttributeType.FriendlyName(),
		prop.GetAttributeDocLink(obj.GetResourceOrDataSourceDocLink()),
		prop.GetGitHubIssueLink(),
		prop.GetRaiseGitHubIssueLink(),
		strings.Join(prop.GetDetails(), "\n"),
		GetPropertyDocContent(resourceName, prop),
	), prop, nil
}
