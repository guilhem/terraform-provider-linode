//go:build unit

package domainrecord

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/linode/linodego"
	"github.com/stretchr/testify/require"
)

func TestReconcileName(t *testing.T) {
	for _, test := range []struct {
		name        string
		plannedName string
		apiName     string
		wantName    string
		parentCalls int32
		parentError bool
	}{
		{"identical relative", "www", "www", "www", 0, false},
		{"identical FQDN", "www.example.com", "www.example.com", "www.example.com", 0, false},
		{"unset name", "", "www", "www", 0, false},
		{"empty names", "", "", "", 0, false},
		{"FQDN prefix", "www.example.com", "www", "www.example.com", 1, false},
		{"FQDN apex", "example.com", "", "example.com", 1, false},
		{"different name", "old.example.com", "new", "new", 1, false},
		{"parent error", "www.example.com", "www", "", 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/v4/domains/1" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				if test.parentError {
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprint(w, `{"errors":[{"reason":"Domain not found"}]}`)
					return
				}
				fmt.Fprint(w, `{"id":1,"domain":"example.com"}`)
			}))
			defer server.Close()
			client := linodego.NewClient(server.Client())
			client.SetBaseURL(server.URL).SetAPIVersion("v4")
			d := schema.TestResourceDataRaw(t, resourceSchema, map[string]interface{}{
				"domain_id": 1,
				"name":      test.plannedName,
			})

			name, err := reconcileName(context.Background(), client, d, test.apiName)
			if test.parentError {
				require.ErrorContains(t, err, "failed to get parent domain")
				require.ErrorContains(t, err, "Domain not found")
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.wantName, name)
			require.Equal(t, test.parentCalls, requests.Load())
		})
	}
}
