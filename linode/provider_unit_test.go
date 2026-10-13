//go:build unit

package linode_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/linode/linodego/v2"
	"github.com/linode/terraform-provider-linode/v4/linode"
	"github.com/stretchr/testify/require"
)

func TestProviderConfigureWithoutAPIRequests(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/v4/domains/1" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("configured token was not used")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if status == http.StatusOK {
					fmt.Fprint(w, `{"id":1,"domain":"example.com"}`)
				} else {
					fmt.Fprint(w, `{"errors":[{"reason":"Invalid token"}]}`)
				}
			}))
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			provider := linode.Provider()
			d := schema.TestResourceDataRaw(t, provider.Schema, map[string]any{
				"token":       "test-token",
				"url":         server.URL,
				"api_version": "v4",
				"config_path": filepath.Join(t.TempDir(), "missing-config"),
			})
			t.Setenv(linodego.APIHostCert, "")
			meta, diags := provider.ConfigureContextFunc(ctx, d)
			require.False(t, diags.HasError(), "%v", diags)
			require.Zero(t, requests.Load(), "configuration must not call the API")

			resource := provider.ResourcesMap["linode_domain"]
			data := schema.TestResourceDataRaw(t, resource.Schema, nil)
			data.SetId("1")
			diags = resource.ReadContext(ctx, data, meta)
			if status == http.StatusOK {
				require.False(t, diags.HasError(), "%v", diags)
				require.Equal(t, "example.com", data.Get("domain"))
			} else {
				require.True(t, diags.HasError())
				require.Contains(t, diags[0].Summary, "Invalid token")
			}
			require.Equal(t, "1", data.Id())
			require.EqualValues(t, 1, requests.Load())
		})
	}
}

func TestProviderConfigurePreservesInitializationErrors(t *testing.T) {
	provider := linode.Provider()
	missing := filepath.Join(t.TempDir(), "missing")
	d := schema.TestResourceDataRaw(t, provider.Schema, map[string]any{
		"api_ca_path": missing,
		"config_path": missing,
	})
	meta, diags := provider.ConfigureContextFunc(context.Background(), d)
	require.Nil(t, meta)
	require.True(t, diags.HasError())
	require.Contains(t, diags[0].Summary, "failed to initialize client")
}
