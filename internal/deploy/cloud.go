package deploy

import (
	"context"
	"fmt"
	"strings"

	"github.com/coffeece/goship/internal/portal"
)

// ResolveCloudAccount turns what a person types into the account the API
// keys on. It matches, in order: the id, then an exact label, then the
// provider name — but only when exactly one account uses that provider.
func ResolveCloudAccount(ctx context.Context, client *portal.Client, org, ref string) (*portal.CloudAccount, error) {
	accounts, err := client.CloudAccounts(ctx, org)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if accounts[i].ID == ref {
			return &accounts[i], nil
		}
	}
	for i := range accounts {
		if accounts[i].Label == ref {
			return &accounts[i], nil
		}
	}

	var byProvider []portal.CloudAccount
	for _, a := range accounts {
		if a.Provider == ref {
			byProvider = append(byProvider, a)
		}
	}
	switch len(byProvider) {
	case 1:
		return &byProvider[0], nil
	case 0:
		if providers, err := client.CloudProviders(ctx); err == nil {
			for _, p := range providers {
				if p.Name == ref {
					return nil, fmt.Errorf("no %s account connected; run `goship cloud connect %s`", p.Label, p.Name)
				}
			}
		}
		return nil, fmt.Errorf("no cloud account %q in org %q; `goship cloud list` (goship_list_cloud_accounts) shows the connected ones, `goship cloud connect <provider>` adds one", ref, org)
	default:
		labels := make([]string, len(byProvider))
		for i, a := range byProvider {
			labels[i] = a.Label
		}
		return nil, fmt.Errorf("more than one %s account in org %q: %s; pass the account's id or label", ref, org, strings.Join(labels, ", "))
	}
}
