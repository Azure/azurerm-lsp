package schema

const (
	TerraformDocsURL  = "https://registry.terraform.io/providers/hashicorp/azurerm/%[1]s/docs/%[2]s/%[3]s"
	DefaultDocVersion = "latest"
	Resources         = "resources"
	DataSources       = "data-sources"
	AzureRMPrefix     = "azurerm_"
	ResourcesPrefix   = "resource"
	DataSourcesPrefix = "data"
)

const (
	InputDataSourcePrefix = "datasource#"
)

const (
	GitHubIssuesURL    = "https://github.com/hashicorp/terraform-provider-azurerm/issues?q=is:issue %[1]s"
	NewGitHubIssuesURL = "https://github.com/hashicorp/terraform-provider-azurerm/issues/new?template=Bug_Report.yml&title=%[1]s "
)
