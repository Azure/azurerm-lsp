package command

import (
	"context"
	"sort"
	"strings"
	"testing"
)

func Test_generateRoleConfig(t *testing.T) {
	actions := map[string]struct{}{
		"MICROSOFT.RESOURCES/DEPLOYMENTS/read":   {},
		"MICROSOFT.RESOURCES/DEPLOYMENTS/write":  {},
		"MICROSOFT.RESOURCES/DEPLOYMENTS/delete": {},
	}

	permissions, err := matchPermissions(actions)
	if err != nil {
		t.Errorf("error get required permissions: %+v", err)
	}

	existingPerm := &[]permission{
		{
			Actions: []string{
				"*/read",
			},
		},
	}

	*permissions = filterPermission(*permissions, *existingPerm)

	expected := []string{
		"Microsoft.Resources/deployments/delete",
		"Microsoft.Resources/deployments/write",
	}

	sort.Strings(permissions.Actions)
	sort.Strings(expected)

	for i := range permissions.Actions {
		if i == len(expected) || !strings.EqualFold(permissions.Actions[i], expected[i]) {
			t.Fatalf("expected generated permissions:\n%v\nbut got:\n%v", expected, permissions.Actions)
		}
	}

	t.Log(string(generateRoleConfig(*permissions)))
}

func Test_getAzurermMapping(t *testing.T) {
	ctx := context.Background()
	if _, err := getAzurermMapping(ctx, "."); err != nil {
		t.Errorf("error get azurerm mapping: %+v", err)
	}
}
