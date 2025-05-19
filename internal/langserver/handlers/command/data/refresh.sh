#!/bin/bash

SCRIPT_DIR=$(dirname "$0")

az provider operation list -o json >${SCRIPT_DIR}/provider_operations.json

curl -s -I https://github.com/hashicorp/terraform-provider-azurerm/releases/latest |
	grep -oP 'releases/tag/\Kv[0-9]+\.[0-9]+\.[0-9]+' |
	xargs -I {} curl -L https://raw.githubusercontent.com/wiki/magodo/aztfo/reports/\{\}.json -o ${SCRIPT_DIR}/aztfo_report.json
